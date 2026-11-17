import base64
import contextlib
import ctypes
from ctypes import wintypes
import hashlib
from functools import lru_cache
import hmac
import json
import os
from pathlib import Path
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import uuid
from urllib.parse import quote
import urllib.request


@lru_cache(maxsize=1)
def current_sid():
    result = subprocess.check_output(["whoami", "/user", "/fo", "csv", "/nh"], text=True)
    import csv
    sid = next(csv.reader([result.strip()]))[1]
    if not sid.startswith("S-1-5-"):
        raise RuntimeError("Cannot establish current Windows user SID")
    return sid


def security_api():
    api = ctypes.WinDLL("advapi32", use_last_error=True)
    pointer = ctypes.c_void_p
    out_pointer = ctypes.POINTER(pointer)
    out_bool = ctypes.POINTER(wintypes.BOOL)
    api.ConvertStringSecurityDescriptorToSecurityDescriptorW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, out_pointer, ctypes.POINTER(wintypes.DWORD)]
    api.ConvertStringSidToSidW.argtypes = [wintypes.LPCWSTR, out_pointer]
    api.SetFileSecurityW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, pointer]
    api.GetFileSecurityW.argtypes = [wintypes.LPCWSTR, wintypes.DWORD, pointer, wintypes.DWORD, ctypes.POINTER(wintypes.DWORD)]
    api.GetSecurityDescriptorOwner.argtypes = [pointer, out_pointer, out_bool]
    api.GetSecurityDescriptorDacl.argtypes = [pointer, out_bool, out_pointer, out_bool]
    api.GetSecurityDescriptorControl.argtypes = [pointer, ctypes.POINTER(wintypes.WORD), ctypes.POINTER(wintypes.DWORD)]
    api.GetAclInformation.argtypes = [pointer, pointer, wintypes.DWORD, ctypes.c_int]
    api.GetAce.argtypes = [pointer, wintypes.DWORD, out_pointer]
    api.EqualSid.argtypes = [pointer, pointer]
    return api


def free_local(pointer):
    api = ctypes.WinDLL("kernel32", use_last_error=True)
    api.LocalFree.argtypes = [ctypes.c_void_p]
    api.LocalFree.restype = ctypes.c_void_p
    api.LocalFree(pointer)


def private_acl(api, descriptor, sid, directory):
    class AclSize(ctypes.Structure):
        _fields_ = [("AceCount", wintypes.DWORD), ("AclBytesInUse", wintypes.DWORD), ("AclBytesFree", wintypes.DWORD)]
    owner = ctypes.c_void_p()
    defaulted = wintypes.BOOL()
    present = wintypes.BOOL()
    dacl = ctypes.c_void_p()
    control = wintypes.WORD()
    revision = wintypes.DWORD()
    if not api.GetSecurityDescriptorOwner(descriptor, ctypes.byref(owner), ctypes.byref(defaulted)) or not owner or not api.EqualSid(owner, sid):
        return False, False
    if not api.GetSecurityDescriptorDacl(descriptor, ctypes.byref(present), ctypes.byref(dacl), ctypes.byref(defaulted)) or not present.value or not dacl:
        return False, False
    if not api.GetSecurityDescriptorControl(descriptor, ctypes.byref(control), ctypes.byref(revision)):
        raise ctypes.WinError(ctypes.get_last_error())
    size = AclSize()
    if not api.GetAclInformation(dacl, ctypes.byref(size), ctypes.sizeof(size), 2):
        raise ctypes.WinError(ctypes.get_last_error())
    if size.AceCount != 1:
        return False, False
    ace = ctypes.c_void_p()
    if not api.GetAce(dacl, 0, ctypes.byref(ace)):
        raise ctypes.WinError(ctypes.get_last_error())
    ace_type = ctypes.c_ubyte.from_address(ace.value).value
    ace_flags = ctypes.c_ubyte.from_address(ace.value + 1).value
    ace_mask = wintypes.DWORD.from_address(ace.value + 4).value
    if ace_type != 0 or ace_mask != 0x1F01FF or not api.EqualSid(ctypes.c_void_p(ace.value + 8), sid):
        return False, False
    protected = bool(control.value & 0x1000)
    explicit_private = protected and ace_flags == (3 if directory else 0)
    inherited_private = not directory and ace_flags == 0x10
    return explicit_private, inherited_private


