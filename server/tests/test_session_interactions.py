from uuid import uuid4

import pytest

from .helpers import login
from .test_session_relay import BASE, attached, deliveries, event, ready


def interaction(client, app, kind="question", **fields):
    headers, view, service = attached(client, app)
    ready(client, headers)
    service.session_relay.receive(event("需要执行"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=headers).json()["messages"][0]
    client.post(
        BASE + "/messages/" + message["id"] + "/dispatch",
        headers=headers,
        json={"generation": view["generation"]},
    )
    body = dict(
        id=str(uuid4()),
        generation=view["generation"],
        native_session=view["native_session"],
        turn_id="turn",
        message_ids=[message["id"]],
        kind=kind,
        prompt="请选择 A 或 B",
        **fields,
    )
    response = client.post(BASE + "/interactions", headers=headers, json=body)
    assert response.status_code == 200, response.text
    return headers, view, service, body


def test_question_response_does_not_create_another_agent_input(client, app):
    headers, view, service, body = interaction(client, app)
    assert any(
        d["card"] and d["card"]["header"]["title"]["content"] == "需要你回答"
        for d in deliveries(service)
    )
    answer = event("/session answer " + body["id"] + " B")
    service.session_relay.receive(answer, "offline-test-app")
    service.session_relay.receive(answer, "offline-test-app")
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "answered" and result["answer"] == "B"
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    assert len(messages) == 1 and messages[0]["content"] == "需要执行"


def test_permission_requires_matching_request_and_exact_decision(client, app):
    headers, view, service, body = interaction(client, app, "permission")
    service.session_relay.receive(
        event("/session answer " + body["id"] + " 同意"), "offline-test-app"
    )
    assert (
        client.get(BASE + "/interactions/" + body["id"], headers=headers).json()["state"]
        == "pending"
    )
    service.session_relay.receive(
        event("/session answer " + body["id"] + " allow", subject="bob"), "offline-test-app"
    )
    assert (
        client.get(BASE + "/interactions/" + body["id"], headers=headers).json()["state"]
        == "pending"
    )
    service.session_relay.receive(
        event("/session answer " + body["id"] + " deny"), "offline-test-app"
    )
    service.session_relay.receive(
        event("/session answer " + body["id"] + " allow"), "offline-test-app"
    )
    assert (
        client.get(BASE + "/interactions/" + body["id"], headers=headers).json()["answer"] == "deny"
    )


@pytest.mark.parametrize("invalidate", ["detach", "expiry"])
def test_invalidated_permission_cannot_be_used(client, app, invalidate):
    headers, view, service, body = interaction(client, app, "permission")
    service.session_relay.receive(
        event("/session answer " + body["id"] + " allow"), "offline-test-app"
    )
    if invalidate == "detach":
        client.post(BASE + "/detach", headers=headers, json={"generation": view["generation"]})
    else:
        with service.db.transaction() as c:
            c.execute(
                "UPDATE session_relay.interactions SET expires_at=clock_timestamp()-interval '1 second'"
            )
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "cancelled" and result["answer"] is None


def test_interactions_reject_other_device_and_nonremote_origin(client, app):
    headers, view, service, body = interaction(client, app)
    other = login(client, app, "alice")
    assert client.get(BASE + "/interactions/" + body["id"], headers=other).status_code == 403
    body["id"] = str(uuid4())
    body["message_ids"] = [str(uuid4())]
    assert client.post(BASE + "/interactions", headers=headers, json=body).status_code == 409


def test_finished_or_mixed_origin_revokes_pending_permission(client, app):
    headers, view, service, body = interaction(client, app, "permission")
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.messages SET state='mixed' WHERE id=%s", (body["message_ids"][0],)
        )
    service.session_relay.receive(
        event("/session answer " + body["id"] + " allow"), "offline-test-app"
    )
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "cancelled" and result["answer"] is None


def test_expired_question_card_is_not_delivered(client, app):
    headers, view, service, body = interaction(client, app)
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE interaction_id IS NULL"
        )
        c.execute(
            "UPDATE session_relay.interactions SET expires_at=clock_timestamp()-interval '1 second'"
        )
    assert not service.session_notifications.once(None)
    assert all(d["state"] == "cancelled" for d in deliveries(service) if d["interaction_id"])


