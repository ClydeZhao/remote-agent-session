#!/usr/bin/env python3
import argparse
import os
import base64
if os.name != "nt":
    import fcntl
import hashlib
import json
from pathlib import Path
import plistlib
import shutil
import signal
import stat
import tempfile
import socket
import subprocess
import sys
import time
from urllib.parse import quote
import urllib.request
import uuid

ROOT = Path(__file__).resolve().parents[1]


def canonical(path):
    return Path(path).expanduser().resolve()


def overlaps(first, second):
    return first == second or first in second.parents or second in first.parents


def client_roots():
    roots = {canonical(Path.home() / ".remote-agent-session")}
    for name in ("REMOTE_AGENT_SESSION_STATE_DIR", "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR"):
        if os.environ.get(name):
            roots.add(canonical(os.environ[name]))
    directory = registry_directory()
    if directory.exists() or directory.is_symlink():
        info = directory.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise RuntimeError("Persistent root registry directory must be owned and private")
        roots.update(canonical(root) for root in read_service_roots(directory, "client-roots.json"))
    homes = {canonical(Path.home() / ".dsh")}
    if os.environ.get("DSH_HOME"):
        homes.add(canonical(os.environ["DSH_HOME"]))
    for home in homes:
        for name in (".remote-agent-session-permissions.json", ".remote-agent-session-permissions-pending.json"):
            for record in (home / "profiles").glob("*/" + name):
                try:
                    if record.stat().st_size > 1048576:
                        raise ValueError()
                    data = json.loads(record.read_text())
                    data = data.get("after_record", data)
                    grants = data["grants"]
                    if not isinstance(grants, dict):
                        raise ValueError()
                    for root, grant in grants.items():
                        if not Path(root).is_absolute() or not isinstance(grant, dict):
                            raise ValueError()
                        roots.add(canonical(root))
                        for owner in grant.get("owners", {}).values():
                            root = owner["state_directory"]
                            if not Path(root).is_absolute():
                                raise ValueError()
                            roots.add(canonical(root))
                except (OSError, ValueError, KeyError, TypeError, AttributeError):
                    raise RuntimeError("Cannot verify known DSH permission ownership; inspect its local permission records before starting the service") from None
    return roots


def validate_service_state(state):
    state = canonical(state)
    if overlaps(state, canonical(registry_directory())):
        raise RuntimeError("Service state must be separate from the persistent service registry control directory")
    for client in client_roots():
        if overlaps(state, client):
            raise RuntimeError("Service state must be separate from client state and granted client roots, including their ancestors, descendants and symlink targets; choose --service-state-dir outside those roots")
    for ancestor in (state, *state.parents):
        if any(ancestor.glob("credential-*.json")):
            raise RuntimeError("Service state cannot be inside a client credential directory; choose a separate --service-state-dir")
    return state


def registry_directory():
    return Path.home() / ".remote-agent-session-runtime"


def registry_file(path, flags):
    descriptor = os.open(path, flags | os.O_NOFOLLOW, 0o600)
    info = os.fstat(descriptor)
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        os.close(descriptor)
        raise RuntimeError("Private root registry must be an owned private regular file")
    return descriptor


def read_service_roots(directory, filename="service-roots.json"):
    path = directory / filename
    try:
        descriptor = registry_file(path, os.O_RDONLY)
    except FileNotFoundError:
        return []
    with os.fdopen(descriptor) as stream:
        if os.fstat(stream.fileno()).st_size > 1048576:
            raise RuntimeError("Private root registry is too large")
        try:
            data = json.load(stream)
            roots = data["roots"]
            if type(data["schema"]) is not int or data["schema"] != 1 or not isinstance(roots, list) or len(roots) != len(set(roots)):
                raise ValueError()
            for value in roots:
                if not isinstance(value, str) or not Path(value).is_absolute() or str(canonical(value)) != value:
                    raise ValueError()
            return roots
        except (ValueError, KeyError, TypeError):
            raise RuntimeError("Private root registry is invalid; restore its private metadata before starting") from None


