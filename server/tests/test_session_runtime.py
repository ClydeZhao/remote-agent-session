from uuid import uuid4

import pytest

from .helpers import login
from .test_session_interactions import deliver_interaction_card
from .test_session_relay import BASE, attached, deliveries, event, ready


def test_runtime_switch_with_same_native_id_is_not_idempotent(client, app):
    headers, previous, service = attached(client, app)
    headers = {**headers, "X-Remote-Agent-Session-Relay-Runtime": "1"}
    ready(client, headers)
    body = {
        key: previous[key] for key in ("native_session", "device_name", "project", "session_name")
    }
    body.update(runtime_kind="deepseek-harness", runtime_instance="deepseek-harness-profile")
    result = client.post(BASE + "/attach", headers=headers, json=body)
    assert result.status_code == 200, result.text
    current = result.json()
    assert current["generation"] > previous["generation"]
    assert current["runtime_kind"] == "deepseek-harness"
    ready(client, headers)
    assert "已切换" in deliveries(service)[-1]["content"]
    same = client.post(BASE + "/attach", headers=headers, json=body).json()
    assert same["generation"] == current["generation"]
    body["runtime_instance"] = "other-profile"
    changed = client.post(BASE + "/attach", headers=headers, json=body).json()
    assert changed["generation"] > same["generation"]


def test_dsh_native_id_and_message_runtime_snapshot(client, app):
    headers, previous, service = attached(client, app)
    headers = {**headers, "X-Remote-Agent-Session-Relay-Runtime": "1"}
    body = {
        key: previous[key] for key in ("native_session", "device_name", "project", "session_name")
    }
    body.update(
        native_session="session-" + str(uuid4()),
        runtime_kind="deepseek-harness",
        runtime_instance="desktop-profile",
    )
    result = client.post(BASE + "/attach", headers=headers, json=body)
    assert result.status_code == 200, result.text
    assert result.json()["native_session"] == body["native_session"]
    ready(client, headers)
    service.session_relay.receive(event("runtime test"), "offline-test-app")
    with service.db.transaction() as connection:
        rows = connection.execute(
            "SELECT native_session,runtime_kind,runtime_instance FROM session_relay.messages"
        ).fetchall()
    assert rows
    assert all(row["runtime_kind"] == "deepseek-harness" for row in rows)
    assert all(row["runtime_instance"] == "desktop-profile" for row in rows)


@pytest.mark.parametrize("native_session", ["../escape", "has space", "line\nbreak", "x" * 201])
def test_invalid_native_session_rejected(client, app, native_session):
    headers, previous, _ = attached(client, app)
    body = {
        key: previous[key] for key in ("native_session", "device_name", "project", "session_name")
    }
    body.update(
        native_session=native_session, runtime_kind="deepseek-harness", runtime_instance="profile"
    )
    assert client.post(BASE + "/attach", headers=headers, json=body).status_code == 422


def test_legacy_client_cannot_read_poll_or_confirm_new_runtime(client, app):
    headers, previous, _ = attached(client, app)
    body = {
        key: previous[key] for key in ("native_session", "device_name", "project", "session_name")
    }
    body.update(
        runtime_kind="deepseek-harness", runtime_instance="profile", native_session="session-test"
    )
    assert client.post(BASE + "/attach", headers=headers, json=body).status_code == 409
    upgraded = {**headers, "X-Remote-Agent-Session-Relay-Runtime": "1"}
    current = client.post(BASE + "/attach", headers=upgraded, json=body).json()
    for method, path, payload in [
        ("GET", BASE, None),
        ("POST", BASE + "/poll", None),
        ("POST", BASE + "/readiness", {"generation": current["generation"], "available": True}),
    ]:
        response = client.request(method, path, headers=headers, json=payload)
        assert response.status_code == 409, response.text
        assert response.json()["code"] == "SESSION_CLIENT_UPGRADE_REQUIRED"
    assert client.get(BASE, headers=upgraded).json()["status"] == "connecting"