@pytest.mark.parametrize("scope", ["local", "remote", "mixed", "inactive", "old_generation"])
def test_hook_origin_is_classified_before_prompt_submission(client, app, scope):
    headers, view, service, body = interaction(client, app)
    ids = body["message_ids"]
    if scope == "local":
        ids = [str(uuid4()), "local-client"]
    elif scope == "mixed":
        ids = [*ids, ""]
    elif scope == "inactive":
        with service.db.transaction() as c:
            c.execute("UPDATE session_relay.messages SET state='completed' WHERE id=%s", (ids[0],))
    elif scope == "old_generation":
        client.post(BASE + "/detach", headers=headers, json={"generation": view["generation"]})
    before = len(deliveries(service))
    response = client.post(
        BASE + "/origin",
        headers=headers,
        json=dict(
            generation=view["generation"], native_session=view["native_session"], message_ids=ids
        ),
    )
    assert response.status_code == 200, response.text
    assert response.json() == {"scope": "inactive" if scope == "old_generation" else scope}
    assert len(deliveries(service)) == before
    with service.db.transaction() as c:
        assert (
            c.execute("SELECT count(*) AS n FROM session_relay.interactions").fetchone()["n"] == 1
        )


@pytest.mark.parametrize("answered", [False, True])
@pytest.mark.parametrize("state", ["resolved", "cancelled"])
def test_native_request_closure_revokes_card_and_late_answers(client, app, answered, state):
    headers, view, service, body = interaction(client, app, "permission")
    if answered:
        service.session_relay.receive(
            event("/session answer " + body["id"] + " allow"), "offline-test-app"
        )
    path = BASE + "/interactions/" + body["id"]
    closing = {"generation": view["generation"], "state": state}
    for _ in range(2):
        response = client.post(path + "/close", headers=headers, json=closing)
        assert response.status_code == 200
        assert response.json() == {"id": body["id"], "state": state, "answer": None}
    service.session_relay.receive(
        event("/session answer " + body["id"] + " allow"), "offline-test-app"
    )
    assert client.get(path, headers=headers).json()["state"] == state
    assert "本次回复未执行" in deliveries(service)[-1]["content"]
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE interaction_id IS NULL"
        )
    assert not service.session_notifications.once(None)
    assert all(d["state"] == "cancelled" for d in deliveries(service) if d["interaction_id"])


def test_native_request_closure_is_bound_to_device_and_generation(client, app):
    headers, view, service, body = interaction(client, app)
    path = BASE + "/interactions/" + body["id"] + "/close"
    closing = {"generation": view["generation"], "state": "cancelled"}
    other = login(client, app, "bob")
    assert client.post(path, headers=other, json=closing).status_code in (403, 404)
    assert (
        client.post(
            path, headers=headers, json={**closing, "generation": view["generation"] + 1}
        ).status_code
        == 409
    )
    assert (
        client.get(BASE + "/interactions/" + body["id"], headers=headers).json()["state"]
        == "pending"
    )


def test_cancellation_before_registration_prevents_late_card(client, app):
    headers, view, service, body = interaction(client, app, "permission")
    body["id"] = str(uuid4())
    path = BASE + "/interactions/" + body["id"]
    before = len(deliveries(service))
    for _ in range(2):
        response = client.post(
            path + "/close",
            headers=headers,
            json={"generation": view["generation"], "state": "cancelled"},
        )
        assert response.status_code == 200
    for _ in range(2):
        response = client.post(BASE + "/interactions", headers=headers, json=body)
        assert response.status_code == 200
        assert response.json()["state"] == "cancelled"
    assert len(deliveries(service)) == before
    assert client.get(path, headers=headers).json()["state"] == "cancelled"
    response = client.post(
        path + "/close",
        headers=headers,
        json={"generation": view["generation"], "state": "resolved"},
    )
    assert response.status_code == 200
    assert response.json()["state"] == "cancelled"


def deliver_interaction_card(service):
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE interaction_id IS NULL"
        )

    class Transport:
        def send_card(self, subject, content, key):
            return {"message_id": "om_" + key}

    assert service.session_notifications.once(Transport())
    return deliveries(service)[-1]["receipt"]


