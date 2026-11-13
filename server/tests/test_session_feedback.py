import pytest

from .test_session_relay import BASE, attached, deliveries, event, ready


@pytest.mark.parametrize("expired", [False, True])
def test_invalid_device_receives_login_hint_without_execution(client, app, expired):
    headers, view, a = attached(client, app)
    ready(client, headers)
    if expired:
        with a.db.transaction() as c:
            c.execute(
                "UPDATE identity.sessions SET expires_at=clock_timestamp()-interval '1 second'"
            )
    else:
        client.delete("/v1/auth/session", headers=headers)
    incoming = event("此前不得执行的指令")
    a.session_relay.receive(incoming, "offline-test-app")
    a.session_relay.receive(incoming, "offline-test-app")
    notices = [d for d in deliveries(a) if "重新登录" in d["content"]]
    assert len(notices) == 1
    transport = Transport()
    for _ in range(4):
        with a.db.transaction() as c:
            c.execute("UPDATE notifications.session_deliveries SET started_at=NULL")
        a.session_notifications.once(transport)
    assert len(transport.posts) == 1
    assert "重新登录" in str(transport.posts)
    assert "内存排查" not in str(transport.posts)
    with a.db.transaction() as c:
        assert (
            c.execute(
                "SELECT count(*) AS n FROM session_relay.messages WHERE state IN ('pending','dispatching')"
            ).fetchone()["n"]
            == 0
        )
    before = len(deliveries(a))
    a.session_relay.receive(event("hi", subject="unknown"), "offline-test-app")
    a.session_relay.receive(event("hi", tenant="other"), "offline-test-app")
    assert len(deliveries(a)) == before
    new_headers, _, _ = attached(client, app)
    ready(client, new_headers)
    a.session_relay.receive(incoming, "offline-test-app")
    assert client.post(BASE + "/poll", headers=new_headers).json()["messages"] == []


class Transport:
    def __init__(self):
        self.posts = []
        self.reactions = []
        self.fail = False

    def send_post(self, subject, content, key):
        self.posts.append(content)
        return {"message_id": "om_reply"}

    def set_typing(self, message, active):
        if self.fail:
            raise ConnectionError("temporary failure")
        self.reactions.append((message, active))


@pytest.mark.parametrize("ending", ["completed", "empty_output", "detach", "offline", "logout"])
def test_typing_tracks_observed_work_and_cleans_up(client, app, ending):
    h, v, a = attached(client, app)
    ready(client, h)
    incoming = event("请处理")
    incoming["event"]["message"]["message_id"] = "om_work"
    a.session_relay.receive(incoming, "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    generation = {"generation": v["generation"]}
    transport = Transport()
    assert not a.session_notifications.activity_once(transport)
    assert client.post(path + "/working", headers=h, json=generation).status_code == 409
    client.post(path + "/dispatch", headers=h, json=generation)
    assert not a.session_notifications.activity_once(transport)
    assert client.post(path + "/working", headers=h, json=generation).status_code == 200
    assert client.post(path + "/working", headers=h, json=generation).status_code == 200
    assert a.session_notifications.activity_once(transport)
    assert transport.reactions == [("om_work", True)]
    assert not a.session_notifications.activity_once(transport)
    if ending == "completed":
        client.post(
            path + "/result", headers=h, json={**generation, "outcome": "completed", "text": "结果"}
        )
    elif ending == "empty_output":
        assert (
            client.post(
                path + "/result", headers=h, json={**generation, "outcome": "empty_output"}
            ).status_code
            == 200
        )
    elif ending == "detach":
        client.post(BASE + "/detach", headers=h, json=generation)
    elif ending == "offline":
        with a.db.transaction() as c:
            c.execute(
                "UPDATE session_relay.bindings SET heartbeat=clock_timestamp()-interval '2 minutes'"
            )
    else:
        client.delete("/v1/auth/session", headers=h)
    transport.fail = True
    assert a.session_notifications.activity_once(transport)
    assert transport.reactions == [("om_work", True)]
    transport.fail = False
    with a.db.transaction() as c:
        c.execute("UPDATE notifications.session_activity SET next_attempt=clock_timestamp()")
    assert a.session_notifications.activity_once(transport)
    assert transport.reactions == [("om_work", True), ("om_work", False)]


@pytest.mark.parametrize("outcome", ["interrupted", "interrupt_unavailable", "empty_output"])
def test_interruption_terminal_results_allow_handoff_cleanup(client, app, outcome):
    from .test_session_relay import finish

    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("work"), "offline-test-app")
    path, _ = finish(client, h, v, text="", outcome=outcome)
    assert client.post(path + "/cancelled", headers=h).status_code == 200