def secure_path(path, create=False, protect_private_inheritance=False):
    path = Path(path)
    info = path.lstat()
    if info.st_file_attributes & stat.FILE_ATTRIBUTE_REPARSE_POINT:
        raise RuntimeError("Private Windows paths cannot be reparse points")
    api = security_api()
    sid_text = current_sid()
    directory = path.is_dir()
    flags = "OICI" if directory else ""
    if create:
        descriptor = ctypes.c_void_p()
        if not api.ConvertStringSecurityDescriptorToSecurityDescriptorW("O:" + sid_text + "D:P(A;" + flags + ";FA;;;" + sid_text + ")", 1, ctypes.byref(descriptor), None):
            raise ctypes.WinError(ctypes.get_last_error())
        try:
            if not api.SetFileSecurityW(str(path), 0x80000004, descriptor):
                raise ctypes.WinError(ctypes.get_last_error())
        finally:
            free_local(descriptor)
    size = wintypes.DWORD()
    api.GetFileSecurityW(str(path), 5, None, 0, ctypes.byref(size))
    buffer = ctypes.create_string_buffer(size.value)
    if not api.GetFileSecurityW(str(path), 5, buffer, size, ctypes.byref(size)):
        raise ctypes.WinError(ctypes.get_last_error())
    sid = ctypes.c_void_p()
    if not api.ConvertStringSidToSidW(sid_text, ctypes.byref(sid)):
        raise ctypes.WinError(ctypes.get_last_error())
    try:
        protected, inherited = private_acl(api, buffer, sid, directory)
    finally:
        free_local(sid)
    if protected:
        return path
    if protect_private_inheritance and inherited:
        secure_path(path.parent)
        return secure_path(path, create=True)
    raise RuntimeError("Private Windows path must have a protected current-user-only ACL: " + str(path))


def private_dir(path):
    path = Path(path)
    if path.exists():
        return secure_path(path)
    parent = path.parent
    if not parent.exists():
        private_dir(parent)
    path.mkdir()
    return secure_path(path, create=True)


def private_file(path, data):
    with open(path, "xb") as stream:
        secure_path(path, create=True)
        stream.write(data)


@contextlib.contextmanager
def exclusive_lock(path, blocking=True):
    import msvcrt
    if not path.exists():
        try:
            private_file(path, b"0")
        except FileExistsError:
            pass
    secure_path(path)
    with open(path, "r+b") as stream:
        while True:
            try:
                msvcrt.locking(stream.fileno(), msvcrt.LK_NBLCK, 1)
                break
            except OSError:
                if not blocking:
                    raise RuntimeError("This service or Feishu application already has a supervisor") from None
                time.sleep(0.1)
        try:
            yield stream
        finally:
            stream.seek(0)
            msvcrt.locking(stream.fileno(), msvcrt.LK_UNLCK, 1)


def atomic_json(path, value):
    temporary = path.with_name("." + path.name + "-" + uuid.uuid4().hex)
    try:
        private_file(temporary, (json.dumps(value) + "\n").encode())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)


def registry_directory():
    return Path(os.environ.get("REMOTE_AGENT_SESSION_RUNTIME_DIR") or str(Path.home() / ".remote-agent-session-runtime")).expanduser()


def paths(state):
    runtime = private_dir(Path(os.environ.get("REMOTE_AGENT_SESSION_RUNTIME_DIR") or str(registry_directory())).expanduser())
    key = hashlib.sha256(str(state).casefold().encode()).hexdigest()[:20]
    return runtime, runtime / (key + ".control.json"), runtime / (key + ".lock")


def registry_file(path, flags):
    if not path.exists() and flags & os.O_CREAT:
        try:
            private_file(path, b"")
        except FileExistsError:
            pass
    secure_path(path)
    return os.open(path, flags, 0o600)


def client_roots():
    roots = {canonical(Path.home() / ".remote-agent-session")}
    for name in ("REMOTE_AGENT_SESSION_STATE_DIR", "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR"):
        if os.environ.get(name):
            roots.add(canonical(os.environ[name]))
    directory = registry_directory()
    if directory.exists():
        secure_path(directory)
        roots.update(canonical(root) for root in read_service_roots(directory, "client-roots.json"))
    return roots


