from uuid import uuid4

import pytest

from .helpers import login
from .test_session_relay import BASE, attached, deliveries, event, ready


def dispatched_pair(client, app):
    headers, view, service = attached(client, app)
    ready(client, headers)
    for text in ("原问题", "答案"):
        service.session_relay.receive(event(text), "offline-test-app")
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    for message in messages:
        response = client.post(
            BASE + "/messages/" + message["id"] + "/dispatch",
            headers=headers,
            json={"generation": view["generation"]},
        )
        assert response.status_code == 200
    return headers, view, service, [message["id"] for message in messages]


def test_same_turn_remote_sources_settle_once(client, app):
    headers, view, service, ids = dispatched_pair(client, app)
    body = {
        "generation": view["generation"],
        "outcome": "completed",
        "text": "完整回答",
        "message_ids": ids,
    }
    for ident in ids:
        response = client.post(BASE + "/messages/" + ident + "/result", headers=headers, json=body)
        assert response.status_code == 200, response.text
    assert sum("完整回答" in item["content"] for item in deliveries(service)) == 1
    assert client.post(BASE + "/poll", headers=headers).json()["messages"] == []
    origin = {
        "generation": view["generation"],
        "native_session": view["native_session"],
        "message_ids": ids,
    }
    assert client.post(BASE + "/origin", headers=headers, json=origin).json()["scope"] == "inactive"
    assert (
        client.post(BASE + "/origin", headers=headers, json={**origin, "for_result": True}).json()[
            "scope"
        ]
        == "remote"
    )


@pytest.mark.parametrize("invalid", ["local", "generation", "device", "session"])
def test_result_rejects_foreign_or_local_source(client, app, invalid):
    headers, view, service, ids = dispatched_pair(client, app)
    if invalid == "local":
        ids.append("native-local-input")
    else:
        if invalid == "device":
            login(client, app, "bob")
        with service.db.transaction() as c:
            if invalid == "generation":
                c.execute(
                    "UPDATE session_relay.messages SET generation=generation+1 WHERE id=%s",
                    (ids[1],),
                )
            elif invalid == "device":
                c.execute(
                    "UPDATE session_relay.messages SET device_id=(SELECT id FROM identity.sessions WHERE id<>session_relay.messages.device_id LIMIT 1) WHERE id=%s",
                    (ids[1],),
                )
            else:
                c.execute(
                    "UPDATE session_relay.messages SET native_session=%s WHERE id=%s",
                    (str(uuid4()), ids[1]),
                )
    response = client.post(
        BASE + "/messages/" + ids[0] + "/result",
        headers=headers,
        json={
            "generation": view["generation"],
            "outcome": "completed",
            "text": "不得转发",
            "message_ids": ids,
        },
    )
    assert response.status_code == 409, response.text
    assert not any("不得转发" in item["content"] for item in deliveries(service))
