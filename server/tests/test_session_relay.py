import json
from uuid import uuid4

from .helpers import login

BASE = "/v1/session-relay"


def attached(client, app, subject="alice"):
    headers = login(client, app, subject)
    response = client.post(
        BASE + "/attach",
        headers=headers,
        json={
            "native_session": str(uuid4()),
            "device_name": "科研电脑",
            "project": "rnaseq",
            "session_name": "内存排查",
        },
    )
    assert response.status_code == 200, response.text
    return headers, response.json(), app.state.application


def ready(client, headers):
    view = client.post(BASE + "/poll", headers=headers).json()
    response = client.post(
        BASE + "/readiness",
        headers=headers,
        json={"generation": view["generation"], "available": True},
    )
    assert response.status_code == 200, response.text
    assert response.json()["status"] == "ready"
    return response.json()


def finish(client, headers, body, text="回答", outcome="completed", summary=""):
    response = client.post(BASE + "/poll", headers=headers)
    message = response.json()["messages"][0]
    generation = message["generation"]
    path = BASE + "/messages/" + message["id"]
    dispatch = client.post(path + "/dispatch", headers=headers, json={"generation": generation})
    assert dispatch.json()["dispatch"]
    result = dict(generation=generation, outcome=outcome, text=text, summary=summary)
    response = client.post(path + "/result", headers=headers, json=result)
    assert response.status_code == 200, response.text
    return path, result


def event(text, subject="alice", ident=None, tenant="offline-test-tenant"):
    return dict(
        header=dict(
            app_id="offline-test-app",
            event_type="im.message.receive_v1",
            event_id=ident or str(uuid4()),
            tenant_key=tenant,
        ),
        event=dict(
            sender=dict(sender_type="user", tenant_key=tenant, sender_id=dict(open_id=subject)),
            message=dict(message_type="text", chat_type="p2p", content=json.dumps(dict(text=text))),
        ),
    )


def deliveries(a):
    with a.db.transaction() as c:
        return c.execute(
            "SELECT * FROM notifications.session_deliveries ORDER BY created_at,message_key"
        ).fetchall()


def test_ack_requires_background_readiness_without_agent_input(client, app):
    h, v, a = attached(client, app)
    assert v["status"] == "connecting"
    assert not deliveries(a)
    assert client.post(BASE + "/poll", headers=h).json()["messages"] == []
    ready(client, h)
    ready(client, h)
    rows = deliveries(a)
    assert len(rows) == 1
    assert "已连接到会话「内存排查」。接下来可以直接在这里继续聊。" in rows[0]["content"]
    assert "> 科研电脑 · Codex · rnaseq · 内存排查" in rows[0]["content"]


def test_result_retry_is_idempotent_and_conflicts_are_rejected(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("问题"), "offline-test-app")
    path, result = finish(client, h, v)
    assert client.post(path + "/result", headers=h, json=result).status_code == 200
    assert len(deliveries(a)) == 2
    result["text"] = "different"
    assert client.post(path + "/result", headers=h, json=result).status_code == 409