def update_service_registry(state, remove=False):
    directory = private_dir(registry_directory())
    with os.fdopen(registry_file(directory / "service-roots.lock", os.O_RDWR | os.O_CREAT), "a+") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        roots = read_service_roots(directory)
        if not remove:
            validate_service_state(state)
        value = str(canonical(state))
        if remove:
            try:
                request(state, "status")
            except (OSError, ValueError):
                pass
            else:
                raise RuntimeError("Stop the service before forgetting its private data root")
            if state.exists() and (not state.is_dir() or any(state.iterdir())):
                raise RuntimeError("Service data remains; retain its registered root until private data is explicitly removed")
            roots = [root for root in roots if root != value]
        elif value not in roots:
            roots.append(value)
        descriptor, temporary = tempfile.mkstemp(prefix=".service-roots-", dir=directory)
        try:
            with os.fdopen(descriptor, "w") as stream:
                json.dump({"schema": 1, "roots": sorted(roots)}, stream)
                stream.write("\n")
                stream.flush()
                os.fsync(stream.fileno())
            os.replace(temporary, directory / "service-roots.json")
            directory_descriptor = os.open(directory, os.O_RDONLY)
            try:
                os.fsync(directory_descriptor)
            finally:
                os.close(directory_descriptor)
            if not remove:
                private_dir(state)
                marker = state / ".remote-agent-session-service-root"
                if not marker.exists():
                    private_file(marker, b'{"schema":1}\n')
        finally:
            Path(temporary).unlink(missing_ok=True)
    return {"service_root_registered": not remove, "service_state_dir": value}


def private_dir(path):
    path.mkdir(parents=True, exist_ok=True, mode=0o700)
    if path.is_symlink() or path.stat().st_uid != os.getuid():
        raise RuntimeError("State directory must be owned by the current user")
    path.chmod(0o700)
    return path


def paths(state):
    runtime = private_dir(Path((os.environ.get("REMOTE_AGENT_SESSION_RUNTIME_DIR") or "~/.remote-agent-session-runtime")).expanduser())
    key = hashlib.sha256(str(state).encode()).hexdigest()[:20]
    return runtime, runtime / (key + ".sock"), runtime / (key + ".lock")


def request(state, command):
    _, endpoint, _ = paths(state)
    with socket.socket(socket.AF_UNIX) as client:
        client.settimeout(3)
        client.connect(str(endpoint))
        client.sendall((json.dumps(command) + "\n").encode() if isinstance(command, dict) else command.encode())
        payload = client.recv(65536)
        if not payload:
            raise ConnectionRefusedError("Service control socket closed during shutdown")
        return json.loads(payload)


def control_reply(payload, status, owner):
    reply = {**status, "installation": str(owner), "control_protocol": 1}
    if payload == "status":
        return reply, False
    try:
        command = json.loads(payload)
    except (ValueError, TypeError):
        return {**reply, "stop_accepted": False, "reason": "invalid_control_request"}, False
    if isinstance(command, dict) and type(command.get("protocol")) is int and command.get("protocol") == 1 and command.get("command") == "stop" and command.get("intent") == "explicit-user-stop":
        return {**reply, "stop_accepted": True, "reason": "explicit_user_stop"}, True
    expected = command.get("expected_installation") if isinstance(command, dict) else None
    try:
        valid = isinstance(expected, str) and Path(expected).is_absolute() and str(canonical(expected)) == expected
    except (OSError, ValueError, RuntimeError):
        valid = False
    accepted = isinstance(command, dict) and type(command.get("protocol")) is int and command.get("protocol") == 1 and command.get("command") == "stop-owned" and valid and expected == str(owner)
    return {**reply, "stop_accepted": accepted, "reason": "owned_installation" if accepted else "installation_mismatch_or_invalid_request"}, accepted


def stop_owned(state):
    try:
        result = request(state, {"protocol": 1, "command": "stop-owned", "expected_installation": str(ROOT.resolve())})
    except (FileNotFoundError, ConnectionRefusedError):
        return {"running": False}
    if result.get("stop_accepted") is not True:
        return {"running": True, "stop_skipped": result.get("reason", "supervisor_ownership_protocol_unavailable")}
    for _ in range(150):
        try:
            current = request(state, "status")
        except (OSError, ValueError):
            return {"running": False}
        if current.get("pid") != result.get("pid") or current.get("installation") != str(ROOT.resolve()):
            return {"running": True, "stop_skipped": "replacement_supervisor_preserved"}
        time.sleep(0.1)
    raise RuntimeError("Owned installation did not stop; no replacement process was signalled")


