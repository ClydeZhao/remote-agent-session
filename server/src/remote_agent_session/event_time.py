import math
import time


def message_is_fresh(data, now=None):
    value = data.get("event", {}).get("message", {}).get("create_time")
    if isinstance(value, bool) or not isinstance(value, (str, int)):
        return False
    try:
        created = int(value) / 1000
    except (ValueError, OverflowError):
        return False
    now = time.time() if now is None else now
    return math.isfinite(created) and now - 300 <= created <= now + 30
