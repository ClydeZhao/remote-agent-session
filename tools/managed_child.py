#!/usr/bin/env python3
import argparse
import os
import signal
import subprocess
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--parent", required=True, type=int)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    stopping = False
    def stop(signum, frame):
        nonlocal stopping
        stopping = True
    signal.signal(signal.SIGTERM, stop)
    signal.signal(signal.SIGINT, stop)
    command = args.command[1:] if args.command[:1] == ["--"] else args.command
    child = subprocess.Popen(command, start_new_session=True)
    try:
        while child.poll() is None and not stopping and os.getppid() == args.parent:
            time.sleep(0.1)
    finally:
        if child.poll() is None:
            os.killpg(child.pid, signal.SIGINT if os.path.basename(command[0]) == "postgres" else signal.SIGTERM)
            try:
                child.wait(timeout=8)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
    return child.returncode


if __name__ == "__main__":
    raise SystemExit(main())
