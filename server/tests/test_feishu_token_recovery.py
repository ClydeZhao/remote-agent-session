import json
from concurrent.futures import ThreadPoolExecutor
from threading import Barrier

import httpx
import pytest

from remote_agent_session.feishu import Feishu, FeishuError, FeishuTransportError


@pytest.fixture
def transport():
    client = Feishu("synthetic-app", "synthetic-secret")
    client.client.close()
    yield client
    client.client.close()


def install(transport, respond):
    transport.client = httpx.Client(
        base_url="https://example.invalid", transport=httpx.MockTransport(respond)
    )


def token(value):
    return httpx.Response(200, json={"code": 0, "tenant_access_token": value, "expire": 7200})


@pytest.mark.parametrize("status", [200, 400, 401])
def test_invalid_cached_token_refreshes_once_preserving_message(transport, status):
    tokens = []
    messages = []

    def respond(request):
        if request.url.path.endswith("/internal"):
            tokens.append(len(tokens))
            return token("old" if len(tokens) == 1 else "new")
        messages.append(json.loads(request.content))
        if request.headers["Authorization"] == "Bearer old":
            return httpx.Response(status, json={"code": 99991663, "msg": "secret"})
        return httpx.Response(200, json={"code": 0, "data": {"message_id": "om_reply"}})

    install(transport, respond)
    assert transport.send_post("recipient", {"text": "answer"}, "stable-id") == {
        "message_id": "om_reply"
    }
    assert len(tokens) == 2 and len(messages) == 2
    assert messages[0] == messages[1]


def test_sleep_elapsed_time_refreshes_before_next_request(transport, monkeypatch):
    clock = {"wall": 100000, "awake": 100}
    monkeypatch.setattr("remote_agent_session.feishu.time.time", lambda: clock["wall"])
    monkeypatch.setattr("remote_agent_session.feishu.time.monotonic", lambda: clock["awake"])
    tokens = []
    authorizations = []

    def respond(request):
        if request.url.path.endswith("/internal"):
            tokens.append(len(tokens))
            return token(str(len(tokens)))
        authorizations.append(request.headers["Authorization"])
        return httpx.Response(200, json={"code": 0, "data": {}})

    install(transport, respond)
    transport.request("GET", "/im/v1/messages/om_example")
    clock["wall"] += 8000
    clock["awake"] += 10
    transport.request("GET", "/im/v1/messages/om_example")
    assert authorizations == ["Bearer 1", "Bearer 2"]


@pytest.mark.parametrize("status,code", [(400, 230001), (400, 99991663), (200, 99991663)])
def test_rejections_are_bounded_and_expose_only_code(transport, status, code):
    paths = []

    def respond(request):
        paths.append(request.url.path)
        if request.url.path.endswith("/internal"):
            return token(str(len(paths)))
        return httpx.Response(status, json={"code": code, "msg": "synthetic-private-body"})

    install(transport, respond)
    with pytest.raises(FeishuError) as caught:
        transport.send_text("recipient", "answer", "stable-id")
    assert caught.value.code == code
    assert "synthetic-private-body" not in str(caught.value)
    assert paths.count("/im/v1/messages") == (2 if code == 99991663 else 1)


@pytest.mark.parametrize("failure", ["timeout", "server_error", "html", "malformed_code"])
def test_uncertain_send_is_not_retried(transport, failure):
    calls = []

    def respond(request):
        if request.url.path.endswith("/internal"):
            return token("valid")
        calls.append(request.url.path)
        if failure == "timeout":
            raise httpx.ReadTimeout("private", request=request)
        if failure == "server_error":
            return httpx.Response(503, json={"code": 99991663})
        if failure == "malformed_code":
            return httpx.Response(400, json={"code": "private"})
        return httpx.Response(400, text="private")

    install(transport, respond)
    with pytest.raises(FeishuTransportError) as caught:
        transport.send_text("recipient", "answer", "stable-id")
    assert len(calls) == 1
    assert "private" not in str(caught.value)


def test_concurrent_rejections_share_refreshed_token(transport):
    barrier = Barrier(2)
    tokens = []

    def respond(request):
        if request.url.path.endswith("/internal"):
            tokens.append(len(tokens))
            return token("old" if len(tokens) == 1 else "new")
        if request.headers["Authorization"] == "Bearer old":
            barrier.wait(timeout=5)
            return httpx.Response(400, json={"code": 99991663})
        return httpx.Response(200, json={"code": 0, "data": {"ok": True}})

    install(transport, respond)
    with ThreadPoolExecutor(max_workers=2) as pool:
        results = list(pool.map(lambda _: transport.request("GET", "/example"), range(2)))
    assert results == [{"ok": True}, {"ok": True}]
    assert len(tokens) == 2


