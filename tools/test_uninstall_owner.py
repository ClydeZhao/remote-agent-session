import importlib.util
import json
import io
from contextlib import redirect_stdout
import os
from pathlib import Path
import plistlib
import shutil
import shlex
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("lifecycle_owner", Path(__file__).with_name("lifecycle.py"))
lifecycle = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(lifecycle)


class UninstallOwnerTests(unittest.TestCase):
    def test_only_valid_matching_owner_can_request_owned_stop(self):
        owner = Path('/tmp/ras-owner-B').resolve()
        valid = {"protocol": 1, "command": "stop-owned", "expected_installation": str(owner)}
        for payload in ['stop', 'stop-owned', '{', '{}', json.dumps({"command": "stop"}), json.dumps({**valid, "expected_installation": '/tmp/ras-owner-A'}), json.dumps({**valid, "expected_installation": None}), json.dumps({**valid, "protocol": True})]:
            with self.subTest(payload=payload):
                reply, stop = lifecycle.control_reply(payload, {"running": True}, owner)
                self.assertFalse(stop)
                self.assertFalse(reply['stop_accepted'])
        self.assertTrue(lifecycle.control_reply(json.dumps(valid), {}, owner)[1])
        self.assertTrue(lifecycle.control_reply(json.dumps({'protocol': 1, 'command': 'stop', 'intent': 'explicit-user-stop'}), {}, owner)[1])

    def test_legacy_supervisor_reply_never_falls_back_to_unconditional_stop(self):
        with patch.object(lifecycle, 'request', return_value={"running": True}) as request:
            result = lifecycle.stop_owned(Path('/tmp/unused-state'))
        self.assertEqual(result['stop_skipped'], 'supervisor_ownership_protocol_unavailable')
        self.assertEqual(request.call_count, 1)
        self.assertEqual(request.call_args.args[1]['command'], 'stop-owned')

    def test_current_explicit_stop_uses_deliberate_service_wide_protocol(self):
        with patch.object(sys, 'argv', ['lifecycle.py', 'stop', '--service-state-dir', '/tmp/unrelated-owner-service']), patch.object(lifecycle, 'request', side_effect=[{'stop_accepted': True}, FileNotFoundError()]) as request:
            with redirect_stdout(io.StringIO()):
                lifecycle.main()
        self.assertEqual(request.call_args_list[0].args[1], {'protocol': 1, 'command': 'stop', 'intent': 'explicit-user-stop'})

    def test_A_uninstall_preserves_B_and_B_uninstall_stops_own_supervisor(self):
        with tempfile.TemporaryDirectory(prefix='ras-owner-', dir='/tmp') as temp:
            base = Path(temp).resolve()
            home = base / 'home'
            home.mkdir(mode=0o700)
            runtime = base / 'runtime'
            state = base / 'service'
            source = Path(__file__).resolve().parent
            installs = []
            for name in ('A', 'B'):
                install = base / name
                (install / 'tools').mkdir(parents=True)
                (install / '.venv/bin').mkdir(parents=True)
                (install / 'install').mkdir()
                shutil.copy2(source / 'lifecycle.py', install / 'tools/lifecycle.py')
                shutil.copy2(source.parent / 'install/uninstall.sh', install / 'install/uninstall.sh')
                (install / '.venv/bin/python').symlink_to(sys.executable)
                (install / '.installation-complete').write_text('remote-agent-session-install-v1\n')
                installs.append(install)
            env = dict(os.environ, HOME=str(home), REMOTE_AGENT_SESSION_RUNTIME_DIR=str(runtime))
            with patch.dict(os.environ, env):
                _, endpoint, _ = lifecycle.paths(state)
            ready = threading.Event()
            stopped = threading.Event()
            failures = []
            def supervisor():
                try:
                    with socket.socket(socket.AF_UNIX) as server:
                        server.bind(str(endpoint)); server.listen(5); server.settimeout(10)
                        ready.set()
                        while not stopped.is_set():
                            client, _ = server.accept()
                            with client:
                                payload = client.recv(16384).decode()
                                reply, stop = lifecycle.control_reply(payload, {"running": True, "pid": os.getpid()}, installs[1])
                                client.sendall(json.dumps(reply).encode())
                                if stop:
                                    stopped.set()
                    endpoint.unlink(missing_ok=True)
                except Exception as error:
                    failures.append(error)
                    ready.set()
            worker = threading.Thread(target=supervisor, daemon=True)
            worker.start()
            self.assertTrue(ready.wait(10))
            try:
                legacy = base / 'legacy-uninstall.sh'
                legacy_code = "import socket,sys; s=socket.socket(socket.AF_UNIX); s.connect(sys.argv[1]); s.sendall(bytes([115,116,111,112])); print(s.recv(65536).decode())"
                legacy.write_text("#!/bin/sh\nset -eu\n" + shlex.join([sys.executable, "-c", legacy_code, str(endpoint)]) + "\n")
                result = subprocess.run(['sh', str(legacy)], env=env, capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertFalse(json.loads(result.stdout)['stop_accepted'])
                self.assertFalse(stopped.is_set())
                for index, install in enumerate(installs):
                    result = subprocess.run(['sh', str(install / 'install/uninstall.sh'), '--service-state-dir', str(state)], env=env, capture_output=True, text=True, timeout=30)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    if index == 0:
                        self.assertIn('installation_mismatch', result.stdout)
                        self.assertFalse(stopped.is_set())
                        self.assertTrue(worker.is_alive())
                    else:
                        self.assertTrue(stopped.wait(5))
                worker.join(timeout=5)
                self.assertFalse(worker.is_alive())
                self.assertFalse(failures)
            finally:
                if worker.is_alive():
                    with patch.dict(os.environ, env):
                        lifecycle.request(state, {'protocol': 1, 'command': 'stop', 'intent': 'explicit-user-stop'})
                    worker.join(timeout=5)

    def test_other_or_malformed_autostart_is_preserved(self):
        with tempfile.TemporaryDirectory(prefix='ras-launch-owner-', dir='/tmp') as temp:
            home = Path(temp)
            state = home / 'service'
            label = 'io.remote-agent-session.' + lifecycle.hashlib.sha256(str(state).encode()).hexdigest()[:12]
            target = home / 'Library/LaunchAgents' / (label + '.plist')
            target.parent.mkdir(parents=True)
            for value in (plistlib.dumps({'ProgramArguments': ['/usr/bin/python3', '/other/install/tools/lifecycle.py']}), b'malformed'):
                target.write_bytes(value)
                with patch.dict(os.environ, {'HOME': temp}), patch.object(sys, 'platform', 'darwin'), patch.object(lifecycle.subprocess, 'run') as run:
                    result = lifecycle.autostart(state, 'offline', 18481, True)
                self.assertFalse(result['autostart_removed'])
                self.assertEqual(target.read_bytes(), value)
                run.assert_not_called()


if __name__ == '__main__':
    unittest.main()
