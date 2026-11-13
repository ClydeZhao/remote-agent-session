import json
from types import SimpleNamespace

import pytest

from remote_agent_session.identity.feishu import FeishuPairing

CODE = "AB12CD34EF56"
CHALLENGE = "a" * 43


def receive(message_type, content):
    calls = []
    sent = []
    identity = SimpleNamespace(
        pairing_event=lambda e: (calls.append(e), CHALLENGE if not e.confirmation else None)[1]
    )
    transport = SimpleNamespace(
        app_id="app",
        send_text=lambda *a: sent.append(("text", a)),
        send_post=lambda *a: sent.append(("post", a)),
    )
    event = {
        "header": {
            "app_id": "app",
            "event_type": "im.message.receive_v1",
            "event_id": "event",
            "tenant_key": "tenant",
        },
        "event": {
            "sender": {"sender_type": "user", "sender_id": {"open_id": "user"}},
            "message": {
                "chat_type": "p2p",
                "message_type": message_type,
                "content": json.dumps(content),
            },
        },
    }
    FeishuPairing(identity, transport).receive(event)
    return calls, sent


@pytest.mark.parametrize(
    "kind,content",
    [
        ("text", {"text": "`" + CODE + "`"}),
        ("text", {"text": "```\n" + CODE + "\n```"}),
        ("text", {"text": "```text\n" + CODE + "\n```"}),
        ("post", {"title": "", "content": [[{"tag": "text", "text": CODE, "style": ["code"]}]]}),
        (
            "post",
            {
                "title": "",
                "content": [[{"tag": "code_block", "language": "PlainText", "text": CODE}]],
            },
        ),
        (
            "post",
            {
                "zh_cn": {
                    "title": "",
                    "content": [[{"tag": "text", "text": CODE, "style": ["bold"]}]],
                }
            },
        ),
    ],
)
def test_formatted_code_requests_confirmation(kind, content):
    calls, sent = receive(kind, content)
    assert len(calls) == 1
    assert calls[0].user_code == CODE
    assert len(sent) == 1


def test_confirmation_reply_has_one_copyable_code_block():
    calls, sent = receive("text", {"text": CODE})
    assert sent[0][0] == "post"
    post = sent[0][1][1]
    nodes = [n for row in post["zh_cn"]["content"] for n in row]
    blocks = [n for n in nodes if n["tag"] == "code_block"]
    assert len(blocks) == 1
    assert blocks[0]["text"] == "确认 " + CODE + " " + CHALLENGE
    assert any("只复制下面代码块" in n.get("text", "") for n in nodes if n["tag"] == "text")


def test_formatted_confirmation_keeps_full_challenge():
    calls, _ = receive(
        "post",
        {
            "title": "",
            "content": [
                [
                    {
                        "tag": "code_block",
                        "language": "PlainText",
                        "text": "确认 " + CODE + " " + CHALLENGE,
                    }
                ]
            ],
        },
    )
    assert len(calls) == 1 and calls[0].confirmation == CHALLENGE


@pytest.mark.parametrize(
    "content",
    [
        {"text": "请看看这个码 " + CODE},
        {"text": CODE + "\n" + CODE},
        {"text": "```\n" + CODE + "\n```\n其他文字"},
        {"text": None},
    ],
)
def test_ambiguous_message_does_not_pair(content):
    calls, _ = receive("text", content)
    assert not calls


def test_expired_pairing_gets_actionable_feedback():
    from remote_agent_session.errors import DomainError

    sent = []

    def expired(event):
        raise DomainError("PAIRING_INVALID", "do not reflect this private diagnostic")

    identity = SimpleNamespace(pairing_event=expired)
    transport = SimpleNamespace(app_id="app", send_text=lambda *a: sent.append(a))
    FeishuPairing(identity, transport).receive(
        {
            "header": {
                "app_id": "app",
                "event_type": "im.message.receive_v1",
                "event_id": "expired",
                "tenant_key": "tenant",
            },
            "event": {
                "sender": {"sender_type": "user", "sender_id": {"open_id": "user"}},
                "message": {
                    "chat_type": "p2p",
                    "message_type": "text",
                    "content": json.dumps({"text": CODE}),
                },
            },
        }
    )
    assert len(sent) == 1
    assert "最新配对码" in sent[0][1]
    assert CODE not in sent[0][1] and "private diagnostic" not in sent[0][1]


def test_confirmation_ack_does_not_override_client_continuation():
    calls, sent = receive("text", {"text": "确认 " + CODE + " " + CHALLENGE})
    assert calls[0].confirmation == CHALLENGE
    assert len(sent) == 1 and sent[0][0] == "text"
    reply = sent[0][1][1]
    assert "登录已确认" in reply
    assert "终端" in reply
    assert "remote_agent_session login finish" not in reply, (
        "setup polls automatically; the bot must not impose the manual CLI workflow"
    )