def update_service_registry(state, remove=False):
    directory = private_dir(registry_directory())
    with exclusive_lock(directory / "service-roots.lock"):
        roots = read_service_roots(directory)
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
        else:
            validate_service_state(state)
            private_dir(state)
            marker = state / ".remote-agent-session-service-root"
            if not marker.exists():
                private_file(marker, b'{"schema":1}\n')
            if value not in roots:
                roots.append(value)
        atomic_json(directory / "service-roots.json", {"schema": 1, "roots": sorted(roots)})
    return {"service_root_registered": not remove, "service_state_dir": value}


def read_endpoint(path):
    secure_path(path)
    if path.stat().st_size > 8192:
        raise ValueError("Control endpoint metadata too large")
    value = json.loads(path.read_text())
    if value.get("protocol") != 1 or type(value.get("port")) is not int or not 1 <= value["port"] <= 65535 or not isinstance(value.get("token"), str) or len(value["token"]) != 64:
        raise ValueError("Invalid private control endpoint metadata")
    return value


def request(state, command):
    _, endpoint, _ = paths(state)
    value = read_endpoint(endpoint)
    with socket.create_connection(("127.0.0.1", value["port"]), timeout=3) as client:
        client.sendall((json.dumps({"token": value["token"], "payload": command}) + "\n").encode())
        payload = client.recv(65536)
        if not payload:
            raise ConnectionRefusedError("Service control endpoint closed")
        reply = json.loads(payload)
        if reply.get("instance_id") != value["instance_id"]:
            raise ValueError("Service control instance mismatch")
        return reply


def authenticated_reply(payload, token, status, owner):
    try:
        envelope = json.loads(payload)
    except (ValueError, RecursionError):
        return None, False
    candidate = envelope.get("token") if isinstance(envelope, dict) else None
    if not isinstance(candidate, str) or len(candidate) != 64 or any(character not in "0123456789abcdef" for character in candidate):
        return None, False
    if not hmac.compare_digest(candidate, token):
        return None, False
    command = envelope.get("payload")
    return control_reply(command if isinstance(command, str) else json.dumps(command), status, owner)


def pg_binary(name):
    filename = name + ".exe"
    bindir = os.environ.get("REMOTE_AGENT_SESSION_PG_BIN")
    candidate = Path(bindir) / filename if bindir else None
    if candidate and candidate.is_file():
        return str(candidate)
    result = shutil.which(filename)
    if result:
        return result
    config = shutil.which("pg_config.exe")
    if config:
        candidate = Path(subprocess.check_output([config, "--bindir"], text=True).strip()) / filename
        if candidate.is_file():
            return str(candidate)
    raise RuntimeError("PostgreSQL executable missing: " + filename)


def python_binary():
    candidate = ROOT / ".venv/Scripts/python.exe"
    return str(candidate) if candidate.exists() else sys.executable


def database_settings(state):
    path = state / "postgres-connection.json"
    if path.exists():
        secure_path(path)
        value = json.loads(path.read_text())
        if value.get("user") != "remote_agent_session" or not isinstance(value.get("password"), str) or len(value["password"]) < 32 or type(value.get("port")) is not int or not 1 <= value["port"] <= 65535:
            raise RuntimeError("Invalid private PostgreSQL connection metadata")
        return value
    with socket.socket() as probe:
        probe.bind(("127.0.0.1", 0))
        port = probe.getsockname()[1]
    value = {"user": "remote_agent_session", "password": secrets.token_hex(32), "port": port}
    atomic_json(path, value)
    return value


def environment(state, mode):
    settings = database_settings(state)
    env = dict(os.environ)
    env.update(REMOTE_AGENT_SESSION_MODE=mode, REMOTE_AGENT_SESSION_SERVICE_STATE_DIR=str(state), REMOTE_AGENT_SESSION_KEY_FILE=str(state / "encryption.key"), REMOTE_AGENT_SESSION_FEISHU_CONFIG=str(state / "feishu.env"), PYTHONUNBUFFERED="1")
    env["REMOTE_AGENT_SESSION_DATABASE_URL"] = "postgresql://" + settings["user"] + ":" + quote(settings["password"], safe="") + "@127.0.0.1:" + str(settings["port"]) + "/remote_agent_session"
    return env