@pytest.mark.parametrize(
    ("reply", "answer", "quoted"),
    [
        ("b", "绿色", False),
        ("回答：我想选蓝色", "我想选蓝色", False),
        ("我想选蓝色", "我想选蓝色", True),
        ("我想选蓝色", "我想选蓝色", False),
    ],
)
@pytest.mark.parametrize("async_question", [False, True])
def test_question_accepts_short_or_free_reply_from_current_user(
    client, app, reply, answer, quoted, async_question
):
    headers, _, service, body = interaction(
        client, app, choices={"A": "红色", "B": "绿色"}, async_question=async_question
    )
    receipt = deliver_interaction_card(service)
    assert receipt.startswith("om_")
    assert "/session answer" not in deliveries(service)[-1]["content"]
    service.session_relay.receive(event(reply, subject="bob"), "offline-test-app")
    path = BASE + "/interactions/" + body["id"]
    assert client.get(path, headers=headers).json()["state"] == "pending"
    response = event(reply)
    if quoted:
        response["event"]["message"]["parent_id"] = receipt
    service.session_relay.receive(response, "offline-test-app")
    service.session_relay.receive(response, "offline-test-app")
    assert client.get(path, headers=headers).json()["answer"] == answer
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1


@pytest.mark.parametrize("reply", ["B", "回答：我想选蓝色"])
def test_expired_question_shortcut_does_not_start_work(client, app, reply):
    headers, _, service, body = interaction(client, app, choices={"B": "绿色"})
    deliver_interaction_card(service)
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.interactions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",
            (body["id"],),
        )
    service.session_relay.receive(event(reply), "offline-test-app")
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1
    assert "没有等待回答的问题" in deliveries(service)[-1]["content"]


def test_new_instruction_can_interrupt_while_question_is_pending(client, app):
    headers, _, service, body = interaction(client, app, choices={"A": "红色"})
    deliver_interaction_card(service)
    message = "先别做了，改做别的"
    service.session_relay.receive(event("/session new " + message), "offline-test-app")
    assert (
        client.get(BASE + "/interactions/" + body["id"], headers=headers).json()["state"]
        == "pending"
    )
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    assert messages[0]["content"] == message and messages[0]["state"] == "pending"


def test_unlisted_letter_is_accepted_as_own_answer(client, app):
    headers, _, service, body = interaction(client, app, choices={"A": "红色"})
    deliver_interaction_card(service)
    service.session_relay.receive(event("Z"), "offline-test-app")
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "answered" and result["answer"] == "Z"
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1


def test_reply_to_closed_question_is_not_dispatched(client, app):
    headers, view, service, body = interaction(client, app, choices={"A": "红色", "B": "绿色"})
    receipt = deliver_interaction_card(service)
    path = BASE + "/interactions/" + body["id"]
    assert (
        client.post(
            path + "/close",
            headers=headers,
            json={"generation": view["generation"], "state": "cancelled"},
        ).status_code
        == 200
    )
    reply = event("绿色")
    reply["event"]["message"]["parent_id"] = receipt
    service.session_relay.receive(reply, "offline-test-app")
    assert client.get(path, headers=headers).json()["state"] == "cancelled"
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1


def test_attention_waits_for_receipt_and_leaves_text_on_chat_path(client, app):
    headers, _, service, body = interaction(client, app, "attention")
    path = BASE + "/interactions/" + body["id"]
    assert client.get(path, headers=headers).json()["state"] == "pending"
    deliver_interaction_card(service)
    assert client.get(path, headers=headers).json()["state"] == "delivered"
    assert (
        client.post(BASE + "/interactions", headers=headers, json=body).json()["state"]
        == "delivered"
    )
    service.session_relay.receive(
        event("/session answer " + body["id"] + " allow"), "offline-test-app"
    )
    assert client.get(path, headers=headers).json()["state"] == "delivered"
    service.session_relay.receive(event("跳过这一步"), "offline-test-app")
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    assert messages[0]["content"] == "跳过这一步" and messages[0]["state"] == "pending"


def test_multiple_questions_require_reply_to_one_card(client, app):
    headers, _, service, first = interaction(client, app, choices={"A": "红色"})
    first_card = deliver_interaction_card(service)
    second = {**first, "id": str(uuid4()), "choices": {"A": "蓝色"}}
    assert client.post(BASE + "/interactions", headers=headers, json=second).status_code == 200
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET started_at=clock_timestamp()-interval '2 seconds'"
        )
    second_card = deliver_interaction_card(service)
    assert first_card != second_card
    service.session_relay.receive(event("A"), "offline-test-app")
    first_path = BASE + "/interactions/" + first["id"]
    second_path = BASE + "/interactions/" + second["id"]
    assert client.get(first_path, headers=headers).json()["state"] == "pending"
    assert client.get(second_path, headers=headers).json()["state"] == "pending"
    assert "长按对应卡片" in deliveries(service)[-1]["content"]
    reply = event("A")
    reply["event"]["message"]["parent_id"] = second_card
    service.session_relay.receive(reply, "offline-test-app")
    assert client.get(first_path, headers=headers).json()["state"] == "pending"
    assert client.get(second_path, headers=headers).json()["answer"] == "蓝色"
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1


