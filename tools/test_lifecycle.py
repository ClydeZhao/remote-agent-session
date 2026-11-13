import importlib.util
import hashlib
import json
import io
import plistlib
from contextlib import redirect_stdout
import os
from pathlib import Path
import socket
import shutil
import time
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("lifecycle", Path(__file__).with_name("lifecycle.py"))
lifecycle = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(lifecycle)


class LifecycleTests(unittest.TestCase):
    def test_service_and_client_defaults_remain_separate(self):
        with tempfile.TemporaryDirectory() as temp:
            env = {"HOME": temp, "REMOTE_AGENT_SESSION_STATE_DIR": str(Path(temp) / "custom-client"), "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR": "", "REMOTE_AGENT_SESSION_SERVICE_STATE_DIR": "", "DSH_HOME": "", "REMOTE_AGENT_SESSION_RUNTIME_DIR": str(Path(temp) / "runtime")}
            with patch.dict(os.environ, env), patch.object(sys, "argv", ["lifecycle.py", "doctor", "--mode", "offline"]), patch.object(lifecycle, "pg_binary", return_value="postgres"):
                os.environ.pop("REMOTE_AGENT_SESSION_SERVICE_STATE_DIR")
                output = io.StringIO()
                with redirect_stdout(output):
                    lifecycle.main()
                result = json.loads(output.getvalue())
                self.assertEqual(Path(result["service_state_dir"]), (Path(temp) / ".remote-agent-session-service").resolve())
                self.assertFalse((Path(temp) / "custom-client").exists())

    def test_service_rejects_client_equality_ancestors_descendants_and_aliases(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            client = home / "client"
            client.mkdir()
            alias = home / "alias"
            alias.symlink_to(client, target_is_directory=True)
            env = {"HOME": temp, "REMOTE_AGENT_SESSION_STATE_DIR": str(client), "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR": "", "DSH_HOME": ""}
            with patch.dict(os.environ, env):
                self.assertEqual(lifecycle.validate_service_state(home / "service"), (home / "service").resolve())
                for path in (client, client / "backend", home, alias, alias / "backend"):
                    with self.subTest(path=path), self.assertRaisesRegex(RuntimeError, "separate"):
                        lifecycle.validate_service_state(path)

    def test_service_rejects_effective_client_before_creating_files(self):
        with tempfile.TemporaryDirectory() as temp:
            client = Path(temp) / "client"
            service = client / "backend"
            env = dict(os.environ, HOME=temp, REMOTE_AGENT_SESSION_STATE_DIR="", REMOTE_AGENT_SESSION_CLIENT_STATE_DIR=str(client), DSH_HOME="")
            result = subprocess.run([sys.executable, str(Path(lifecycle.__file__)), "start", "--service-state-dir", str(service), "--mode", "offline"], env=env, text=True, capture_output=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn("separate", result.stderr)
            self.assertFalse(client.exists())

    def test_registered_client_grants_and_pending_grants_block_service(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            dsh = home / "custom-dsh"
            profile = dsh / "profiles" / "desktop"
            profile.mkdir(parents=True)
            grant = home / "previous-client"
            record = {"grants": {str(grant): {"owners": {"older-install": {"state_directory": str(grant)}}}}}
            env = {"HOME": temp, "REMOTE_AGENT_SESSION_STATE_DIR": "", "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR": "", "DSH_HOME": str(dsh)}
            with patch.dict(os.environ, env):
                for name, data in ((".remote-agent-session-permissions.json", record), (".remote-agent-session-permissions-pending.json", {"after_record": record})):
                    path = profile / name
                    path.write_text(json.dumps(data))
                    with self.assertRaisesRegex(RuntimeError, "separate"):
                        lifecycle.validate_service_state(grant / "backend")
                    path.unlink()

    def test_legacy_combined_service_can_still_be_inspected_without_migration(self):
        with tempfile.TemporaryDirectory() as temp:
            client = Path(temp) / ".remote-agent-session"
            env = dict(os.environ, HOME=temp, REMOTE_AGENT_SESSION_STATE_DIR=str(client), REMOTE_AGENT_SESSION_RUNTIME_DIR=str(Path(temp) / "runtime"))
            result = subprocess.run([sys.executable, str(Path(lifecycle.__file__)), "status", "--service-state-dir", str(client)], env=env, text=True, capture_output=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(result.stdout), {"running": False})
            self.assertFalse(client.exists())

    def test_autostart_records_service_path_and_client_validation_context(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            service = home / "service"
            client = home / "client"
            env = {"HOME": temp, "REMOTE_AGENT_SESSION_STATE_DIR": str(client), "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR": str(client), "DSH_HOME": ""}
            with patch.dict(os.environ, env), patch.object(sys, "platform", "darwin"), patch.object(lifecycle, "pg_binary", return_value="/fake/bin/postgres"), patch.object(lifecycle, "request", side_effect=FileNotFoundError()), patch.object(lifecycle.subprocess, "run"):
                self.assertEqual(lifecycle.autostart(service, "offline", 18481, False), {"autostart": True})
                files = list((home / "Library/LaunchAgents").glob("*.plist"))
                self.assertEqual(len(files), 1)
                definition = plistlib.loads(files[0].read_bytes())
                args = definition["ProgramArguments"]
                self.assertIn("--service-state-dir", args)
                self.assertNotIn("--state-dir", args)
                self.assertEqual(args[args.index("--service-state-dir") + 1], str(service))
                self.assertEqual(definition["EnvironmentVariables"]["REMOTE_AGENT_SESSION_CLIENT_STATE_DIR"], str(client))

    def test_service_registry_reserves_custom_root_before_secret_creation(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            state = (home / "custom" / "service").resolve()
            env = {"HOME": temp, "REMOTE_AGENT_SESSION_STATE_DIR": "", "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR": "", "DSH_HOME": "", "REMOTE_AGENT_SESSION_RUNTIME_DIR": str(home / "custom-runtime")}
            with patch.dict(os.environ, env):
                lifecycle.update_service_registry(state)
                registry = home / ".remote-agent-session-runtime/service-roots.json"
                data = json.loads(registry.read_text())
                self.assertEqual(data, {"schema": 1, "roots": [str(state)]})
                self.assertEqual(registry.stat().st_mode & 0o777, 0o600)
                self.assertEqual(list(path.name for path in state.iterdir()), [".remote-agent-session-service-root"])
                with self.assertRaisesRegex(RuntimeError, "data remains"):
                    lifecycle.update_service_registry(state, remove=True)
                shutil.rmtree(state)
                lifecycle.update_service_registry(state, remove=True)
                self.assertEqual(json.loads(registry.read_text())["roots"], [])

    def test_persistent_custom_client_grant_blocks_service_from_fresh_shell(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            directory = home / ".remote-agent-session-runtime"
            directory.mkdir(mode=0o700)
            client = (home / "custom-profile/client").resolve()
            registry = directory / "client-roots.json"
            registry.write_text(json.dumps({"schema": 1, "roots": [str(client)]}))
            registry.chmod(0o600)
            env = dict(os.environ, HOME=temp, REMOTE_AGENT_SESSION_STATE_DIR="", REMOTE_AGENT_SESSION_CLIENT_STATE_DIR="", DSH_HOME="")
            service = client / "private-service"
            result = subprocess.run([sys.executable, str(Path(lifecycle.__file__)), "start", "--service-state-dir", str(service), "--mode", "offline"], env=env, text=True, capture_output=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn("separate", result.stderr)
            self.assertFalse(service.exists())
            self.assertFalse((directory / "service-roots.json").exists())

    def test_service_registry_serializes_concurrent_root_registration(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            env = dict(os.environ, HOME=temp, REMOTE_AGENT_SESSION_STATE_DIR="", REMOTE_AGENT_SESSION_CLIENT_STATE_DIR="", DSH_HOME="")
            code = "import runpy,sys,pathlib; module=runpy.run_path(sys.argv[1]); module['update_service_registry'](pathlib.Path(sys.argv[2]))"
            roots = [(home / ("service-" + str(index))).resolve() for index in range(4)]
            children = [subprocess.Popen([sys.executable, "-c", code, str(Path(lifecycle.__file__)), str(root)], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE) for root in roots]
            for child in children:
                output, error = child.communicate(timeout=10)
                self.assertEqual(child.returncode, 0, error.decode())
            data = json.loads((home / ".remote-agent-session-runtime/service-roots.json").read_text())
            self.assertEqual(data["roots"], sorted(str(root) for root in roots))

    def test_registration_rechecks_new_client_grant_under_shared_lock(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            directory = home / ".remote-agent-session-runtime"
            directory.mkdir(mode=0o700)
            profile = home / ".dsh/profiles/desktop"
            profile.mkdir(parents=True)
            service = (home / "custom/client/private-service").resolve()
            ready = home / "ready"
            env = dict(os.environ, HOME=temp, REMOTE_AGENT_SESSION_STATE_DIR="", REMOTE_AGENT_SESSION_CLIENT_STATE_DIR="", DSH_HOME="")
            code = "import pathlib,runpy,sys; m=runpy.run_path(sys.argv[1]); pathlib.Path(sys.argv[3]).write_text('ready'); m['update_service_registry'](pathlib.Path(sys.argv[2]))"
            with (directory / "service-roots.lock").open("a") as lock:
                (directory / "service-roots.lock").chmod(0o600)
                lifecycle.fcntl.flock(lock, lifecycle.fcntl.LOCK_EX)
                child = subprocess.Popen([sys.executable, "-c", code, str(Path(lifecycle.__file__)), str(service), str(ready)], env=env, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
                for _ in range(100):
                    if ready.exists():
                        break
                    time.sleep(0.02)
                self.assertTrue(ready.exists())
                grant = str(service.parent)
                (profile / ".remote-agent-session-permissions.json").write_text(json.dumps({"grants": {grant: {"owners": {}}}}))
            output, error = child.communicate(timeout=10)
            self.assertNotEqual(child.returncode, 0)
            self.assertIn("separate", error.decode())
            self.assertFalse(service.exists())
            self.assertFalse((directory / "service-roots.json").exists())

    def test_service_registry_rejects_unsafe_existing_metadata(self):
        with tempfile.TemporaryDirectory() as temp:
            home = Path(temp)
            directory = home / ".remote-agent-session-runtime"
            directory.mkdir(mode=0o700)
            registry = directory / "service-roots.json"
            registry.write_text('{"schema":1,"roots":[]}')
            registry.chmod(0o644)
            with patch.dict(os.environ, {"HOME": temp, "REMOTE_AGENT_SESSION_STATE_DIR": "", "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR": "", "DSH_HOME": ""}):
                with self.assertRaisesRegex(RuntimeError, "private regular"):
                    lifecycle.update_service_registry((home / "service").resolve())
                self.assertFalse((home / "service").exists())

    def test_readiness_allows_slow_probe_with_shared_deadline(self):
        clock = [0.0]
        child = unittest.mock.Mock()
        child.poll.return_value = None
        probes = []
        def probe(timeout):
            probes.append(timeout)
            clock[0] += 2
            return len(probes) == 2
        def advance(seconds):
            clock[0] += seconds
        with patch.object(lifecycle.time, "monotonic", side_effect=lambda: clock[0]), patch.object(lifecycle.time, "sleep", side_effect=advance):
            lifecycle.wait_ready("API readiness", lambda: [("api", child)], probe, 10)
        self.assertEqual(probes, [5, 5])
        self.assertGreater(clock[0], 4)

    def test_readiness_rejects_other_instance_health_until_deadline(self):
        clock = [0.0]
        def advance(seconds):
            clock[0] += seconds
        with patch.object(lifecycle.time, "monotonic", side_effect=lambda: clock[0]), patch.object(lifecycle.time, "sleep", side_effect=advance):
            with self.assertRaisesRegex(RuntimeError, "deadline.*API instance mismatch"):
                lifecycle.wait_ready("API readiness", lambda: [], lambda timeout: (False, "API instance mismatch"), 1)

    def test_readiness_reports_child_exit_instead_of_timeout(self):
        child = unittest.mock.Mock()
        child.poll.return_value = 7
        with self.assertRaisesRegex(RuntimeError, "api.*exit 7"):
            lifecycle.wait_ready("API readiness", lambda: [("api", child)], lambda timeout: True, lifecycle.time.monotonic() + 10)

    def test_readiness_reports_deadline_and_last_probe_failure(self):
        clock = [0.0]
        def probe(timeout):
            clock[0] += 1
            raise TimeoutError()
        def advance(seconds):
            clock[0] += seconds
        with patch.object(lifecycle.time, "monotonic", side_effect=lambda: clock[0]), patch.object(lifecycle.time, "sleep", side_effect=advance):
            with self.assertRaisesRegex(RuntimeError, "deadline.*API readiness.*TimeoutError"):
                lifecycle.wait_ready("API readiness", lambda: [], probe, 3)

    def test_private_config_rejects_public_credentials(self):
        with tempfile.TemporaryDirectory() as temp:
            state = Path(temp)
            config = state / "feishu.env"
            config.write_text("LARK_APP_ID=test\nLARK_APP_SECRET=private\n")
            config.chmod(0o644)
            with self.assertRaisesRegex(RuntimeError, "0600"):
                lifecycle.config_app(state, "live")
            config.chmod(0o600)
            self.assertEqual(lifecycle.config_app(state, "live"), "test")

    def test_status_uses_private_socket_without_signalling_pid(self):
        with tempfile.TemporaryDirectory() as temp:
            with patch.dict(os.environ, {"REMOTE_AGENT_SESSION_RUNTIME_DIR": temp}):
                state = Path(temp) / "state"
                _, endpoint, _ = lifecycle.paths(state)
                with socket.socket(socket.AF_UNIX) as server:
                    server.bind(str(endpoint))
                    server.listen(1)
                    def answer():
                        client, _ = server.accept()
                        with client:
                            self.assertEqual(client.recv(64), b"status")
                            client.sendall(json.dumps({"running": True}).encode())
                    worker = threading.Thread(target=answer)
                    worker.start()
                    self.assertEqual(lifecycle.request(state, "status"), {"running": True})
                    worker.join(timeout=2)

    def test_shutdown_socket_eof_is_an_inactive_service(self):
        with tempfile.TemporaryDirectory() as temp:
            with patch.dict(os.environ, {"REMOTE_AGENT_SESSION_RUNTIME_DIR": temp}):
                state = Path(temp) / "state"
                _, endpoint, _ = lifecycle.paths(state)
                with socket.socket(socket.AF_UNIX) as server:
                    server.bind(str(endpoint))
                    server.listen(1)
                    def close_without_reply():
                        client, _ = server.accept()
                        with client:
                            client.recv(64)
                    worker = threading.Thread(target=close_without_reply)
                    worker.start()
                    with self.assertRaises(ConnectionRefusedError):
                        lifecycle.request(state, "stop")
                    worker.join(timeout=2)

    def test_separate_states_share_application_lock(self):
        with tempfile.TemporaryDirectory() as temp:
            runtime = Path(temp) / "runtime"
            runtime.mkdir(mode=0o700)
            env = dict(os.environ, HOME=temp, REMOTE_AGENT_SESSION_RUNTIME_DIR=str(runtime), REMOTE_AGENT_SESSION_STATE_DIR="", REMOTE_AGENT_SESSION_CLIENT_STATE_DIR="", DSH_HOME="")
            app = "synthetic-lock-test"
            registry = Path(temp) / ".remote-agent-session-runtime"
            registry.mkdir(mode=0o700)
            lock = registry / ("feishu-" + hashlib.sha256(app.encode()).hexdigest() + ".lock")
            with lock.open("a") as stream:
                lock.chmod(0o600)
                lifecycle.fcntl.flock(stream, lifecycle.fcntl.LOCK_EX | lifecycle.fcntl.LOCK_NB)
                for name in ("first", "second"):
                    env["REMOTE_AGENT_SESSION_RUNTIME_DIR"] = str(runtime / name)
                    state = Path(temp) / name
                    state.mkdir(mode=0o700)
                    config = state / "feishu.env"
                    config.write_text("LARK_APP_ID=" + app + "\nLARK_APP_SECRET=synthetic-only\n")
                    config.chmod(0o600)
                    result = subprocess.run([sys.executable, str(Path(lifecycle.__file__)), "run", "--service-state-dir", str(state), "--mode", "live"], env=env, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 1)
                    self.assertIn("already has a consumer", result.stderr)
                    self.assertFalse((state / "postgres").exists())

    def test_supervisor_death_stops_managed_process_group(self):
        with tempfile.TemporaryDirectory() as temp:
            pidfile = Path(temp) / "child.pid"
            child_code = "import os,time,pathlib; pathlib.Path(" + repr(str(pidfile)) + ").write_text(str(os.getpid())); time.sleep(60)"
            wrapper = str(Path(__file__).with_name("managed_child.py").resolve())
            owner_code = "import os,subprocess,sys,time; subprocess.Popen([sys.executable,sys.argv[1],'--parent',str(os.getpid()),'--',sys.executable,'-c',sys.argv[2]]); time.sleep(60)"
            owner = subprocess.Popen([sys.executable, "-c", owner_code, wrapper, child_code])
            try:
                for _ in range(200):
                    if pidfile.exists() and pidfile.read_text().strip():
                        break
                    time.sleep(0.05)
                self.assertTrue(pidfile.exists() and pidfile.read_text().strip())
                child_pid = int(pidfile.read_text())
                owner.kill()
                owner.wait(timeout=3)
                for _ in range(60):
                    try:
                        os.kill(child_pid, 0)
                    except ProcessLookupError:
                        break
                    time.sleep(0.05)
                else:
                    self.fail("Managed child survived supervisor death")
            finally:
                if owner.poll() is None:
                    owner.terminate()
                    owner.wait(timeout=3)

    def test_postgres_has_private_socket_database(self):
        with tempfile.TemporaryDirectory(prefix="ras space ") as temp:
            env = lifecycle.environment(Path(temp), "offline")
            self.assertIn("%20", env["REMOTE_AGENT_SESSION_DATABASE_URL"])
            self.assertNotIn("PASSWORD", " ".join(env.keys()))
            self.assertEqual(env["REMOTE_AGENT_SESSION_MODE"], "offline")
            self.assertEqual(env["LC_ALL"], "C")


if __name__ == "__main__":
    unittest.main()
