import os

from cryptography.fernet import Fernet

from remote_agent_session.api import create_app
from remote_agent_session.settings import Settings


def test_contract_only_exposes_identity_and_session_relay():
    schema = create_app(schema_only=True).openapi()
    assert all(
        path.startswith(("/v1/auth/", "/v1/session-relay")) or path == "/v1/me"
        for path in schema["paths"]
    )
    assert "/v1/session-relay/notify" not in schema["paths"]
    assert schema["components"]["schemas"]["RelayAttach"]["properties"]["runtime_kind"]["enum"] == [
        "codex",
        "deepseek-harness",
    ]


def test_live_settings_have_no_hpc_or_shared_configuration(tmp_path, monkeypatch):
    key = tmp_path / "key"
    key.write_bytes(Fernet.generate_key())
    feishu = tmp_path / "feishu.env"
    feishu.write_text("LARK_APP_ID=synthetic\nLARK_APP_SECRET=synthetic-secret\n")
    if os.name == "nt":
        from remote_agent_session.windows_security import secure_path

        secure_path(key, create=True)
        secure_path(feishu, create=True)
    else:
        key.chmod(0o600)
        feishu.chmod(0o600)
    for name, value in {
        "MODE": "live",
        "DATABASE_URL": "postgresql://example.invalid/test",
        "KEY_FILE": str(key),
        "FEISHU_CONFIG": str(feishu),
    }.items():
        monkeypatch.setenv("REMOTE_AGENT_SESSION_" + name, value)
    settings = Settings.load()
    assert settings.feishu_app_id == "synthetic"
    assert not hasattr(settings, "known_hosts")
    assert "synthetic-secret" not in repr(settings)


def test_initial_schema_contains_all_relay_migration_fields(app):
    expected = {
        ("identity", "users"): {"display_name"},
        ("session_relay", "bindings"): {"previous_binding", "runtime_kind", "runtime_instance"},
        ("session_relay", "messages"): {
            "platform_message_id",
            "answer_target",
            "runtime_kind",
            "runtime_instance",
            "summary_digest",
        },
        ("session_relay", "interactions"): {
            "reaction_enabled",
            "reaction_next_check",
            "choices",
            "async_question",
        },
        ("notifications", "session_deliveries"): {"device_id", "card", "interaction_id", "kind"},
        ("notifications", "session_activity"): {
            "message_id",
            "platform_message_id",
            "applied",
            "lease_id",
            "next_attempt",
        },
        ("session_relay", "interaction_cancellations"): {
            "id",
            "user_id",
            "device_id",
            "generation",
        },
    }
    with app.state.application.db.transaction() as c:
        for (schema, table), fields in expected.items():
            rows = c.execute(
                "SELECT column_name FROM information_schema.columns WHERE table_schema=%s AND table_name=%s",
                (schema, table),
            ).fetchall()
            assert fields <= {row["column_name"] for row in rows}
        schemas = c.execute(
            "SELECT schema_name FROM information_schema.schemata WHERE schema_name IN ('hpc','analysis','connections')"
        ).fetchall()
        assert not schemas


def test_health_checks_storage(client):
    assert client.get("/health").json() == {"status": "ok", "instance_id": ""}