def test_pending_message_is_delivered_while_old_turn_is_working(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("old work"), "offline-test-app")
    old = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    client.post(
        BASE + "/messages/" + old["id"] + "/dispatch",
        headers=h,
        json={"generation": v["generation"]},
    )
    a.session_relay.receive(event("new direction"), "offline-test-app")
    messages = client.post(BASE + "/poll", headers=h).json()["messages"]
    assert [m["content"] for m in messages] == ["new direction", "old work"]


@pytest.mark.parametrize(
    ("outcome", "notice"),
    [
        ("empty_output", "没有可转发的正文"),
        ("not_submitted", "这条消息未交给 Agent"),
        ("unknown", "核对前不要重复发送"),
    ],
)
def test_terminal_failures_release_capacity_and_report_next_step(client, app, outcome, notice):
    from .test_session_relay import finish

    h, v, a = attached(client, app)
    ready(client, h)
    for _ in range(5):
        a.session_relay.receive(event("work"), "offline-test-app")
        path, result = finish(client, h, v, text="", outcome=outcome)
        assert client.post(path + "/result", headers=h, json=result).status_code == 200
    a.session_relay.receive(event("next work"), "offline-test-app")
    messages = client.post(BASE + "/poll", headers=h).json()["messages"]
    assert len(messages) == 1 and messages[0]["content"] == "next work"
    notices = [d for d in deliveries(a) if notice in d["content"]]
    assert len(notices) == 5


@pytest.mark.parametrize(
    "status,online,desired,action",
    [
        ("ready", True, True, "可以在飞书继续"),
        ("connecting", True, True, "等待连接确认"),
        ("ready", False, True, "打开电脑和原 Agent"),
        ("unavailable", True, True, "在本地"),
        ("detached", True, False, "/session resume"),
        ("detached", False, False, "打开电脑和原 Agent"),
        ("revoked", True, False, "重新登录"),
    ],
)
def test_session_status_explains_next_action_in_both_channels(
    client, app, status, online, desired, action
):
    h, v, a = attached(client, app)
    with a.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.bindings SET status=%s,desired=%s,heartbeat=clock_timestamp()-(%s * interval '1 second')",
            (status, desired, 0 if online else 120),
        )
    view = client.get(BASE, headers=h).json()
    assert view["status"] == status
    assert action in view["guidance"]
    assert status not in view["guidance"]
    a.session_relay.receive(event("/session status"), "offline-test-app")
    assert view["guidance"] in deliveries(a)[-1]["content"]


