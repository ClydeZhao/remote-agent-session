import os
import stat
from pathlib import Path


def private_file(path):
    path = Path(path)
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode):
        raise ValueError("Configuration must be an owned private regular file")
    if os.name == "nt":
        from remote_agent_session.windows_security import secure_path

        try:
            secure_path(path)
        except RuntimeError as exc:
            raise ValueError("Configuration must be an owned private regular file") from exc
    elif info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError("Configuration must be an owned private regular file")
    return path


def read_private_env(path):
    values = {}
    for line in private_file(path).read_text().splitlines():
        if not line.strip() or line.startswith("#"):
            continue
        key, separator, value = line.partition("=")
        if not separator or key in values or any(c in value for c in "\x00\r\n"):
            raise ValueError("Invalid literal private configuration")
        values[key] = value
    return values