@pytest.mark.parametrize("ending", ["logout", "expiry"])
@pytest.mark.parametrize("card_state", ["unsent", "delivered", "answered"])
def test_dsh_timed_card_cannot_survive_auth_exit(client, app, ending, card_state):
    headers = {**login(client, app, "alice"), "X-Remote-Agent-Session-Relay-Runtime": "1"}
    target = dict(
        native_session="session-auth-exit",
        device_name="offline-test",
        project="auth-exit",
        session_name="DSH timed question",
        runtime_kind="deepseek-harness",
        runtime_instance="offline-profile",
    )
    attached = client.post(BASE + "/attach", headers=headers, json=target)
    assert attached.status_code == 200
    generation = ready(client, headers)["generation"]
    service = app.state.application
    service.session_relay.receive(event("ask a timed question"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=headers).json()["messages"][0]
    assert client.post(
        BASE + "/messages/" + message["id"] + "/dispatch",
        headers=headers,
        json={"generation": generation},
    ).json()["dispatch"]
    ident = str(uuid4())
    question = dict(
        id=ident,
        generation=generation,
        native_session=target["native_session"],
        turn_id="1",
        message_ids=[message["id"]],
        kind="question",
        async_question=True,
        prompt="Choose yes or no",
    )
    assert client.post(BASE + "/interactions", headers=headers, json=question).status_code == 200
    assert (
        client.post(
            BASE + "/messages/" + message["id"] + "/result",
            headers=headers,
            json=dict(generation=generation, outcome="completed", text="independent work finished"),
        ).status_code
        == 200
    )
    parent = None
    if card_state != "unsent":
        deliver_interaction_card(service)
        parent = next(
            row["receipt"] for row in deliveries(service) if str(row["interaction_id"]) == ident
        )
        assert parent.startswith("om_")
    if card_state == "answered":
        service.session_relay.receive(
            event("/session answer " + ident + " yes"), "offline-test-app"
        )
        assert (
            client.get(BASE + "/interactions/" + ident, headers=headers).json()["state"]
            == "answered"
        )
    if ending == "logout":
        assert client.delete("/v1/auth/session", headers=headers).status_code == 204
    else:
        with service.db.transaction() as connection:
            connection.execute(
                "UPDATE identity.sessions SET expires_at=clock_timestamp()-interval '1 second'"
            )
    for method, path, body in [
        ("POST", BASE + "/poll", None),
        ("GET", BASE + "/interactions/" + ident, None),
        (
            "POST",
            BASE + "/origin",
            dict(
                generation=generation,
                native_session=target["native_session"],
                message_ids=[message["id"]],
                for_result=True,
            ),
        ),
    ]:
        assert client.request(method, path, headers=headers, json=body).status_code == 401
    late = event("/session answer " + ident + " no")
    if parent:
        late["event"]["message"]["parent_id"] = parent
    service.session_relay.receive(late, "offline-test-app")
    assert any(row["kind"] == "login_required" for row in deliveries(service))
    with service.db.transaction() as connection:
        stored = connection.execute(
            "SELECT state,answer FROM session_relay.interactions WHERE id=%s", (ident,)
        ).fetchone()
        assert stored["answer"] != "no"
        connection.execute("UPDATE notifications.session_deliveries SET started_at=NULL")
    if card_state == "unsent":

        class NoCardTransport:
            def send_card(self, *args):
                raise AssertionError("expired question card was delivered")

            def send_post(self, *args):
                return {"message_id": "om_login"}

        service.session_notifications.once(NoCardTransport())
        assert all(
            row["state"] == "cancelled" for row in deliveries(service) if row["interaction_id"]
        )
    fresh = {**login(client, app, "alice"), "X-Remote-Agent-Session-Relay-Runtime": "1"}
    rebound = client.post(BASE + "/attach", headers=fresh, json=target)
    assert rebound.status_code == 200
    assert rebound.json()["generation"] > generation
    ready(client, fresh)
    repeated = event("/session answer " + ident + " no")
    service.session_relay.receive(repeated, "offline-test-app")
    result = client.get(BASE + "/interactions/" + ident, headers=fresh)
    assert result.status_code == 404
    assert result.json()["code"] == "SESSION_REQUEST_INVALID"
    assert client.post(BASE + "/poll", headers=fresh).json()["messages"] == []
    with service.db.transaction() as connection:
        unchanged = connection.execute(
            "SELECT state,answer FROM session_relay.interactions WHERE id=%s", (ident,)
        ).fetchone()
        assert unchanged == {"state": "cancelled", "answer": None}
