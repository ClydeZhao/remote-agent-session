import os
import subprocess

import pytest

from remote_agent_session.private_config import private_file, read_private_env
from remote_agent_session.settings import Settings


def write_private(path, content):
    path.write_text(content)
    if os.name == "nt":
        from remote_agent_session.windows_security import secure_path

        secure_path(path, create=True)
    else:
        path.chmod(0o600)
    return path


def test_private_configuration_reads_literal_credentials(tmp_path):
    path = write_private(tmp_path / "feishu.env", "# credentials\nLARK_APP_ID=synthetic\nLARK_APP_SECRET=$literal;secret\n")
    assert private_file(path) == path
    assert read_private_env(path) == {"LARK_APP_ID": "synthetic", "LARK_APP_SECRET": "$literal;secret"}


def test_private_configuration_rejects_other_principal_access(tmp_path):
    path = write_private(tmp_path / "key", "synthetic-key")
    if os.name == "nt":
        subprocess.run(["icacls", str(path), "/grant", "*S-1-1-0:R"], check=True, capture_output=True)
    else:
        path.chmod(0o644)
    with pytest.raises(ValueError, match="owned private regular file"):
        private_file(path)


def test_private_configuration_rejects_directory(tmp_path):
    with pytest.raises(ValueError, match="owned private regular file"):
        private_file(tmp_path)


def test_private_configuration_rejects_duplicate_fields(tmp_path):
    path = write_private(tmp_path / "feishu.env", "LARK_APP_ID=first\nLARK_APP_ID=second\n")
    with pytest.raises(ValueError, match="Invalid literal"):
        read_private_env(path)


def test_backend_settings_load_private_key_and_live_configuration(tmp_path, monkeypatch):
    key = write_private(tmp_path / "encryption.key", "A" * 43 + "=")
    config = write_private(tmp_path / "feishu.env", "LARK_APP_ID=synthetic\nLARK_APP_SECRET=synthetic-secret\n")
    monkeypatch.setenv("REMOTE_AGENT_SESSION_MODE", "live")
    monkeypatch.setenv("REMOTE_AGENT_SESSION_KEY_FILE", str(key))
    monkeypatch.setenv("REMOTE_AGENT_SESSION_FEISHU_CONFIG", str(config))
    monkeypatch.setenv("REMOTE_AGENT_SESSION_DATABASE_URL", "postgresql://synthetic@127.0.0.1/test")
    settings = Settings.load()
    assert settings.key == ("A" * 43 + "=").encode()
    assert settings.feishu_app_id == "synthetic"
    assert settings.feishu_secret == "synthetic-secret"