def pg_binary(name):
    bindir = os.environ.get("REMOTE_AGENT_SESSION_PG_BIN")
    candidate = Path(bindir) / name if bindir else None
    if candidate and candidate.is_file():
        return str(candidate)
    result = shutil.which(name)
    if result:
        return result
    config = shutil.which("pg_config")
    if config:
        candidate = Path(subprocess.check_output([config, "--bindir"], text=True).strip()) / name
        if candidate.is_file():
            return str(candidate)
    raise RuntimeError("PostgreSQL executable missing: " + name)


def environment(state, mode):
    env = dict(os.environ)
    env["REMOTE_AGENT_SESSION_MODE"] = mode
    env["REMOTE_AGENT_SESSION_SERVICE_STATE_DIR"] = str(state)
    env["REMOTE_AGENT_SESSION_KEY_FILE"] = str(state / "encryption.key")
    env["REMOTE_AGENT_SESSION_FEISHU_CONFIG"] = str(state / "feishu.env")
    env["REMOTE_AGENT_SESSION_DATABASE_URL"] = "postgresql://" + quote(os.environ.get("USER", ""), safe="") + "@/remote_agent_session?host=" + quote(str(state / "pgsocket"), safe="")
    env["PYTHONUNBUFFERED"] = "1"
    env["LC_ALL"] = "C"
    return env


def python_binary():
    candidate = ROOT / ".venv/bin/python"
    return str(candidate) if candidate.exists() else sys.executable


def private_file(path, data):
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "wb") as stream:
        stream.write(data)


def config_app(state, mode):
    if mode != "live":
        return None
    config = state / "feishu.env"
    if not config.is_file() or config.is_symlink() or config.stat().st_mode & 0o077:
        raise RuntimeError("Live mode requires a private (0600) feishu.env in the state directory")
    values = dict(line.split("=", 1) for line in config.read_text().splitlines() if "=" in line and not line.lstrip().startswith("#"))
    if not values.get("LARK_APP_ID") or not values.get("LARK_APP_SECRET"):
        raise RuntimeError("feishu.env must contain LARK_APP_ID and LARK_APP_SECRET")
    return values["LARK_APP_ID"].strip()


def startup_remaining(deadline, phase):
    remaining = deadline - time.monotonic()
    if remaining <= 0:
        raise RuntimeError("Startup deadline exhausted during " + phase)
    return remaining


def wait_ready(phase, children, probe, deadline, cancelled=lambda: False):
    last = "not ready"
    while True:
        if cancelled():
            raise RuntimeError("Startup cancelled during " + phase)
        for name, child in children():
            code = child.poll()
            if code is not None:
                raise RuntimeError("Startup component exited during " + phase + ": " + name + " (exit " + str(code) + ")")
        remaining = startup_remaining(deadline, phase + "; last probe: " + last)
        try:
            result = probe(min(5.0, remaining))
            ready, last = result if isinstance(result, tuple) else (result, "health response not ready")
        except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
            ready = False
            last = type(exc).__name__
        if ready:
            startup_remaining(deadline, phase)
            if all(child.poll() is None for _, child in children()):
                return
        time.sleep(min(0.2, startup_remaining(deadline, phase + "; last probe: " + last)))


def startup_record(state, instance, phase, error=""):
    descriptor, temporary = tempfile.mkstemp(prefix=".startup-", dir=state)
    try:
        with os.fdopen(descriptor, "w") as stream:
            json.dump({"instance_id": instance, "phase": phase, "error": error}, stream)
        os.replace(temporary, state / "startup-status.json")
    finally:
        Path(temporary).unlink(missing_ok=True)


