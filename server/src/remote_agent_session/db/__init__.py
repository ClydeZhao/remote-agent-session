from contextlib import contextmanager

import psycopg
from psycopg.rows import dict_row


class Database:
    def __init__(self, url: str):
        self.url = url

    def bind_mode(self, mode):
        if mode not in ("offline", "live"):
            raise ValueError("Unknown environment mode")
        with self.transaction() as c:
            c.execute(
                "INSERT INTO public.remote_agent_session_environment(singleton,mode) VALUES (true,%s) ON CONFLICT DO NOTHING",
                (mode,),
            )
            actual = c.execute(
                "SELECT mode FROM public.remote_agent_session_environment"
            ).fetchone()["mode"]
            if actual != mode:
                raise RuntimeError("Database mode mismatch; use a separate database")

    @contextmanager
    def transaction(self):
        with psycopg.connect(
            self.url,
            row_factory=dict_row,
            connect_timeout=5,
            options="-c statement_timeout=5000 -c lock_timeout=1000",
        ) as conn:
            yield conn
