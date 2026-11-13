import logging
import signal
import threading

import psycopg

from remote_agent_session.application import Application
from remote_agent_session.settings import Settings


def run(settings, stop=None):
    stop = stop or threading.Event()
    app = Application(settings)
    with psycopg.connect(settings.database_url, autocommit=True) as lock:
        if not lock.execute("SELECT pg_try_advisory_lock(781342101)").fetchone()[0]:
            raise RuntimeError("Another monitor owns this database")
        while not stop.is_set():
            lock.execute("SELECT 1")
            try:
                busy = app.session_notify_once()
            except Exception:
                logging.error("Session notification failed; durable state will recover")
                busy = False
            stop.wait(0.05 if busy else 1)


def main():
    stop = threading.Event()
    signal.signal(signal.SIGTERM, lambda *_: stop.set())
    signal.signal(signal.SIGINT, lambda *_: stop.set())
    run(Settings.load(), stop)


if __name__ == "__main__":
    main()