def test_attention_can_retry_after_close_and_reuses_delivered_notice(client, app):
    headers, view, service, first = interaction(client, app, "attention")
    close = {"generation": view["generation"], "state": "cancelled"}
    path = BASE + "/interactions"
    assert (
        client.post(f"{path}/{first['id']}/close", headers=headers, json=close).json()["state"]
        == "cancelled"
    )
    second = {**first, "id": str(uuid4())}
    assert client.post(path, headers=headers, json=second).json()["id"] == second["id"]
    deliver_interaction_card(service)
    third = {**first, "id": str(uuid4())}
    assert client.post(path, headers=headers, json=third).json() == {
        "id": second["id"],
        "state": "delivered",
        "answer": None,
    }
    assert client.get(f"{path}/{first['id']}", headers=headers).json()["state"] == "cancelled"
    assert not any(d["state"] == "pending" for d in deliveries(service))


def test_attention_close_does_not_duplicate_in_flight_notice(client, app):
    headers, view, service, first = interaction(client, app, "attention")
    path = BASE + "/interactions"
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='sending' WHERE interaction_id=%s",
            (first["id"],),
        )
    close = {"generation": view["generation"], "state": "cancelled"}
    assert (
        client.post(f"{path}/{first['id']}/close", headers=headers, json=close).json()["state"]
        == "pending"
    )
    second = {**first, "id": str(uuid4())}
    assert client.post(path, headers=headers, json=second).json()["id"] == first["id"]
    with service.db.transaction() as c:
        c.execute(
            """UPDATE notifications.session_deliveries SET state='accepted',receipt='om_late'
            WHERE interaction_id=%s""",
            (first["id"],),
        )
    assert client.post(path, headers=headers, json=second).json()["state"] == "delivered"
    assert len([d for d in deliveries(service) if d["interaction_id"]]) == 1


def test_quoted_question_answer_waits_for_sending_receipt(client, app):
    headers, view, service, body = interaction(client, app, choices={"A": "红色", "B": "绿色"})
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE interaction_id IS NULL"
        )

    class Transport:
        def send_card(self, subject, content, key):
            incoming = event("绿色")
            incoming["event"]["message"]["parent_id"] = "om_early_answer"
            service.session_relay.receive(incoming, "offline-test-app")
            assert not any(
                m["content"] == "绿色"
                for m in client.post(BASE + "/poll", headers=headers).json()["messages"]
            )
            return {"message_id": "om_early_answer"}

    assert service.session_notifications.once(Transport())
    service.session_relay.expire()
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "answered" and result["answer"] == "绿色"


def test_async_question_survives_completed_origin_without_new_input(client, app):
    headers, view, service, body = interaction(
        client, app, choices={"A": "绿色"}, async_question=True
    )
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.messages SET state='completed' WHERE id=ANY(%s::uuid[])",
            (body["message_ids"],),
        )
    service.session_relay.receive(event("/session answer " + body["id"] + " A"), "offline-test-app")
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "answered" and result["answer"] == "绿色"
    polled = client.post(BASE + "/poll", headers=headers).json()
    assert polled["messages"] == []
    assert body["id"] in polled["async_interactions"]


@pytest.mark.parametrize("state", ["mixed", "interrupted", "cancelled"])
def test_async_question_invalid_origin_cannot_be_answered(client, app, state):
    headers, view, service, body = interaction(client, app, choices={}, async_question=True)
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.messages SET state=%s WHERE id=ANY(%s::uuid[])",
            (state, body["message_ids"]),
        )
    service.session_relay.receive(
        event("/session answer " + body["id"] + " 不应交付"), "offline-test-app"
    )
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "cancelled" and result["answer"] is None


