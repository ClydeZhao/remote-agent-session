def session_card(title, content, source):
    return {
        "schema": "2.0",
        "header": {
            "title": {"tag": "plain_text", "content": title},
            "template": "orange" if title == "需要授权" else "blue",
        },
        "body": {
            "elements": [
                {"tag": "markdown", "content": content},
                {"tag": "hr"},
                {"tag": "markdown", "content": source.removeprefix("> ")},
            ],
        },
    }
