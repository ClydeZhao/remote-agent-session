import os
from dataclasses import dataclass, field

from remote_agent_session.private_config import private_file, read_private_env


@dataclass(frozen=True)
class Settings:
    database_url: str = field(repr=False)
    key: bytes = field(repr=False)
    mode: str = "offline"
    feishu_app_id: str = ""
    feishu_secret: str = field(default="", repr=False)

    @classmethod
    def load(cls):
        mode = os.environ.get("REMOTE_AGENT_SESSION_MODE")
        if mode not in ("offline", "live"):
            raise RuntimeError("Explicit offline or live mode required")
        key = private_file(os.environ["REMOTE_AGENT_SESSION_KEY_FILE"]).read_bytes().strip()
        url = os.environ["REMOTE_AGENT_SESSION_DATABASE_URL"]
        if mode == "offline":
            return cls(url, key)
        cfg = read_private_env(os.environ["REMOTE_AGENT_SESSION_FEISHU_CONFIG"])
        return cls(url, key, mode, cfg["LARK_APP_ID"], cfg["LARK_APP_SECRET"])