def test_event_dedup_sources_and_stale_result(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    e = event("看一下结果")
    a.session_relay.receive(e, "offline-test-app")
    a.session_relay.receive(e, "offline-test-app")
    a.session_relay.receive(event("不属于该用户", subject="bob"), "offline-test-app")
    a.session_relay.receive(event("另一个租户", tenant="other"), "offline-test-app")
    poll = client.post(BASE + "/poll", headers=h).json()
    assert len(poll["messages"]) == 1
    message = poll["messages"][0]
    client.post(
        BASE + "/messages/" + message["id"] + "/dispatch",
        headers=h,
        json={"generation": v["generation"]},
    )
    detached = client.post(BASE + "/detach", headers=h, json={"generation": v["generation"]}).json()
    assert not detached["desired"]
    response = client.post(
        BASE + "/messages/" + message["id"] + "/result",
        headers=h,
        json={"generation": v["generation"], "outcome": "completed", "text": "不得转发"},
    )
    assert response.status_code == 409
    assert all("不得转发" not in d["content"] for d in deliveries(a))
    a.session_relay.receive(event("/session resume"), "offline-test-app")
    resumed = client.post(BASE + "/poll", headers=h).json()
    assert resumed["native_session"] == v["native_session"]
    assert resumed["status"] == "connecting"
    assert resumed["messages"] == []
    ready(client, h)


def test_mixed_origin_no_body_leak_and_summary_order(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("第一条"), "offline-test-app")
    finish(client, h, v, text="本地秘密内容", outcome="mixed")
    assert all("本地秘密内容" not in d["content"] for d in deliveries(a))
    a.session_relay.receive(event("第二条"), "offline-test-app")
    finish(client, h, v, text="完整正文", summary="无需回复")
    rows = deliveries(a)
    assert "完整正文" in rows[-2]["content"]
    assert "TL;DR" in rows[-1]["content"]


def test_offline_and_logout_prevent_execution(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    with a.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.bindings SET heartbeat=clock_timestamp()-interval '2 minutes'"
        )
    assert a.session_relay.expire()
    a.session_relay.receive(event("不应执行"), "offline-test-app")
    with a.db.transaction() as c:
        assert (
            c.execute(
                "SELECT count(*) AS n FROM session_relay.messages WHERE content='不应执行' AND state='pending'"
            ).fetchone()["n"]
            == 0
        )
    client.delete("/v1/auth/session", headers=h)
    a.session_relay.receive(event("/session resume"), "offline-test-app")
    assert client.post(BASE + "/poll", headers=h).status_code == 401


def test_dispatch_does_not_authorize_second_send(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("问题"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    path = BASE + "/messages/" + message["id"] + "/dispatch"
    assert client.post(path, headers=h, json={"generation": v["generation"]}).json() == {
        "dispatch": True
    }
    assert client.post(path, headers=h, json={"generation": v["generation"]}).json() == {
        "dispatch": False
    }


def test_other_device_cannot_poll_or_detach(client, app):
    h, v, a = attached(client, app)
    other = login(client, app, "alice")
    other_view = client.post(BASE + "/poll", headers=other).json()
    assert (
        other_view["status"] == "replaced"
        and not other_view["desired"]
        and not other_view["messages"]
    )
    assert (
        client.post(
            BASE + "/detach", headers=other, json={"generation": v["generation"]}
        ).status_code
        == 403
    )


def test_repeated_detach_does_not_exhaust_new_generation(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    for _ in range(6):
        a.session_relay.receive(event("待撤销"), "offline-test-app")
        view = client.post(BASE + "/poll", headers=h).json()
        message = view["messages"][0]
        client.post(
            BASE + "/messages/" + message["id"] + "/dispatch",
            headers=h,
            json={"generation": view["generation"]},
        )
        client.post(BASE + "/detach", headers=h, json={"generation": view["generation"]})
        cancellations = client.post(BASE + "/poll", headers=h).json()["cancellations"]
        assert any(x["id"] == message["id"] for x in cancellations)
        a.session_relay.receive(event("/session resume"), "offline-test-app")
        ready(client, h)
    a.session_relay.receive(event("新一轮"), "offline-test-app")
    current = client.post(BASE + "/poll", headers=h).json()
    assert len(current["messages"]) == 1 and current["messages"][0]["content"] == "新一轮"


def test_quote_context_and_unreadable_reference(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    seen = []

    def quote(parent, chat):
        seen.append((parent, chat))
        return "HPC task abc: failed, log available"

    a.session_relay.quote_reader = quote
    e = event("这个为什么失败？")
    e["event"]["message"].update(parent_id="om_parent", chat_id="oc_current")
    a.session_relay.receive(e, "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    assert "HPC task abc" in message["content"] and "这个为什么失败" in message["content"]
    client.post(BASE + "/poll", headers=h)
    assert seen == [("om_parent", "oc_current")]


def test_platform_rejection_does_not_block_status(client, app):
    from remote_agent_session.feishu import FeishuError

    h, v, a = attached(client, app)
    ready(client, h)

    class Rejected:
        def send_post(self, *args):
            raise FeishuError(99991400)

    assert a.session_notifications.once(Rejected())
    assert deliveries(a)[0]["state"] == "pending"
    a.session_relay.receive(event("/session status"), "offline-test-app")

    class Accepted:
        def send_post(self, *args):
            return {"message_id": "om_test_status"}

    with a.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET started_at=clock_timestamp()-interval '2 seconds'"
        )
    assert a.session_notifications.once(Accepted())
    assert deliveries(a)[-1]["state"] == "accepted"


def test_uncertain_body_suppresses_summary_but_not_future_messages(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    with a.db.transaction() as c:
        c.execute("UPDATE notifications.session_deliveries SET state='accepted',receipt='om_ack'")
    a.session_relay.receive(event("需要长回答"), "offline-test-app")
    finish(client, h, v, text="全文", summary="简述")

    class Lost:
        def send_post(self, *args):
            raise ConnectionError()

    assert a.session_notifications.once(Lost())
    with a.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET started_at=clock_timestamp()-interval '31 seconds' WHERE state='unknown'"
        )
    a.session_relay.receive(event("/session status"), "offline-test-app")

    class Accepted:
        def send_post(self, *args):
            return {"message_id": "om_after_unknown"}

    assert a.session_notifications.once(Accepted())
    rows = deliveries(a)
    assert any(x["state"] == "unconfirmed" for x in rows)
    assert any(x["state"] == "cancelled" and "TL;DR" in x["content"] for x in rows)
    assert rows[-1]["state"] == "accepted"
    assert client.get(BASE, headers=h).json()["delivery_warning"]


def test_slow_answer_does_not_emit_speculative_approval_notice(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("执行下一步"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    client.post(path + "/dispatch", headers=h, json={"generation": message["generation"]})
    for _ in range(2):
        assert (
            client.post(
                path + "/waiting", headers=h, json={"generation": message["generation"]}
            ).status_code
            == 200
        )
    notices = [d for d in deliveries(a) if "原生权限确认" in d["content"]]
    assert notices == []
    assert len(deliveries(a)) == 1


def test_platform_quote_reader_rejects_other_chat():
    from remote_agent_session.feishu import Feishu

    f = Feishu("synthetic", "synthetic")
    f.request = lambda *a, **kw: {
        "items": [{"chat_id": "oc_other", "body": {"content": "other chat"}}]
    }
    import pytest

    with pytest.raises(ValueError):
        f.quote_message("om_parent", "oc_mine")


def test_replaced_device_receives_only_its_cancellations(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("问题"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    client.post(
        BASE + "/messages/" + message["id"] + "/dispatch",
        headers=h,
        json={"generation": v["generation"]},
    )
    other = login(client, app, "alice")
    new = client.post(
        BASE + "/attach",
        headers=other,
        json={
            "native_session": str(uuid4()),
            "device_name": "second",
            "project": "other",
            "session_name": "new",
        },
    ).json()
    old = client.post(BASE + "/poll", headers=h).json()
    assert old["status"] == "replaced" and old["messages"] == []
    assert new["status"] == "connecting"
    assert "当前连接已被替换" in old["guidance"]
    assert "飞书已连接" not in old["guidance"] and "这台电脑不再转发" not in old["guidance"]
    assert old["cancellations"] == [
        {
            "id": message["id"],
            "native_session": v["native_session"],
            "runtime_kind": "codex",
            "runtime_instance": "",
        }
    ]
    assert (
        client.post(BASE + "/messages/" + message["id"] + "/cancelled", headers=h).status_code
        == 200
    )
    assert client.post(BASE + "/poll", headers=h).json()["cancellations"] == []
    assert client.get(BASE, headers=other).json()["native_session"] == new["native_session"]


def test_uncertain_waiting_notice_does_not_cancel_answer(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("question"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    client.post(path + "/dispatch", headers=h, json={"generation": v["generation"]})
    with a.db.transaction() as c:
        binding = c.execute("SELECT * FROM session_relay.bindings").fetchone()
        a.session_notifications.enqueue(
            c, binding, str(message["id"]) + "-waiting", "原会话尚未返回完整回答。"
        )
    with a.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE content LIKE '已连接到会话%'"
        )
        c.execute(
            "UPDATE notifications.session_deliveries SET state='unknown',started_at=clock_timestamp()-interval '31 seconds' WHERE content LIKE '原会话尚未%'"
        )
    response = client.post(
        path + "/result",
        headers=h,
        json={
            "generation": v["generation"],
            "outcome": "completed",
            "text": "answer",
            "summary": "summary",
        },
    )
    assert response.status_code == 200

    class Accepted:
        def send_post(self, *args):
            return {"message_id": "om_answer"}

    assert a.session_notifications.once(Accepted())
    assert any(
        x["state"] == "accepted" and x["content"].startswith("answer") for x in deliveries(a)
    )


def test_cancellation_receipt_requires_owned_message(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("问题"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    path = BASE + "/messages/" + message["id"] + "/cancelled"
    assert client.post(path, headers=h).status_code == 409
    status = path.removesuffix("cancelled") + "cancellation"
    assert client.get(status, headers=h).json() == {"requested": False, "settled": False}
    client.post(
        BASE + "/messages/" + message["id"] + "/dispatch",
        headers=h,
        json={"generation": v["generation"]},
    )
    assert client.get(status, headers=h).json() == {"requested": False, "settled": False}
    other = login(client, app, "other-user")
    assert client.get(status, headers=other).status_code == 404
    assert client.post(path, headers=other).status_code == 404
    assert (
        client.post(BASE + "/messages/" + str(uuid4()) + "/cancelled", headers=h).status_code == 404
    )
    client.post(BASE + "/detach", headers=h, json={"generation": v["generation"]})
    assert client.get(status, headers=h).json() == {"requested": True, "settled": False}
    assert client.post(path, headers=h).json() == {"accepted": True}
    assert client.get(status, headers=h).json() == {"requested": False, "settled": True}


def test_markdown_parts_fit_serialized_platform_budget(client, app):
    h, v, a = attached(client, app)
    with a.db.transaction() as c:
        binding = c.execute("SELECT * FROM session_relay.bindings").fetchone()
        a.session_notifications.enqueue(
            c, binding, "escaping-budget", "```text\n" + "\\\n" * 18000 + "\n```"
        )
    rows = deliveries(a)
    assert len(rows) > 1
    for row in rows:
        payload = {
            "receive_id": "ou_synthetic",
            "msg_type": "post",
            "content": json.dumps(
                {"zh_cn": {"content": [[{"tag": "md", "text": row["content"]}]]}},
                ensure_ascii=False,
            ),
        }
        assert len(json.dumps(payload, ensure_ascii=False).encode()) < 30000
        assert "科研电脑" in row["content"]


def test_failed_readiness_can_retry_attach_and_resume(client, app):
    h, v, a = attached(client, app)
    for action in ("attach", "resume"):
        failed = client.post(
            BASE + "/readiness", headers=h, json={"generation": v["generation"], "available": False}
        )
        assert failed.json()["status"] == "unavailable"
        old_generation = v["generation"]
        if action == "attach":
            body = {
                key: v[key] for key in ("native_session", "device_name", "project", "session_name")
            }
            v = client.post(BASE + "/attach", headers=h, json=body).json()
        else:
            a.session_relay.receive(event("/session resume"), "offline-test-app")
            v = client.get(BASE, headers=h).json()
        assert v["status"] == "connecting" and v["generation"] > old_generation
        assert (
            client.post(
                BASE + "/readiness",
                headers=h,
                json={"generation": old_generation, "available": True},
            ).status_code
            == 409
        )
    ready(client, h)


def test_each_offline_transition_notifies_after_reconnect(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    for expected in (1, 2):
        with a.db.transaction() as c:
            c.execute(
                "UPDATE session_relay.bindings SET heartbeat=clock_timestamp()-interval '2 minutes'"
            )
        assert a.session_relay.expire()
        assert not a.session_relay.expire()
        assert sum("暂时联系不到本地会话" in item["content"] for item in deliveries(a)) == expected
        ready(client, h)
        assert client.get(BASE, headers=h).json()["generation"] == v["generation"]


def test_output_too_large_releases_queue_and_explains_local_fallback(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("长回答"), "offline-test-app")
    a.session_relay.receive(event("下一条"), "offline-test-app")
    finish(client, h, v, text="", outcome="output_too_large")
    assert any("超过转发上限" in item["content"] for item in deliveries(a))
    assert client.post(BASE + "/poll", headers=h).json()["messages"][0]["content"] == "下一条"
    finish(client, h, v, text="下一条的回答")
    assert client.post(BASE + "/poll", headers=h).json()["messages"] == []


def test_failed_state_notice_does_not_suppress_next_transition(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    for _ in range(2):
        with a.db.transaction() as c:
            c.execute(
                "UPDATE notifications.session_deliveries SET state='unconfirmed',started_at=clock_timestamp()-interval '2 seconds'"
            )
            c.execute(
                "UPDATE session_relay.bindings SET heartbeat=clock_timestamp()-interval '2 minutes'"
            )
        assert a.session_relay.expire()

        class Accepted:
            def send_post(self, *args):
                return {"message_id": "om_state_notice"}

        assert a.session_notifications.once(Accepted())
        assert deliveries(a)[-1]["state"] == "accepted"
        with a.db.transaction() as c:
            c.execute(
                "UPDATE notifications.session_deliveries SET started_at=clock_timestamp()-interval '2 seconds'"
            )
        ready(client, h)
        assert a.session_notifications.once(Accepted())
        assert deliveries(a)[-1]["state"] == "accepted"


def test_uncertain_delivery_keeps_safe_diagnostic_without_resending(client, app):
    import httpx

    from remote_agent_session.feishu import Feishu

    headers, _, service = attached(client, app)
    ready(client, headers)
    transport = Feishu("synthetic", "synthetic-secret")
    transport.client.close()
    calls = []

    def respond(request):
        calls.append(request.url.path)
        if request.url.path.endswith("tenant_access_token/internal"):
            return httpx.Response(
                200, json={"code": 0, "tenant_access_token": "synthetic-token", "expire": 3600}
            )
        raise httpx.ReadTimeout("synthetic-secret-body", request=request)

    transport.client = httpx.Client(
        base_url="https://example.invalid", transport=httpx.MockTransport(respond)
    )
    try:
        assert service.session_notifications.once(transport)
        row = deliveries(service)[0]
        assert row["state"] == "unknown" and row["attempts"] == 1
        assert row["last_error"] == "delivery_unknown:message:transport:ReadTimeout"
        with service.db.transaction() as connection:
            connection.execute(
                "UPDATE notifications.session_deliveries SET started_at=clock_timestamp()-interval '31 seconds'"
            )
        assert not service.session_notifications.once(transport)
        settled = deliveries(service)[0]
        assert settled["state"] == "unconfirmed" and settled["attempts"] == 1
        assert settled["last_error"] == row["last_error"]
        assert calls.count("/im/v1/messages") == 1
        assert client.get(BASE, headers=headers).json()["delivery_warning"]
    finally:
        transport.client.close()