def config_app(state, mode):
    if mode != "live":
        return None
    config = secure_path(state / "feishu.env", protect_private_inheritance=True)
    values = dict(line.split("=", 1) for line in config.read_text().splitlines() if "=" in line and not line.lstrip().startswith("#"))
    if not values.get("LARK_APP_ID") or not values.get("LARK_APP_SECRET"):
        raise RuntimeError("feishu.env must contain LARK_APP_ID and LARK_APP_SECRET")
    return values["LARK_APP_ID"].strip()


def startup_record(state, instance, phase, error=""):
    atomic_json(state / "startup-status.json", {"instance_id": instance, "phase": phase, "error": error})


class OwnedJob:
    def __init__(self):
        class Basic(ctypes.Structure):
            _fields_ = [("PerProcessUserTimeLimit", ctypes.c_int64), ("PerJobUserTimeLimit", ctypes.c_int64), ("LimitFlags", wintypes.DWORD), ("MinimumWorkingSetSize", ctypes.c_size_t), ("MaximumWorkingSetSize", ctypes.c_size_t), ("ActiveProcessLimit", wintypes.DWORD), ("Affinity", ctypes.c_size_t), ("PriorityClass", wintypes.DWORD), ("SchedulingClass", wintypes.DWORD)]
        class IO(ctypes.Structure):
            _fields_ = [(name, ctypes.c_uint64) for name in ("ReadOperationCount", "WriteOperationCount", "OtherOperationCount", "ReadTransferCount", "WriteTransferCount", "OtherTransferCount")]
        class Extended(ctypes.Structure):
            _fields_ = [("BasicLimitInformation", Basic), ("IoInfo", IO), ("ProcessMemoryLimit", ctypes.c_size_t), ("JobMemoryLimit", ctypes.c_size_t), ("PeakProcessMemoryUsed", ctypes.c_size_t), ("PeakJobMemoryUsed", ctypes.c_size_t)]
        self.api = ctypes.WinDLL("kernel32", use_last_error=True)
        self.api.CreateJobObjectW.restype = wintypes.HANDLE
        self.api.CreateJobObjectW.argtypes = [ctypes.c_void_p, wintypes.LPCWSTR]
        self.api.SetInformationJobObject.argtypes = [wintypes.HANDLE, ctypes.c_int, ctypes.c_void_p, wintypes.DWORD]
        self.api.AssignProcessToJobObject.argtypes = [wintypes.HANDLE, wintypes.HANDLE]
        self.api.GetCurrentProcess.restype = wintypes.HANDLE
        self.api.CloseHandle.argtypes = [wintypes.HANDLE]
        self.handle = self.api.CreateJobObjectW(None, None)
        settings = Extended()
        settings.BasicLimitInformation.LimitFlags = 0x2000
        if not self.handle or not self.api.SetInformationJobObject(self.handle, 9, ctypes.byref(settings), ctypes.sizeof(settings)) or not self.api.AssignProcessToJobObject(self.handle, self.api.GetCurrentProcess()):
            if self.handle:
                self.api.CloseHandle(self.handle)
            raise RuntimeError("Cannot establish Windows owned-process job; refusing unmanaged service startup")

    def close(self):
        if self.handle:
            self.api.CloseHandle(self.handle)
            self.handle = None


def autostart(state, mode, port, remove):
    if remove:
        return {"autostart": False}
    raise RuntimeError("Windows login autostart is not supported; use start to launch the private backend")


def install(namespace):
    globals().update({key: value for key, value in namespace.items() if key in ("ROOT", "canonical", "validate_service_state", "read_service_roots", "control_reply", "startup_remaining", "wait_ready")})
    for name in ("registry_directory", "private_dir", "private_file", "paths", "registry_file", "client_roots", "update_service_registry", "request", "pg_binary", "python_binary", "environment", "config_app", "startup_record", "run", "launch_service", "autostart"):
        namespace[name] = globals()[name]