@pytest.mark.parametrize("invalidate", ["timeout", "detach"])
def test_unconfirmed_card_reply_never_falls_back_to_new_task(client, app, invalidate):
    headers, view, service, body = interaction(client, app, choices={})
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='unknown' WHERE interaction_id=%s",
            (body["id"],),
        )
    incoming = event("尚未关联的回答")
    incoming["event"]["message"]["parent_id"] = "om_no_receipt"
    service.session_relay.receive(incoming, "offline-test-app")
    if invalidate == "detach":
        client.post(BASE + "/detach", headers=headers, json={"generation": view["generation"]})
    else:
        with service.db.transaction() as c:
            c.execute(
                "UPDATE session_relay.messages SET created_at=clock_timestamp()-interval '61 seconds' WHERE event_id=%s",
                (incoming["header"]["event_id"],),
            )
    service.session_relay.expire()
    service.session_relay.receive(incoming, "offline-test-app")
    with service.db.transaction() as c:
        row = c.execute(
            "SELECT state FROM session_relay.messages WHERE event_id=%s",
            (incoming["header"]["event_id"],),
        ).fetchone()
        assert row["state"] == "received"
    if invalidate == "timeout":
        assert any("未能确认这条回复" in d["content"] for d in deliveries(service))


def test_async_answer_is_persisted_before_native_submission_and_not_dispatched_twice(client, app):
    headers, view, service, body = interaction(client, app, choices={}, async_question=True)
    service.session_relay.receive(
        event("/session answer " + body["id"] + " 绿色"), "offline-test-app"
    )
    answer = {"id": str(uuid4()), "generation": view["generation"], "interaction_ids": [body["id"]]}
    response = client.post(BASE + "/async-answers", headers=headers, json=answer)
    assert response.status_code == 200 and response.json() == {"dispatch": True}
    assert client.post(BASE + "/async-answers", headers=headers, json=answer).json() == {
        "dispatch": False
    }
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    registered = next(m for m in messages if m["id"] == answer["id"])
    assert registered["state"] == "dispatching" and registered["kind"] == "async_answer"
    origin = client.post(
        BASE + "/origin",
        headers=headers,
        json={
            "generation": view["generation"],
            "native_session": view["native_session"],
            "message_ids": [answer["id"]],
        },
    )
    assert origin.json()["scope"] == "remote"
    result = client.post(
        BASE + "/messages/" + answer["id"] + "/result",
        headers=headers,
        json={
            "generation": view["generation"],
            "outcome": "completed",
            "text": "根据绿色方案继续完成",
        },
    )
    assert result.status_code == 200
    assert any("根据绿色方案继续完成" in d["content"] for d in deliveries(service))


@pytest.mark.parametrize("async_question", [False, True])
def test_bare_answer_before_card_receipt_waits_for_exact_question(client, app, async_question):
    headers, view, service, body = interaction(
        client, app, choices={}, async_question=async_question
    )
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE interaction_id IS NULL"
        )

    class Transport:
        def send_card(self, subject, content, key):
            service.session_relay.receive(event("我想选蓝色"), "offline-test-app")
            assert not any(
                m["content"] == "我想选蓝色"
                for m in client.post(BASE + "/poll", headers=headers).json()["messages"]
            )
            return {"message_id": "om_bare_early_answer"}

    assert service.session_notifications.once(Transport())
    service.session_relay.expire()
    result = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert result["state"] == "answered" and result["answer"] == "我想选蓝色"


def test_bare_answer_to_multiple_questions_never_starts_task(client, app):
    headers, view, service, body = interaction(client, app, choices={})
    deliver_interaction_card(service)
    second = {**body, "id": str(uuid4()), "prompt": "第二个问题"}
    assert client.post(BASE + "/interactions", headers=headers, json=second).status_code == 200
    service.session_relay.receive(event("蓝色"), "offline-test-app")
    assert (
        client.get(BASE + "/interactions/" + body["id"], headers=headers).json()["state"]
        == "pending"
    )
    assert (
        client.get(BASE + "/interactions/" + second["id"], headers=headers).json()["state"]
        == "pending"
    )
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1
    assert any("有多条问题" in d["content"] for d in deliveries(service))


