import importlib.util
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import threading
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("windows_lifecycle", Path(__file__).with_name("windows_lifecycle.py"))
windows = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(windows)
LIFECYCLE_SPEC = importlib.util.spec_from_file_location("base_lifecycle", Path(__file__).with_name("lifecycle.py"))
lifecycle = importlib.util.module_from_spec(LIFECYCLE_SPEC)
LIFECYCLE_SPEC.loader.exec_module(lifecycle)
windows.control_reply = lifecycle.control_reply
windows.canonical = lifecycle.canonical


class ControlTests(unittest.TestCase):
    def test_unauthenticated_control_cannot_read_status_or_stop(self):
        status = {"running": True, "pid": 12}
        owner = Path(__file__).resolve().parent
        for envelope in ({"payload": "status"}, {"token": "wrong", "payload": {"protocol": 1, "command": "stop", "intent": "explicit-user-stop"}}, []):
            self.assertEqual(windows.authenticated_reply(json.dumps(envelope), "secret", status, owner), (None, False))
        reply, stopped = windows.authenticated_reply(json.dumps({"token": "secret", "payload": "status"}), "secret", status, owner)
        self.assertTrue(reply["running"])
        self.assertFalse(stopped)

    def test_authenticated_stop_preserves_installation_ownership(self):
        owner = Path(__file__).resolve().parent
        for expected, accepted in ((str(owner), True), (str(owner.parent), False)):
            payload = {"token": "secret", "payload": {"protocol": 1, "command": "stop-owned", "expected_installation": expected}}
            reply, stopped = windows.authenticated_reply(json.dumps(payload), "secret", {"running": True}, owner)
            self.assertEqual(stopped, accepted)
            self.assertEqual(reply["stop_accepted"], accepted)

    def test_request_checks_authenticated_instance_using_real_loopback(self):
        with socket.socket() as server:
            server.bind(("127.0.0.1", 0))
            server.listen(1)
            received = []
            def serve():
                with server.accept()[0] as client:
                    received.append(json.loads(client.recv(8192)))
                    client.sendall(b'{"running":true,"instance_id":"replacement"}')
            worker = threading.Thread(target=serve)
            worker.start()
            with patch.object(windows, "paths", return_value=(None, Path("ignored"), None)), patch.object(windows, "read_endpoint", return_value={"port": server.getsockname()[1], "token": "private", "instance_id": "original"}):
                with self.assertRaisesRegex(ValueError, "instance mismatch"):
                    windows.request(Path("state"), "status")
            worker.join(timeout=3)
            self.assertEqual(received, [{"token": "private", "payload": "status"}])

    def test_autostart_boundary(self):
        with self.assertRaisesRegex(RuntimeError, "not supported"):
            windows.autostart(Path("state"), "offline", 18481, False)
        self.assertEqual(windows.autostart(Path("state"), "offline", 18481, True), {"autostart": False})


@unittest.skipUnless(os.name == "nt", "Native Windows ACL and process boundary")
class NativeWindowsTests(unittest.TestCase):
    def test_current_user_acl_rejects_an_added_everyone_grant(self):
        with tempfile.TemporaryDirectory() as temporary:
            state = windows.private_dir(Path(temporary) / "private")
            path = state / "secret"
            windows.private_file(path, b"secret")
            windows.secure_path(path)
            subprocess.run(["icacls", str(path), "/grant", "*S-1-1-0:R"], check=True, stdout=subprocess.DEVNULL)
            with self.assertRaisesRegex(RuntimeError, "current-user-only"):
                windows.secure_path(path)

    def test_loopback_credentials_are_private_and_stable(self):
        with tempfile.TemporaryDirectory() as temporary:
            state = windows.private_dir(Path(temporary) / "private")
            first = windows.database_settings(state)
            self.assertEqual(first, windows.database_settings(state))
            self.assertGreaterEqual(len(first["password"]), 32)
            windows.secure_path(state / "postgres-connection.json")
            windows.ROOT = Path(temporary)
            env = windows.environment(state, "offline")
            self.assertIn("@127.0.0.1:", env["REMOTE_AGENT_SESSION_DATABASE_URL"])
            self.assertNotIn("?host=", env["REMOTE_AGENT_SESSION_DATABASE_URL"])

    def test_process_job_kills_descendant_when_supervisor_dies(self):
        import ctypes
        from ctypes import wintypes
        script = "import importlib.util,subprocess,sys,time; s=importlib.util.spec_from_file_location('w',sys.argv[1]); w=importlib.util.module_from_spec(s); s.loader.exec_module(w); job=w.OwnedJob(); child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(60)']); print(child.pid,flush=True); time.sleep(60)"
        supervisor = subprocess.Popen([sys.executable, "-c", script, str(Path(__file__).with_name("windows_lifecycle.py"))], stdout=subprocess.PIPE, text=True)
        handle = None
        api = ctypes.WinDLL("kernel32", use_last_error=True)
        api.OpenProcess.argtypes = [wintypes.DWORD, wintypes.BOOL, wintypes.DWORD]
        api.OpenProcess.restype = wintypes.HANDLE
        api.WaitForSingleObject.argtypes = [wintypes.HANDLE, wintypes.DWORD]
        api.CloseHandle.argtypes = [wintypes.HANDLE]
        try:
            descendant = int(supervisor.stdout.readline())
            handle = api.OpenProcess(0x100000, False, descendant)
            self.assertTrue(handle)
            supervisor.terminate()
            supervisor.wait(timeout=10)
            self.assertEqual(api.WaitForSingleObject(handle, 10000), 0)
        finally:
            if supervisor.poll() is None:
                supervisor.kill()
                supervisor.wait()
            if handle:
                api.CloseHandle(handle)


if __name__ == "__main__":
    unittest.main()
