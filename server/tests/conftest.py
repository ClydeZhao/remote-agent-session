import os
import secrets
import socket as networking
import subprocess
import tempfile
from pathlib import Path
from urllib.parse import quote

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
        binary = Path(os.environ.get("REMOTE_AGENT_SESSION_PG_BIN") or subprocess.check_output(["pg_config", "--bindir"], text=True).strip())
        socket = root / "socket"
        socket.mkdir()

        def command(*args):
            with (root / "commands.log").open("ab", buffering=0) as log:
                result = subprocess.run(
                    [str(binary / args[0]), *args[1:]], stdout=log, stderr=log, timeout=60
                )
            assert result.returncode == 0, (args[0], (root / "commands.log").read_text(errors="replace"))

        if os.name == "nt":
            password = secrets.token_urlsafe(32)
            password_file = root / "password"
            password_file.write_text(password)
            command("initdb", "-D", str(root / "data"), "-U", "postgres", "--pwfile", str(password_file), "-A", "scram-sha-256", "--no-locale", "--encoding=UTF8")
            password_file.unlink()
            with networking.socket() as probe:
                probe.bind(("127.0.0.1", 0))
                port = probe.getsockname()[1]
            options = f"-p {port} -h 127.0.0.1"
            url = f"postgresql://postgres:{quote(password)}@127.0.0.1:{port}/postgres"
        else:
            command("initdb", "-D", str(root / "data"), "-A", "trust", "--no-locale", "--encoding=UTF8")
            options = f"-k {socket} -p 55449 -c listen_addresses=''"
            url = f"postgresql://localhost/postgres?host={socket}&port=55449"
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
