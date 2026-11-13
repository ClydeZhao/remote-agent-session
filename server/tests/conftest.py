import os
import subprocess
import tempfile
from pathlib import Path

import pytest
from cryptography.fernet import Fernet
from fastapi.testclient import TestClient

from remote_agent_session.api import create_app
from remote_agent_session.db import Database
from remote_agent_session.settings import Settings


@pytest.fixture(scope="session")
def pg():
    with tempfile.TemporaryDirectory(prefix="ras-pg-") as temp:
        root = Path(temp)
        binary = Path(subprocess.check_output(["pg_config", "--bindir"], text=True).strip())
        socket = root / "socket"
        socket.mkdir()

        def command(*args):
            subprocess.run(
                [str(binary / args[0]), *args[1:]], check=True, capture_output=True, text=True
            )

        command("initdb", "-D", str(root / "data"), "-A", "trust", "--no-locale", "--encoding=UTF8")
        options = f"-k {socket} -p 55449 -c listen_addresses=''"
        command(
            "pg_ctl",
            "-D",
            str(root / "data"),
            "-l",
            str(root / "pg.log"),
            "-o",
            options,
            "-w",
            "start",
        )
        url = f"postgresql://localhost/postgres?host={socket}&port=55449"
        try:
            subprocess.run(
                ["alembic", "upgrade", "head"],
                check=True,
                env={**os.environ, "REMOTE_AGENT_SESSION_DATABASE_URL": url},
            )
            yield dict(url=url, command=command, root=root, options=options)
        finally:
            command("pg_ctl", "-D", str(root / "data"), "-m", "fast", "-w", "stop")


@pytest.fixture
def app(pg):
    db = Database(pg["url"])
    with db.transaction() as c:
        schemas = ("identity", "notifications", "session_relay")
        rows = c.execute(
            "SELECT schemaname,tablename FROM pg_tables WHERE schemaname=ANY(%s)", (list(schemas),)
        ).fetchall()
        if rows:
            from psycopg import sql

            tables = sql.SQL(",").join(
                sql.Identifier(r["schemaname"], r["tablename"]) for r in rows
            )
            c.execute(sql.SQL("TRUNCATE {} CASCADE").format(tables))
    return create_app(Settings(pg["url"], Fernet.generate_key()))


@pytest.fixture
def client(app):
    with TestClient(app) as client:
        yield client
