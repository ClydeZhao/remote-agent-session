import fcntl
import os
from pathlib import Path
import signal
import time
from dataclasses import dataclass


@dataclass(frozen=True)
class LinuxProcess:
    pid: int
    start_ticks: int
    state: str


def parse_linux_stat(pid, text):
    prefix, separator, tail = text.rpartition(") ")
    fields = tail.split()
    if not separator or not prefix.startswith(str(pid) + " (") or len(fields) < 20:
        raise AssertionError("Invalid isolated bridge proc stat")
    try:
        return LinuxProcess(pid, int(fields[19]), fields[0])
    except ValueError as error:
        raise AssertionError("Invalid isolated bridge start time") from error


def read_linux_process(pid, proc_root=Path("/proc")):
    try:
        text = (proc_root / str(pid) / "stat").read_text()
    except (FileNotFoundError, ProcessLookupError):
        return None
    return parse_linux_stat(pid, text)


def capture_linux_bridge(pid, state_dir, proc_root=Path("/proc")):
    process = read_linux_process(pid, proc_root)
    if process is None or process.state in ("Z", "X", "x"):
        return process
    try:
        arguments = (proc_root / str(pid) / "cmdline").read_bytes().split(b"\0")
    except (FileNotFoundError, ProcessLookupError):
        return None
    arguments = [os.fsdecode(value) for value in arguments if value]
    current = read_linux_process(pid, proc_root)
    if current is None or current.start_ticks != process.start_ticks:
        return None
    expected = ["--state-dir", str(state_dir), "session", "_serve"]
    if not arguments or Path(arguments[0]).name != "remote-agent-session" or arguments[1:] != expected:
        return None
    return current


def terminate_linux_bridge(process, proc_root=Path("/proc")):
    if process.state in ("Z", "X", "x"):
        return
    try:
        descriptor = os.pidfd_open(process.pid)
    except ProcessLookupError:
        return
    try:
        current = read_linux_process(process.pid, proc_root)
        if current is None or current.start_ticks != process.start_ticks or current.state in ("Z", "X", "x"):
            return
        try:
            signal.pidfd_send_signal(descriptor, signal.SIGTERM)
        except ProcessLookupError:
            pass
    finally:
        os.close(descriptor)


def relay_lock_available(path):
    descriptor = os.open(path, os.O_RDWR | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        try:
            fcntl.flock(descriptor, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return False
        fcntl.flock(descriptor, fcntl.LOCK_UN)
        return True
    finally:
        os.close(descriptor)


def wait_linux_bridges(processes, lock_path, timeout, proc_root=Path("/proc")):
    deadline = time.monotonic() + timeout
    while True:
        live = []
        for original in processes:
            current = read_linux_process(original.pid, proc_root)
            if current is not None and current.start_ticks == original.start_ticks and current.state not in ("Z", "X", "x"):
                live.append(current)
        lock_free = relay_lock_available(lock_path)
        if not live and lock_free:
            return
        if time.monotonic() >= deadline:
            states = [(value.pid, value.start_ticks, value.state) for value in live]
            raise AssertionError(f"Isolated bridge still live or lock held: processes={states}, lock_available={lock_free}")
        time.sleep(0.05)