def run(state, mode, port):
    deadline = float(os.environ.get("REMOTE_AGENT_SESSION_STARTUP_DEADLINE", time.monotonic() + 180))
    instance = os.environ.get("REMOTE_AGENT_SESSION_INSTANCE_ID") or uuid.uuid4().hex
    phase = "initialization"
    validate_service_state(state)
    update_service_registry(state)
    runtime, endpoint, lockpath = paths(state)
    with contextlib.ExitStack() as stack:
        stack.enter_context(exclusive_lock(lockpath, blocking=False))
        app = config_app(state, mode)
        if app:
            stack.enter_context(exclusive_lock(private_dir(registry_directory()) / ("feishu-" + hashlib.sha256(app.encode()).hexdigest() + ".lock"), blocking=False))
        job = OwnedJob()
        children = []
        stopping = False
        def stop_signal(signum, frame):
            nonlocal stopping
            stopping = True
        signal.signal(signal.SIGTERM, stop_signal)
        signal.signal(signal.SIGINT, stop_signal)
        if not (state / "encryption.key").exists():
            private_file(state / "encryption.key", base64.urlsafe_b64encode(os.urandom(32)))
        secure_path(state / "encryption.key")
        env = environment(state, mode)
        env["REMOTE_AGENT_SESSION_INSTANCE_ID"] = instance
        settings = database_settings(state)
        pg_env = {key: value for key, value in env.items() if not key.startswith("PG")}
        pg_env.update(PGPASSWORD=settings["password"], PGUSER=settings["user"], PGHOST="127.0.0.1", PGPORT=str(settings["port"]), PGSSLMODE="disable")
        logpath = state / "service.log"
        if not logpath.exists():
            private_file(logpath, b"")
        secure_path(logpath)
        data = state / "postgres"
        token = secrets.token_hex(32)
        published = False
        pg = None
        with open(logpath, "ab", buffering=0) as log, socket.socket() as server:
            def launch(args, cwd=None):
                child = subprocess.Popen(args, cwd=cwd, env=pg_env, stdin=subprocess.DEVNULL, stdout=log, stderr=log)
                children.append(child)
                return child
            def checked(args, cwd=None):
                child = launch(args, cwd)
                if child.wait(timeout=startup_remaining(deadline, phase)) != 0:
                    raise RuntimeError("Managed initialization failed; inspect private service.log")
                children.remove(child)
            tracked = lambda: [(str(child.args[0]), child) for child in children]
            try:
                phase = "PostgreSQL initialization"
                if not (data / "PG_VERSION").exists():
                    password_file = state / ".initdb-password"
                    password_file.unlink(missing_ok=True)
                    private_file(password_file, (settings["password"] + "\n").encode())
                    try:
                        checked([pg_binary("initdb"), "-D", str(data), "-U", settings["user"], "--pwfile", str(password_file), "--auth=scram-sha-256", "--no-locale", "--encoding=UTF8"])
                    finally:
                        password_file.unlink(missing_ok=True)
                    secure_path(data, create=True)
                secure_path(data)
                pg = launch([pg_binary("postgres"), "-D", str(data), "-h", "127.0.0.1", "-p", str(settings["port"]), "-c", "unix_socket_directories=", "-c", "password_encryption=scram-sha-256"])
                phase = "PostgreSQL readiness"
                def pg_probe(timeout):
                    return subprocess.run([pg_binary("psql"), "-X", "-d", "postgres", "-tAc", "SELECT 1"], env=pg_env, timeout=timeout, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True).stdout.strip() == "1"
                wait_ready(phase, tracked, pg_probe, deadline, lambda: stopping)
                phase = "database preparation"
                exists = subprocess.check_output([pg_binary("psql"), "-X", "-d", "postgres", "-tAc", "SELECT 1 FROM pg_database WHERE datname='remote_agent_session'"], env=pg_env, stderr=log, text=True, timeout=min(10, startup_remaining(deadline, phase))).strip()
                if exists != "1":
                    checked([pg_binary("createdb"), "remote_agent_session"])
                phase = "schema migration"
                checked([python_binary(), "-m", "alembic", "upgrade", "head"], ROOT / "server")
                launch([python_binary(), "-m", "uvicorn", "remote_agent_session.api:create_app", "--factory", "--host", "127.0.0.1", "--port", str(port)])
                launch([python_binary(), "-m", "remote_agent_session.monitor"])
                if mode == "live":
                    launch([python_binary(), "-m", "remote_agent_session.feishu_events"])
                phase = "API readiness"
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                def api_probe(timeout):
                    with opener.open("http://127.0.0.1:" + str(port) + "/health", timeout=timeout) as response:
                        body = json.loads(response.read(8192))
                        return response.status == 200 and body.get("status") == "ok" and body.get("instance_id") == instance
                wait_ready(phase, tracked, api_probe, deadline, lambda: stopping)
                server.bind(("127.0.0.1", 0))
                server.listen(4)
                server.settimeout(0.25)
                atomic_json(endpoint, {"protocol": 1, "port": server.getsockname()[1], "token": token, "instance_id": instance})
                published = True
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
                            payload = b""
                            while b"\n" not in payload and len(payload) < 16384:
                                chunk = client.recv(16384 - len(payload))
                                if not chunk:
                                    break
                                payload += chunk
                            status = {"running": True, "ready": True, "mode": mode, "api_url": "http://127.0.0.1:" + str(port), "pid": os.getpid(), "instance_id": instance}
                            reply, should_stop = authenticated_reply(payload.decode(errors="replace"), token, status, ROOT.resolve())
                            if reply is None:
                                continue
                            stopping = stopping or should_stop
                            client.sendall(json.dumps(reply).encode())
                        except (OSError, ValueError):
                            continue
            except Exception as exc:
                startup_record(state, instance, phase, str(exc) if isinstance(exc, RuntimeError) else type(exc).__name__)
                raise
            finally:
                for child in reversed(children):
                    if child is not pg and child.poll() is None:
                        child.terminate()
                if pg is not None and pg.poll() is None:
                    try:
                        subprocess.run([pg_binary("pg_ctl"), "-D", str(data), "stop", "-m", "fast", "-w", "-t", "10"], env=pg_env, stdin=subprocess.DEVNULL, stdout=log, stderr=log, timeout=15)
                    except (OSError, subprocess.TimeoutExpired):
                        pass
                for child in reversed(children):
                    try:
                        child.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        child.kill()
                        child.wait()
                if published:
                    endpoint.unlink(missing_ok=True)
                job.close()


