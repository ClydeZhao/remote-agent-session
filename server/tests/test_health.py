from contextlib import contextmanager

import psycopg
from cryptography.fernet import Fernet
from fastapi.testclient import TestClient

from remote_agent_session.api import create_app
from remote_agent_session.settings import Settings


def test_health_reports_nonce_captured_at_startup(app, monkeypatch):
    monkeypatch.setenv("REMOTE_AGENT_SESSION_INSTANCE_ID", "synthetic-startup-nonce")
    candidate = create_app(Settings(app.state.application.db.url, Fernet.generate_key()))
    monkeypatch.setenv("REMOTE_AGENT_SESSION_INSTANCE_ID", "different-process-nonce")
    with TestClient(candidate) as client:
        assert client.get("/health").json() == {
            "status": "ok",
            "instance_id": "synthetic-startup-nonce",
        }
    assert "/health" not in candidate.openapi()["paths"]


def test_health_does_not_report_ready_when_storage_is_unavailable(app, client, monkeypatch):
    @contextmanager
    def unavailable():
        raise psycopg.OperationalError("synthetic storage unavailable")
        yield

    monkeypatch.setattr(app.state.application.db, "transaction", unavailable)
    response = client.get("/health")
    assert response.status_code == 503
    assert response.json()["code"] == "STORAGE_UNAVAILABLE"
    assert "instance_id" not in response.json()