def test_session_typing_and_result_recover_expired_credentials(client, app, transport):
    from .test_session_relay import BASE, attached, deliveries, event, ready

    headers, view, service = attached(client, app)
    ready(client, headers)
    issued = []
    rejected = set()
    posts = []
    reactions = []

    def respond(request):
        if request.url.path.endswith("/internal"):
            issued.append(str(len(issued) + 1))
            return token(issued[-1])
        authorization = request.headers["Authorization"]
        if authorization in rejected:
            return httpx.Response(400, json={"code": 99991663})
        if request.url.path.endswith("/reactions"):
            if request.method == "GET":
                items = [
                    {
                        "reaction_id": ident,
                        "operator": {"operator_type": "app", "operator_id": "synthetic-app"},
                    }
                    for ident in reactions
                ]
                return httpx.Response(200, json={"code": 0, "data": {"items": items}})
            reactions.append("typing")
        elif request.method == "DELETE":
            reactions.clear()
        else:
            posts.append(json.loads(request.content))
        return httpx.Response(200, json={"code": 0, "data": {"message_id": "om_reply"}})

    install(transport, respond)
    assert service.session_notifications.once(transport)
    incoming = event("work")
    incoming["event"]["message"]["message_id"] = "om_work"
    service.session_relay.receive(incoming, "offline-test-app")
    message = client.post(BASE + "/poll", headers=headers).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    generation = {"generation": view["generation"]}
    assert client.post(path + "/dispatch", headers=headers, json=generation).status_code == 200
    assert client.post(path + "/working", headers=headers, json=generation).status_code == 200
    rejected.add("Bearer " + issued[-1])
    assert service.session_notifications.activity_once(transport)
    assert reactions == ["typing"]
    assert (
        client.post(
            path + "/result",
            headers=headers,
            json={**generation, "outcome": "completed", "text": "result-content"},
        ).status_code
        == 200
    )
    rejected.add("Bearer " + issued[-1])
    with service.db.transaction() as connection:
        connection.execute("UPDATE notifications.session_deliveries SET started_at=NULL")
    assert service.session_notifications.once(transport)
    assert deliveries(service)[-1]["state"] == "accepted"
    assert sum("result-content" in post["content"] for post in posts) == 1
    assert service.session_notifications.activity_once(transport)
    assert not reactions
    assert len(issued) == 3


def test_rejected_delivery_and_typing_keep_safe_error_code(client, app, transport, caplog):
    from .test_session_relay import BASE, attached, deliveries, event, ready

    headers, view, service = attached(client, app)
    ready(client, headers)

    def respond(request):
        if request.url.path.endswith("/internal"):
            return token("valid")
        return httpx.Response(400, json={"code": 230001, "msg": "private-response-body"})

    install(transport, respond)
    assert service.session_notifications.once(transport)
    row = deliveries(service)[0]
    assert row["state"] == "pending"
    assert row["last_error"] == "platform_rejected:230001"
    incoming = event("work")
    incoming["event"]["message"]["message_id"] = "om_work"
    service.session_relay.receive(incoming, "offline-test-app")
    message = client.post(BASE + "/poll", headers=headers).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    generation = {"generation": view["generation"]}
    client.post(path + "/dispatch", headers=headers, json=generation)
    client.post(path + "/working", headers=headers, json=generation)
    assert service.session_notifications.activity_once(transport)
    assert "platform_rejected:230001" in caplog.text
    assert "private-response-body" not in caplog.text


def test_clock_rollback_does_not_extend_token_lifetime(transport, monkeypatch):
    clock = {"wall": 100000, "awake": 100}
    monkeypatch.setattr("remote_agent_session.feishu.time.time", lambda: clock["wall"])
    monkeypatch.setattr("remote_agent_session.feishu.time.monotonic", lambda: clock["awake"])
    issued = []

    def respond(request):
        if request.url.path.endswith("/internal"):
            issued.append(len(issued))
            return token(str(len(issued)))
        return httpx.Response(200, json={"code": 0, "data": {}})

    install(transport, respond)
    transport.request("GET", "/example")
    clock["wall"] -= 10000
    clock["awake"] += 8000
    transport.request("GET", "/example")
    assert len(issued) == 2


@pytest.mark.parametrize("failure", ["timeout", "rejection"])
def test_failed_refresh_never_repeats_message_with_rejected_token(transport, failure):
    paths = []
    issued = []

    def respond(request):
        paths.append(request.url.path)
        if request.url.path.endswith("/internal"):
            issued.append(len(issued))
            if len(issued) == 1:
                return token("old")
            if failure == "timeout":
                raise httpx.ReadTimeout("private", request=request)
            return httpx.Response(400, json={"code": 10003, "msg": "private"})
        return httpx.Response(400, json={"code": 99991663})

    install(transport, respond)
    with pytest.raises((FeishuError, FeishuTransportError)) as caught:
        transport.send_text("recipient", "answer", "stable-id")
    assert paths.count("/im/v1/messages") == 1
    assert len(issued) == 2
    assert "private" not in str(caught.value)
