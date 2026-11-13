import os
import re
from importlib.metadata import version


def headers():
    revision = os.environ.get("REMOTE_AGENT_SESSION_BUILD_REVISION", "unknown")
    if not re.fullmatch(r"[A-Za-z0-9._-]{1,128}", revision):
        revision = "unknown"
    return {
        "X-Remote-Agent-Session-Server-Version": version("remote-agent-session-server"),
        "X-Remote-Agent-Session-Server-Build": revision,
    }