def launch_service_locked(state, mode, port):
    validate_service_state(state)
    update_service_registry(state)
    try:
        current = request(state, "status")
    except (OSError, ValueError):
        current = None
    if current:
        if current.get("mode") != mode or current.get("api_url") != "http://127.0.0.1:" + str(port):
            raise RuntimeError("Service already runs with different mode or port; restart explicitly to change it")
        return current
    with exclusive_lock(paths(state)[2]):
        pass
    deadline = time.monotonic() + 180
    instance = uuid.uuid4().hex
    env = dict(os.environ, REMOTE_AGENT_SESSION_STARTUP_DEADLINE=str(deadline), REMOTE_AGENT_SESSION_INSTANCE_ID=instance)
    logpath = state / "supervisor.log"
    if not logpath.exists():
        private_file(logpath, b"")
    secure_path(logpath)
    with open(logpath, "ab") as log:
        child = subprocess.Popen([python_binary(), str(ROOT / "tools/lifecycle.py"), "run", "--service-state-dir", str(state), "--mode", mode, "--port", str(port)], env=env, stdin=subprocess.DEVNULL, stdout=log, stderr=log, creationflags=subprocess.DETACHED_PROCESS | subprocess.CREATE_NEW_PROCESS_GROUP)
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
                cause = result.get("phase", "initialization") + ": " + result.get("error", "supervisor exited before readiness") if result.get("instance_id") == instance else "supervisor exited before readiness"
                raise RuntimeError("Service startup failed: " + cause + "; inspect private startup-status.json and logs")
            time.sleep(0.2)
        raise RuntimeError("Service startup deadline exhausted; inspect private startup-status.json and logs")
    except Exception:
        if child.poll() is None:
            child.terminate()
            child.wait(timeout=10)
        raise


def launch_service(state, mode, port):
    runtime, endpoint, lockpath = paths(state)
    with exclusive_lock(runtime / (lockpath.stem + ".startup.lock")):
        return launch_service_locked(state, mode, port)