def run(state, mode, port):
    deadline = float(os.environ.get("REMOTE_AGENT_SESSION_STARTUP_DEADLINE", time.monotonic() + 180))
    instance = os.environ.get("REMOTE_AGENT_SESSION_INSTANCE_ID") or uuid.uuid4().hex
    phase = "initialization"
    validate_service_state(state)
    update_service_registry(state)
    private_dir(state)
    runtime, endpoint, lockpath = paths(state)
    with lockpath.open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            raise RuntimeError("This state directory already has a supervisor")
        app = config_app(state, mode)
        app_lock = None
        if app:
            app_lock = os.fdopen(registry_file(registry_directory() / ("feishu-" + hashlib.sha256(app.encode()).hexdigest() + ".lock"), os.O_RDWR | os.O_CREAT), "a+")
            try:
                fcntl.flock(app_lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                raise RuntimeError("This Feishu application already has a consumer in another installation")
        if not (state / "encryption.key").exists():
            private_file(state / "encryption.key", base64.urlsafe_b64encode(os.urandom(32)))
        env = environment(state, mode)
        env["REMOTE_AGENT_SESSION_INSTANCE_ID"] = instance
        children = []
        names = {}
        ready = False
        stopping = False
        def stop_signal(signum, frame):
            nonlocal stopping
            stopping = True
        signal.signal(signal.SIGTERM, stop_signal)
        signal.signal(signal.SIGINT, stop_signal)
        logfd = os.open(state / "service.log", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
        os.chmod(state / "service.log", 0o600)
        with os.fdopen(logfd, "ab", buffering=0) as log:
            def launch(args, cwd=None, name=None):
                held_locks = (lock.fileno(), app_lock.fileno()) if app_lock else (lock.fileno(),)
                command = [python_binary(), str(ROOT / "tools/managed_child.py"), "--parent", str(os.getpid()), "--", *args]
                child = subprocess.Popen(command, cwd=cwd, env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log, pass_fds=held_locks)
                children.append(child)
                names[child.pid] = name or " ".join(args[:3])
                return child
            def checked(args, cwd=None):
                child = launch(args, cwd=cwd)
                if child.wait(timeout=startup_remaining(deadline, phase)) != 0:
                    raise RuntimeError("Managed initialization failed; inspect the private service.log")
                children.remove(child)
            endpoint.unlink(missing_ok=True)
            with socket.socket(socket.AF_UNIX) as server:
                try:
                    data = state / "postgres"
                    private_dir(state / "pgsocket")
                    phase = "PostgreSQL initialization"
                    if not (data / "PG_VERSION").exists():
                        checked([pg_binary("initdb"), "-D", str(data), "--auth-local=trust", "--auth-host=reject", "--no-locale", "--encoding=UTF8"])
                    pg = launch([pg_binary("postgres"), "-D", str(data), "-k", str(state / "pgsocket"), "-h", "", "-p", "5432"])
                    phase = "PostgreSQL readiness"
                    def postgres_probe(timeout):
                        return subprocess.run([pg_binary("pg_isready"), "-h", str(state / "pgsocket"), "-p", "5432", "-d", "postgres"], timeout=timeout, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL).returncode == 0
                    tracked = lambda: [(names[child.pid], child) for child in children]
                    wait_ready(phase, tracked, postgres_probe, deadline, lambda: stopping)
                    phase = "database preparation"
                    exists = subprocess.check_output([pg_binary("psql"), "-h", str(state / "pgsocket"), "-d", "postgres", "-tAc", "SELECT 1 FROM pg_database WHERE datname='remote_agent_session'"], stderr=log, text=True, timeout=min(10, startup_remaining(deadline, phase))).strip()
                    if exists != "1":
                        checked([pg_binary("createdb"), "-h", str(state / "pgsocket"), "remote_agent_session"])
                    phase = "schema migration"
                    checked([python_binary(), "-m", "alembic", "upgrade", "head"], cwd=ROOT / "server")
                    launch([python_binary(), "-m", "uvicorn", "remote_agent_session.api:create_app", "--factory", "--host", "127.0.0.1", "--port", str(port)])
                    launch([python_binary(), "-m", "remote_agent_session.monitor"])
                    if mode == "live":
                        launch([python_binary(), "-m", "remote_agent_session.feishu_events"])
                    phase = "API readiness"
                    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                    def api_probe(timeout):
                        with opener.open("http://127.0.0.1:" + str(port) + "/health", timeout=timeout) as response:
                            body = json.loads(response.read(8192))
                            if response.status != 200 or not isinstance(body, dict) or body.get("status") != "ok":
                                return False, "database health response not ready"
                            return body.get("instance_id") == instance, "API instance mismatch"
                    wait_ready(phase, tracked, api_probe, deadline, lambda: stopping)
                    ready = True
                    server.bind(str(endpoint))
                    endpoint.chmod(0o600)
                    server.listen(4)
                    server.settimeout(0.25)
                    startup_record(state, instance, "ready")
                    while not stopping:
                        if any(child.poll() is not None for child in children):
                            raise RuntimeError("A managed component exited; the complete service has been stopped")
                        try:
                            client, _ = server.accept()
                        except socket.timeout:
                            continue
                        with client:
                            client.settimeout(1)
                            try:
                                payload = client.recv(16384)
                                if payload.startswith(b"{"):
                                    while b"\n" not in payload and len(payload) < 16384:
                                        chunk = client.recv(16384 - len(payload))
                                        if not chunk:
                                            break
                                        payload += chunk
                            except OSError:
                                continue
                            status = {"running": True, "ready": ready, "mode": mode, "api_url": "http://127.0.0.1:" + str(port), "pid": os.getpid()}
                            reply, should_stop = control_reply(payload.decode(errors="replace"), status, ROOT.resolve())
                            if should_stop:
                                stopping = True
                            try:
                                client.sendall(json.dumps(reply).encode())
                            except (BrokenPipeError, ConnectionResetError):
                                continue
                except Exception as exc:
                    startup_record(state, instance, phase, str(exc) if isinstance(exc, RuntimeError) else type(exc).__name__)
                    raise
                finally:
                    for child in reversed(children):
                        if child.poll() is None:
                            child.send_signal(signal.SIGINT if child is children[0] else signal.SIGTERM)
                    for child in reversed(children):
                        try:
                            child.wait(timeout=10)
                        except subprocess.TimeoutExpired:
                            child.kill()
                            child.wait()
                    endpoint.unlink(missing_ok=True)


def launch_service(state, mode, port):
    validate_service_state(state)
    update_service_registry(state)
    private_dir(state)
    try:
        current = request(state, "status")
        if current.get("mode") != mode or current.get("api_url") != "http://127.0.0.1:" + str(port):
            raise RuntimeError("Service already runs with different mode or port; restart explicitly to change it")
        return current
    except (OSError, ValueError):
        pass
    deadline = time.monotonic() + 180
    instance = uuid.uuid4().hex
    env = dict(os.environ, REMOTE_AGENT_SESSION_STARTUP_DEADLINE=str(deadline), REMOTE_AGENT_SESSION_INSTANCE_ID=instance)
    logfd = os.open(state / "supervisor.log", os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o600)
    with os.fdopen(logfd, "ab") as log:
        child = subprocess.Popen([python_binary(), str(Path(__file__).resolve()), "run", "--service-state-dir", str(state), "--mode", mode, "--port", str(port)], env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log, start_new_session=True)
    try:
        while time.monotonic() < deadline:
            try:
                return request(state, "status")
            except (OSError, ValueError):
                pass
            if child.poll() is not None:
                try:
                    result = json.loads((state / "startup-status.json").read_text())
                except (OSError, ValueError):
                    result = {}
                cause = (result.get("phase", "unknown phase") + ": " + result.get("error", "")) if result.get("instance_id") == instance else "supervisor exited before readiness"
                raise RuntimeError("Service startup failed: " + (cause or result.get("phase", "unknown phase")) + "; inspect private startup-status.json and logs")
            time.sleep(0.2)
        raise RuntimeError("Service startup deadline exhausted; inspect private startup-status.json and logs")
    except Exception:
        if child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=30)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()
        raise


def autostart(state, mode, port, remove):
    directory = private_dir(registry_directory())
    name = "autostart-" + hashlib.sha256(str(state).encode()).hexdigest()[:20] + ".lock"
    with os.fdopen(registry_file(directory / name, os.O_RDWR | os.O_CREAT), "a+") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        return autostart_locked(state, mode, port, remove)


def autostart_locked(state, mode, port, remove):
    if sys.platform != "darwin":
        if remove:
            return {"autostart": False}
        raise RuntimeError("Login autostart currently supports macOS only")
    label = "io.remote-agent-session." + hashlib.sha256(str(state).encode()).hexdigest()[:12]
    target = Path.home() / "Library/LaunchAgents" / (label + ".plist")
    domain = "gui/" + str(os.getuid())
    if remove:
        if target.exists():
            try:
                existing = plistlib.loads(target.read_bytes())
                arguments = existing.get("ProgramArguments")
                owned = isinstance(arguments, list) and len(arguments) >= 2 and isinstance(arguments[1], str) and canonical(arguments[1]) == Path(__file__).resolve()
            except (OSError, ValueError, TypeError, AttributeError, plistlib.InvalidFileException):
                owned = False
            if not owned:
                return {"autostart_removed": False, "reason": "different_or_unverifiable_installation"}
            subprocess.run(["launchctl", "bootout", domain + "/" + label], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            target.unlink()
        return {"autostart": False}
    validate_service_state(state)
    try:
        request(state, "status")
    except (OSError, ValueError):
        pass
    else:
        raise RuntimeError("Stop the running service before enabling login autostart")
    target.parent.mkdir(parents=True, exist_ok=True)
    if target.exists():
        raise RuntimeError("Autostart already exists; remove its owning entry before replacing it")
    update_service_registry(state)
    private_dir(state)
    definition = {"Label": label, "ProgramArguments": [python_binary(), str(Path(__file__).resolve()), "run", "--service-state-dir", str(state), "--mode", mode, "--port", str(port)], "RunAtLoad": True, "KeepAlive": {"SuccessfulExit": False}, "ThrottleInterval": 30, "StandardOutPath": str(state / "launchd.log"), "StandardErrorPath": str(state / "launchd.log"), "EnvironmentVariables": {"PATH": os.environ.get("PATH", "/usr/bin:/bin"), "REMOTE_AGENT_SESSION_PG_BIN": str(Path(pg_binary("postgres")).parent)}, "Umask": 63}
    for name in ("REMOTE_AGENT_SESSION_RUNTIME_DIR", "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR", "REMOTE_AGENT_SESSION_STATE_DIR", "DSH_HOME"):
        if os.environ.get(name):
            definition["EnvironmentVariables"][name] = os.environ[name]
    private_file(target, plistlib.dumps(definition))
    subprocess.run(["launchctl", "bootstrap", domain, str(target)], check=True)
    return {"autostart": True}


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument("command", choices=["start", "stop", "restart", "status", "doctor", "run", "service-install", "service-remove", "service-forget", "stop-owned"])
    parser.add_argument("--service-state-dir", default=(os.environ.get("REMOTE_AGENT_SESSION_SERVICE_STATE_DIR") or "~/.remote-agent-session-service"))
    parser.add_argument("--mode", choices=["offline", "live"], default=os.environ.get("REMOTE_AGENT_SESSION_MODE", "live"))
    parser.add_argument("--port", type=int, default=18481)
    parser.add_argument("--json", action="store_true")
    args = parser.parse_args()
    state = canonical(args.service_state_dir)
    try:
        if args.command == "run":
            run(state, args.mode, args.port)
            return
        if args.command in ("stop", "restart"):
            try:
                accepted = request(state, {"protocol": 1, "command": "stop", "intent": "explicit-user-stop"})
                if accepted.get("stop_accepted") is not True:
                    raise RuntimeError("Supervisor did not accept the stop protocol; use the matching owning installation for explicit stop")
                for _ in range(150):
                    try:
                        request(state, "status")
                    except (OSError, ValueError):
                        break
                    time.sleep(0.1)
                else:
                    raise RuntimeError("Service did not stop; no unrelated process was signalled")
            except (FileNotFoundError, ConnectionRefusedError):
                pass
            result = {"running": False}
        if args.command in ("start", "restart"):
            result = launch_service(state, args.mode, args.port)
        elif args.command == "status":
            try:
                result = request(state, "status")
            except (OSError, ValueError):
                result = {"running": False}
        elif args.command == "doctor":
            validate_service_state(state)
            result = {"python": python_binary(), "postgres": pg_binary("postgres"), "installation": str(ROOT), "service_state_dir": str(state), "mode": args.mode, "feishu_configured": bool(config_app(state, args.mode))}
            try:
                result["service"] = request(state, "status")
            except (OSError, ValueError):
                result["service"] = {"running": False}
        elif args.command == "stop-owned":
            result = stop_owned(state)
        elif args.command == "service-forget":
            result = update_service_registry(state, remove=True)
        elif args.command in ("service-install", "service-remove"):
            result = autostart(state, args.mode, args.port, args.command == "service-remove")
        print(json.dumps(result))
    except Exception as exc:
        print(json.dumps({"error": str(exc)}), file=sys.stderr)
        sys.exit(1)


if os.name == "nt":
    import windows_lifecycle
    windows_lifecycle.install(globals())


if __name__ == "__main__":
    main()
