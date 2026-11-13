import json
import re


def command_text(message):
    raw = message.get("content")
    if not isinstance(raw, str) or len(raw) > 8192:
        return None
    try:
        body = json.loads(raw)
    except ValueError:
        return None
    if not isinstance(body, dict):
        return None
    if message.get("message_type") == "text":
        text = body.get("text")
    elif message.get("message_type") == "post":
        if "content" not in body and len(body) == 1:
            body = next(iter(body.values()))
        if not isinstance(body, dict):
            return None
        title = body.get("title", "")
        if not isinstance(title, str) or title.strip():
            return None
        rows = body.get("content_v2", body.get("content"))
        if not isinstance(rows, list):
            return None
        lines = []
        for row in rows:
            if not isinstance(row, list):
                return None
            pieces = []
            for node in row:
                if not isinstance(node, dict) or node.get("tag") not in (
                    "text",
                    "code_block",
                    "md",
                ):
                    return None
                if not isinstance(node.get("text"), str):
                    return None
                style = node.get("style", [])
                if not isinstance(style, list) or "lineThrough" in style:
                    return None
                pieces.append(node["text"])
            lines.append("".join(pieces))
        text = "\n".join(lines)
    else:
        return None
    if not isinstance(text, str):
        return None
    text = text.strip()
    fence = re.fullmatch(r"```[A-Za-z0-9_+-]*[ \t]*\r?\n(.*?)\r?\n```", text, re.S)
    if fence:
        return fence[1].strip()
    for marker in ("``", "`", "**", "__"):
        if text.startswith(marker) and text.endswith(marker) and len(text) > 2 * len(marker):
            inner = text[len(marker) : -len(marker)]
            if marker not in inner:
                return inner.strip()
    return text
