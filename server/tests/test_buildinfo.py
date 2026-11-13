from remote_agent_session.buildinfo import headers


def test_server_build_identifies_package_and_rejects_header_injection(client, monkeypatch):
    monkeypatch.setenv("REMOTE_AGENT_SESSION_BUILD_REVISION", "commit-123")
    response = client.get("/v1/me")
    assert response.status_code == 401
    assert response.headers["X-Remote-Agent-Session-Server-Build"] == "commit-123"
    assert response.headers["X-Remote-Agent-Session-Server-Version"]
    monkeypatch.setenv("REMOTE_AGENT_SESSION_BUILD_REVISION", "secret\r\nInjected: token")
    assert headers()["X-Remote-Agent-Session-Server-Build"] == "unknown"