@pytest.mark.parametrize("transition", ["detach", "feishu_detach", "replace", "logout"])
@pytest.mark.parametrize("answered", [False, True])
def test_binding_generation_change_closes_timed_card_without_lazy_read(
    client, app, transition, answered
):
    headers, view, service, body = interaction(client, app, async_question=True)
    if answered:
        service.session_relay.receive(
            event("/session answer " + body["id"] + " original-answer"), "offline-test-app"
        )
    with service.db.transaction() as connection:
        before = connection.execute(
            "SELECT state,answer FROM session_relay.interactions WHERE id=%s", (body["id"],)
        ).fetchone()
    assert before["state"] == ("answered" if answered else "pending")
    if transition == "detach":
        result = client.post(
            BASE + "/detach", headers=headers, json={"generation": view["generation"]}
        )
        assert result.status_code == 200
    elif transition == "feishu_detach":
        service.session_relay.receive(event("/session detach"), "offline-test-app")
    elif transition == "replace":
        result = client.post(
            BASE + "/attach",
            headers=headers,
            json={
                "native_session": str(uuid4()),
                "device_name": "other",
                "project": "test",
                "session_name": "replacement",
            },
        )
        assert result.status_code == 200
    else:
        assert client.delete("/v1/auth/session", headers=headers).status_code == 204
    with service.db.transaction() as connection:
        closed = connection.execute(
            "SELECT state,answer FROM session_relay.interactions WHERE id=%s", (body["id"],)
        ).fetchone()
    assert closed == {"state": "cancelled", "answer": None}
    if transition != "logout":
        obsolete_close = client.post(
            BASE + "/interactions/" + body["id"] + "/close",
            headers=headers,
            json={"generation": view["generation"], "state": "cancelled"},
        )
        assert obsolete_close.status_code == 409
    with service.db.transaction() as connection:
        connection.execute(
            "UPDATE notifications.session_deliveries SET state='accepted' WHERE interaction_id IS NULL"
        )
    assert not service.session_notifications.once(None)
    assert all(row["state"] == "cancelled" for row in deliveries(service) if row["interaction_id"])


def test_temporary_offline_keeps_timed_card_pending_in_original_generation(client, app):
    headers, view, service, body = interaction(client, app, async_question=True)
    with service.db.transaction() as connection:
        connection.execute(
            "UPDATE session_relay.bindings SET heartbeat=clock_timestamp()-interval '2 minutes'"
        )
    assert service.session_relay.expire()
    with service.db.transaction() as connection:
        question = connection.execute(
            "SELECT state,answer FROM session_relay.interactions WHERE id=%s", (body["id"],)
        ).fetchone()
        binding = connection.execute("SELECT generation FROM session_relay.bindings").fetchone()
    assert question == {"state": "pending", "answer": None}
    assert binding["generation"] == view["generation"]


def test_generation_cleanup_preserves_resolved_cards_receipts_and_new_questions(client, app):
    headers, view, service, original = interaction(client, app, async_question=True)
    resolved = client.post(
        BASE + "/interactions/" + original["id"] + "/close",
        headers=headers,
        json={"generation": view["generation"], "state": "resolved"},
    )
    assert resolved.json()["state"] == "resolved"
    old = {**original, "id": str(uuid4())}
    assert client.post(BASE + "/interactions", headers=headers, json=old).status_code == 200
    deliver_interaction_card(service)
    with service.db.transaction() as connection:
        receipt = connection.execute(
            "SELECT state,receipt FROM notifications.session_deliveries WHERE interaction_id=%s",
            (old["id"],),
        ).fetchone()
    assert receipt["state"] == "accepted" and receipt["receipt"]
    detached = client.post(
        BASE + "/detach", headers=headers, json={"generation": view["generation"]}
    )
    assert detached.status_code == 200
    service.session_relay.receive(event("/session resume"), "offline-test-app")
    current = ready(client, headers)
    service.session_relay.receive(event("new generation input"), "offline-test-app")
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    message = next(item for item in messages if item["state"] == "pending")
    assert client.post(
        BASE + "/messages/" + message["id"] + "/dispatch",
        headers=headers,
        json={"generation": current["generation"]},
    ).json()["dispatch"]
    new = {
        **original,
        "id": str(uuid4()),
        "generation": current["generation"],
        "message_ids": [message["id"]],
    }
    assert client.post(BASE + "/interactions", headers=headers, json=new).status_code == 200
    with service.db.transaction() as connection:
        states = connection.execute(
            "SELECT id,state FROM session_relay.interactions WHERE id=ANY(%s::uuid[])",
            ([original["id"], old["id"], new["id"]],),
        ).fetchall()
        after_receipt = connection.execute(
            "SELECT state,receipt FROM notifications.session_deliveries WHERE interaction_id=%s",
            (old["id"],),
        ).fetchone()
    assert {str(row["id"]): row["state"] for row in states} == {
        original["id"]: "resolved",
        old["id"]: "cancelled",
        new["id"]: "pending",
    }
    assert after_receipt == receipt
