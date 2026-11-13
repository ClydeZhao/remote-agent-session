import fcntl
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

from relay_process import LinuxProcess, capture_linux_bridge, parse_linux_stat, read_linux_process, relay_lock_available, terminate_linux_bridge, wait_linux_bridges


def stat_text(pid, state="S", start=42, name="relay worker (test)"):
    return f"{pid} ({name}) " + " ".join([state] + ["0"] * 18 + [str(start), "0", "0"])


class RelayProcessTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.proc = self.root / "proc"
        self.proc.mkdir()
        self.lock_path = self.root / ".relay-lock"
        self.lock_path.touch(mode=0o600)

    def process(self, pid=123, state="S", start=42, arguments=None):
        directory = self.proc / str(pid)
        directory.mkdir(exist_ok=True)
        (directory / "stat").write_text(stat_text(pid, state, start))
        if arguments is not None:
            (directory / "cmdline").write_bytes(b"\0".join(os.fsencode(value) for value in arguments) + b"\0")
        return LinuxProcess(pid, start, state)

    def test_stat_identity_handles_spaces_and_parentheses(self):
        self.assertEqual(parse_linux_stat(123, stat_text(123, name="x ) spaced (( )")), LinuxProcess(123, 42, "S"))
        with self.assertRaises(AssertionError):
            parse_linux_stat(123, stat_text(456))
        with self.assertRaises(AssertionError):
            parse_linux_stat(123, "123 (broken) S")

    def test_exact_bridge_arguments_required_before_signalling(self):
        arguments = ["/isolated/remote-agent-session", "--state-dir", "/isolated/state", "session", "_serve"]
        expected = self.process(arguments=arguments)
        self.assertEqual(capture_linux_bridge(123, "/isolated/state", self.proc), expected)
        self.assertIsNone(capture_linux_bridge(123, "/different/state", self.proc))
        self.process(arguments=["/usr/bin/other", *arguments[1:]])
        self.assertIsNone(capture_linux_bridge(123, "/isolated/state", self.proc))

    def test_existing_zombie_entry_is_exited_only_after_lock_release(self):
        original = self.process(state="Z")
        with self.lock_path.open("r+") as held:
            fcntl.flock(held, fcntl.LOCK_EX)
            with self.assertRaisesRegex(AssertionError, "lock_available=False"):
                wait_linux_bridges([original], self.lock_path, 0, self.proc)
        wait_linux_bridges([original], self.lock_path, 0, self.proc)
        self.assertTrue((self.proc / "123/stat").exists())

    def test_live_states_never_count_as_exited_even_with_free_lock(self):
        for state in ("R", "S", "D", "T"):
            with self.subTest(state=state):
                process = self.process(state=state)
                with self.assertRaisesRegex(AssertionError, "processes=\\[\\("):
                    wait_linux_bridges([process], self.lock_path, 0, self.proc)

    def test_reused_pid_does_not_keep_original_process_alive(self):
        original = self.process(start=42)
        self.process(start=99)
        wait_linux_bridges([original], self.lock_path, 0, self.proc)

    def test_pidfd_is_not_signalled_after_identity_changes(self):
        original = self.process(start=42)
        self.process(start=99)
        descriptor = os.open(os.devnull, os.O_RDONLY)
        with patch.object(os, "pidfd_open", return_value=descriptor, create=True), patch.object(signal, "pidfd_send_signal", create=True) as send:
            terminate_linux_bridge(original, self.proc)
            send.assert_not_called()
        with self.assertRaises(OSError):
            os.fstat(descriptor)

    @unittest.skipUnless(sys.platform.startswith("linux"), "real Linux procfs and pidfd required")
    def test_real_terminated_zombie_releases_bridge_lock_before_reaping(self):
        script = "import fcntl,sys; f=open(sys.argv[1],'r+'); fcntl.flock(f,fcntl.LOCK_EX); print('locked',flush=True); sys.stdin.readline()"
        child = subprocess.Popen([sys.executable, "-c", script, str(self.lock_path)], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        try:
            self.assertEqual(child.stdout.readline().strip(), "locked")
            self.assertFalse(relay_lock_available(self.lock_path))
            original = read_linux_process(child.pid)
            self.assertIsNotNone(original)
            terminate_linux_bridge(original)
            wait_linux_bridges([original], self.lock_path, 10)
            deadline = time.monotonic() + 2
            while True:
                current = read_linux_process(child.pid)
                if current is not None and current.state == "Z":
                    break
                if time.monotonic() >= deadline:
                    self.fail(f"Expected unreaped terminated child, got {current}")
                time.sleep(0.01)
            self.assertTrue(Path(f"/proc/{child.pid}").exists())
            self.assertTrue(relay_lock_available(self.lock_path))
        finally:
            if child.poll() is None:
                child.terminate()
            child.wait(timeout=5)
            child.stdin.close()
            child.stdout.close()


if __name__ == "__main__":
    unittest.main()