def test_delivery_warning_does_not_turn_connected_session_into_execution_failure(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    with a.db.transaction() as c:
        c.execute("UPDATE notifications.session_deliveries SET state='unconfirmed'")
    view = client.get(BASE, headers=h).json()
    assert view["status"] == "ready" and view["delivery_warning"]
    assert "已连接" in view["guidance"] and "消息未能确认送达" in view["guidance"]
    assert "不要为了取回回复重复提交任务" in view["guidance"]


def test_unknown_execution_guidance_requires_reconciliation_before_another_submission(client, app):
    from .test_session_relay import finish

    h, v, a = attached(client, app)
    ready(client, h)
    a.session_relay.receive(event("run work"), "offline-test-app")
    finish(client, h, v, text="", outcome="unknown")
    content = deliveries(a)[-1]["content"]
    assert "无法确认消息执行结果" in content and "核对前不要重复发送" in content
    assert "重新发送" not in content and "再次发送" not in content


def test_same_computer_relogin_does_not_claim_new_connection_is_ready(client, app):
    from .helpers import login

    headers, view, service = attached(client, app)
    ready(client, headers)
    current_login = login(client, app, "alice")
    body = {key: view[key] for key in ("native_session", "device_name", "project", "session_name")}
    new_view = client.post(BASE + "/attach", headers=current_login, json=body).json()
    assert new_view["status"] == "connecting"
    old_view = client.post(BASE + "/poll", headers=headers).json()
    assert old_view["status"] == "replaced"
    assert "当前连接已被替换" in old_view["guidance"]
    assert (
        "飞书已连接" not in old_view["guidance"] and "这台电脑不再转发" not in old_view["guidance"]
    )


@pytest.mark.parametrize("state", ["pending", "dispatching", "completed"])
@pytest.mark.parametrize("expired", [False, True])
def test_processed_event_is_not_reclassified_after_login_expires(client, app, state, expired):
    h, v, a = attached(client, app)
    ready(client, h)
    incoming = event("already received work")
    a.session_relay.receive(incoming, "offline-test-app")
    message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    generation = {"generation": v["generation"]}
    if state != "pending":
        assert client.post(path + "/dispatch", headers=h, json=generation).json()["dispatch"]
    if state == "completed":
        response = client.post(
            path + "/result",
            headers=h,
            json={**generation, "outcome": "completed", "text": "finished"},
        )
        assert response.status_code == 200
    if expired:
        with a.db.transaction() as c:
            c.execute(
                "UPDATE identity.sessions SET expires_at=clock_timestamp()-interval '1 second'"
            )
    else:
        assert client.delete("/v1/auth/session", headers=h).status_code == 204
    a.session_relay.receive(incoming, "offline-test-app")
    assert not [d for d in deliveries(a) if d["kind"] == "login_required"]
    fresh = event("not received before login expired")
    a.session_relay.receive(fresh, "offline-test-app")
    assert len([d for d in deliveries(a) if d["kind"] == "login_required"]) == 1
    new_h, _, _ = attached(client, app)
    ready(client, new_h)
    a.session_relay.receive(incoming, "offline-test-app")
    a.session_relay.receive(fresh, "offline-test-app")
    assert client.post(BASE + "/poll", headers=new_h).json()["messages"] == []
    with a.db.transaction() as c:
        count = c.execute(
            "SELECT count(*) AS n FROM session_relay.messages WHERE event_id=%s",
            (incoming["header"]["event_id"],),
        ).fetchone()["n"]
        assert count == 1


def test_interrupted_turn_is_quiet_and_typing_moves_only_after_new_work(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    generation = {"generation": v["generation"]}
    transport = Transport()
    incoming = event("old work")
    incoming["event"]["message"]["message_id"] = "om_old"
    a.session_relay.receive(incoming, "offline-test-app")
    old = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    old_path = BASE + "/messages/" + old["id"]
    assert client.post(old_path + "/dispatch", headers=h, json=generation).status_code == 200
    assert client.post(old_path + "/working", headers=h, json=generation).status_code == 200
    assert a.session_notifications.activity_once(transport)
    incoming = event("new work")
    incoming["event"]["message"]["message_id"] = "om_new"
    a.session_relay.receive(incoming, "offline-test-app")
    before = len(deliveries(a))
    result = {**generation, "outcome": "interrupted"}
    assert client.post(old_path + "/result", headers=h, json=result).status_code == 200
    assert client.post(old_path + "/result", headers=h, json=result).status_code == 200
    assert len(deliveries(a)) == before
    assert a.session_notifications.activity_once(transport)
    assert transport.reactions == [("om_old", True), ("om_old", False)]
    new = client.post(BASE + "/poll", headers=h).json()["messages"][0]
    new_path = BASE + "/messages/" + new["id"]
    assert client.post(new_path + "/dispatch", headers=h, json=generation).status_code == 200
    assert not a.session_notifications.activity_once(transport)
    assert client.post(new_path + "/working", headers=h, json=generation).status_code == 200
    assert a.session_notifications.activity_once(transport)
    assert transport.reactions[-1] == ("om_new", True)


def test_new_work_supersedes_typing_before_old_result_arrives(client, app):
    h, v, a = attached(client, app)
    ready(client, h)
    generation = {"generation": v["generation"]}
    transport = Transport()
    paths = []
    for name in ("old", "new"):
        incoming = event(name)
        incoming["event"]["message"]["message_id"] = "om_" + name
        a.session_relay.receive(incoming, "offline-test-app")
        message = client.post(BASE + "/poll", headers=h).json()["messages"][0]
        path = BASE + "/messages/" + message["id"]
        paths.append(path)
        assert client.post(path + "/dispatch", headers=h, json=generation).status_code == 200
        assert not a.session_notifications.activity_once(transport)
        assert client.post(path + "/working", headers=h, json=generation).status_code == 200
        assert a.session_notifications.activity_once(transport)
    assert transport.reactions == [("om_old", True), ("om_old", False)]
    assert a.session_notifications.activity_once(transport)
    assert transport.reactions[-1] == ("om_new", True)
    assert client.post(paths[0] + "/working", headers=h, json=generation).status_code == 200
    assert not a.session_notifications.activity_once(transport)
    assert (
        client.post(
            paths[0] + "/result", headers=h, json={**generation, "outcome": "interrupted"}
        ).status_code
        == 200
    )
    assert not a.session_notifications.activity_once(transport)


@pytest.mark.parametrize("runtime", ["codex", "deepseek-harness"])
@pytest.mark.parametrize(
    "status,online,desired",
    [("unavailable", True, True), ("offline", False, True), ("detached", False, False)],
)
def test_connection_recovery_guidance_refers_to_original_agent_in_both_channels(
    client, app, runtime, status, online, desired
):
    headers, _, service = attached(client, app)
    headers = {**headers, "X-Remote-Agent-Session-Relay-Runtime": "1"}
    with service.db.transaction() as connection:
        connection.execute(
            "UPDATE session_relay.bindings SET runtime_kind=%s,runtime_instance=%s,status=%s,desired=%s,heartbeat=clock_timestamp()-(%s * interval '1 second')",
            (
                runtime,
                "profile" if runtime == "deepseek-harness" else "",
                status,
                desired,
                0 if online else 120,
            ),
        )
    view = client.get(BASE, headers=headers).json()
    assert view["runtime_kind"] == runtime and view["status"] == status
    assert "原 Agent" in view["guidance"] and "Codex" not in view["guidance"]
    assert "连接" in view["guidance"] and "原 Agent" in view["guidance"]
    service.session_relay.receive(event("/session status"), "offline-test-app")
    assert view["guidance"] in deliveries(service)[-1]["content"]


@pytest.mark.parametrize("runtime", ["codex", "deepseek-harness"])
def test_failed_initial_readiness_uses_original_agent_guidance(client, app, runtime):
    headers, view, service = attached(client, app)
    headers = {**headers, "X-Remote-Agent-Session-Relay-Runtime": "1"}
    with service.db.transaction() as connection:
        connection.execute(
            "UPDATE session_relay.bindings SET runtime_kind=%s,runtime_instance=%s",
            (runtime, "profile" if runtime == "deepseek-harness" else ""),
        )
    result = client.post(
        BASE + "/readiness",
        headers=headers,
        json={"generation": view["generation"], "available": False},
    )
    assert result.status_code == 200
    assert result.json()["status"] == "unavailable"
    assert "原 Agent" in deliveries(service)[-1]["content"]
    if runtime == "deepseek-harness":
        assert "Codex" not in deliveries(service)[-1]["content"]
