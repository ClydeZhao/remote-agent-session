import json


def chat_text(message):
    raw = message.get("content")
    if not isinstance(raw, str) or len(raw) > 262144:
        raise ValueError("Invalid message size")
    body = json.loads(raw)
    if not isinstance(body, dict):
        raise ValueError("Invalid message body")
    if message.get("message_type") == "text":
        text = body.get("text")
    elif message.get("message_type") == "post":
        if "content" not in body and "content_v2" not in body and len(body) == 1:
            body = next(iter(body.values()))
        if not isinstance(body, dict):
            raise ValueError("Invalid post")
        title = body.get("title", "")
        rows = body.get("content_v2", body.get("content"))
        if not isinstance(title, str) or not isinstance(rows, list):
            raise ValueError("Invalid post rows")
        lines = [title] if title else []
        for row in rows:
            if not isinstance(row, list):
                raise ValueError("Invalid post row")
            pieces = []
            for node in row:
                if not isinstance(node, dict):
                    raise ValueError("Invalid post node")
                tag = node.get("tag")
                value = node.get("text")
                if tag not in ("text", "md", "a", "code_block") or not isinstance(value, str):
                    raise ValueError("Unsupported post element")
                style = node.get("style", [])
                if not isinstance(style, list) or "lineThrough" in style:
                    raise ValueError("Unsupported post style")
                if tag == "a":
                    href = node.get("href")
                    if not isinstance(href, str):
                        raise ValueError("Invalid link")
                    value = value + " (" + href + ")"
                elif tag == "code_block":
                    value = "\n```\n" + value + "\n```\n"
                pieces.append(value)
            lines.append("".join(pieces))
        text = "\n".join(lines)
    else:
        raise ValueError("Unsupported message type")
    if not isinstance(text, str) or not text.strip() or len(text) > 32000:
        raise ValueError("Invalid text size")
    return text.strip()
