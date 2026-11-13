import time
from types import SimpleNamespace

import pytest

from remote_agent_session.event_time import message_is_fresh
from remote_agent_session.identity.feishu import FeishuPairing
from remote_agent_session.session_relay.service import SessionRelay

from .test_session_relay import BASE, attached, deliveries, event, ready


@pytest.mark.parametrize(
    "value,expected",
    [
        (1000000, True),
        (700000, True),
        (699999, False),
        (1030000, True),
        (1030001, False),
        (None, False),
        (True, False),
        ("broken", False),
    ],
)
def test_message_timestamp_boundaries(value, expected):
    assert message_is_fresh({"event": {"message": {"create_time": value}}}, now=1000) == expected


@pytest.mark.parametrize("age", [301, -31, None, "invalid"])
def test_expired_live_input_is_durable_deduplicated_and_never_replayed(client, app, age):
    headers, view, service = attached(client, app)
    ready(client, headers)
    service.session_relay.require_event_time = True
    incoming = event("绝不执行的旧指令")
    if isinstance(age, int):
        incoming["event"]["message"]["create_time"] = str(int((time.time() - age) * 1000))
    elif age is not None:
        incoming["event"]["message"]["create_time"] = age
    service.session_relay.receive(incoming, "offline-test-app")
    restarted = SessionRelay(service.db, service.session_notifications, require_event_time=True)
    restarted.receive(incoming, "offline-test-app")
    assert client.post(BASE + "/poll", headers=headers).json()["messages"] == []
    assert len([d for d in deliveries(service) if "消息已过期" in d["content"]]) == 1
    with service.db.transaction() as c:
        rows = c.execute("SELECT state FROM session_relay.messages").fetchall()
    assert len(rows) == 1 and rows[0]["state"] == "received"


def test_fresh_live_input_keeps_normal_source_and_deduplication(client, app):
    headers, view, service = attached(client, app)
    ready(client, headers)
    service.session_relay.require_event_time = True
    incoming = event("新指令")
    incoming["event"]["message"]["create_time"] = str(int(time.time() * 1000))
    for _ in range(2):
        service.session_relay.receive(incoming, "offline-test-app")
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    assert len(messages) == 1 and messages[0]["content"] == "新指令"


def test_expired_pairing_cannot_authorize_and_duplicate_survives_restart(client, app):
    device = client.post("/v1/auth/devices", json={"device_name": "test"}).json()
    incoming = event(device["user_code"])
    incoming["event"]["message"]["create_time"] = str(int((time.time() - 301) * 1000))
    sent = []
    transport = SimpleNamespace(
        app_id="offline-test-app", send_text=lambda *args: sent.append(args)
    )
    for _ in range(2):
        assert FeishuPairing(app.state.identity, transport, require_event_time=True).receive(
            incoming
        )
    assert len(sent) == 1 and "已过期" in sent[0][1]
    with app.state.application.db.transaction() as c:
        row = c.execute("SELECT state,candidate FROM identity.devices").fetchone()
    assert row["state"] == "pending" and row["candidate"] is None
