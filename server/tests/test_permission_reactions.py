from uuid import uuid4

import pytest

from .test_session_interactions import interaction
from .test_session_relay import BASE, deliveries


class Reactions:
    app_id = "offline-test-app"

    def __init__(self, choices):
        self.choices = choices
        self.calls = []

    def permission_reactions(self, message, subject):
        self.calls.append((message, subject))
        return self.choices


def reaction(emoji="CheckMark", subject="alice", message="om_permission", event_type="created"):
    return {
        "header": {
            "app_id": "offline-test-app",
            "tenant_key": "offline-test-tenant",
            "event_id": str(uuid4()),
            "event_type": "im.message.reaction." + event_type + "_v1",
        },
        "event": {
            "operator_type": "user",
            "user_id": {"open_id": subject},
            "message_id": message,
            "reaction_type": {"emoji_type": emoji},
        },
    }


def delivered(service, ident):
    with service.db.transaction() as c:
        c.execute(
            "UPDATE notifications.session_deliveries SET state='accepted',receipt='om_permission' WHERE interaction_id=%s",
            (ident,),
        )


@pytest.mark.parametrize("emoji,answer", [("CheckMark", "allow"), ("CrossMark", "deny")])
def test_permission_reaction_answers_exact_request_once(client, app, emoji, answer):
    h, v, a, body = interaction(client, app, "permission")
    delivered(a, body["id"])
    transport = Reactions({emoji})
    incoming = reaction(emoji)
    assert a.session_relay.receive_reaction(incoming, transport)
    assert not a.session_relay.receive_reaction(incoming, transport)
    view = client.get(BASE + "/interactions/" + body["id"], headers=h).json()
    assert view["answer"] == answer
    assert len(client.post(BASE + "/poll", headers=h).json()["messages"]) == 1


def test_conflicting_reactions_wait_until_unambiguous(client, app):
    h, v, a, body = interaction(client, app, "permission")
    delivered(a, body["id"])
    transport = Reactions({"CheckMark", "CrossMark"})
    assert not a.session_relay.receive_reaction(reaction(), transport)
    assert client.get(BASE + "/interactions/" + body["id"], headers=h).json()["state"] == "pending"
    transport.choices = {"CrossMark"}
    assert a.session_relay.receive_reaction(reaction(event_type="deleted"), transport)
    assert client.get(BASE + "/interactions/" + body["id"], headers=h).json()["answer"] == "deny"


@pytest.mark.parametrize(
    "case",
    [
        "other_user",
        "other_card",
        "other_app",
        "other_tenant",
        "app_operator",
        "expired",
        "detach",
        "question",
    ],
)
def test_reactions_cannot_authorize_outside_active_permission(client, app, case):
    h, v, a, body = interaction(client, app, "question" if case == "question" else "permission")
    delivered(a, body["id"])
    incoming = reaction()
    if case == "other_user":
        incoming["event"]["user_id"]["open_id"] = "bob"
    if case == "other_card":
        incoming["event"]["message_id"] = "om_other"
    if case == "other_app":
        incoming["header"]["app_id"] = "other"
    if case == "other_tenant":
        incoming["header"]["tenant_key"] = "other"
    if case == "app_operator":
        incoming["event"]["operator_type"] = "app"
    if case == "expired":
        with a.db.transaction() as c:
            c.execute(
                "UPDATE session_relay.interactions SET expires_at=clock_timestamp()-interval '1 second'"
            )
    if case == "detach":
        client.post(BASE + "/detach", headers=h, json={"generation": v["generation"]})
    transport = Reactions({"CheckMark"})
    assert not a.session_relay.receive_reaction(incoming, transport)
    assert transport.calls == []


def test_reconciliation_recovers_reaction_before_delivery_receipt(client, app):
    h, v, a, body = interaction(client, app, "permission")
    transport = Reactions({"CheckMark"})
    assert not a.session_relay.receive_reaction(reaction(), transport)
    delivered(a, body["id"])
    assert a.session_relay.permission_reactions_once(transport)
    assert client.get(BASE + "/interactions/" + body["id"], headers=h).json()["answer"] == "allow"


def test_reaction_answer_rechecks_binding_after_remote_read(client, app):
    h, v, a, body = interaction(client, app, "permission")
    delivered(a, body["id"])
    transport = Reactions({"CheckMark"})

    def read(message, subject):
        client.post(BASE + "/detach", headers=h, json={"generation": v["generation"]})
        return {"CheckMark"}

    transport.permission_reactions = read
    assert not a.session_relay.receive_reaction(reaction(), transport)
    assert client.get(BASE + "/interactions/" + body["id"], headers=h).json()["answer"] is None


def test_old_cards_do_not_gain_reaction_authorization(client, app):
    h, v, a, body = interaction(client, app, "permission")
    delivered(a, body["id"])
    with a.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.interactions SET reaction_enabled=false WHERE id=%s",
            (body["id"],),
        )
    transport = Reactions({"CheckMark"})
    assert not a.session_relay.receive_reaction(reaction(), transport)
    assert not a.session_relay.permission_reactions_once(transport)
    assert not transport.calls


def test_oversized_permission_does_not_register_or_deliver_partial_card(client, app):
    h, _, a, body = interaction(client, app, "permission")
    before = len(deliveries(a))
    long_request = {
        **body,
        "id": str(uuid4()),
        "prompt": "长" * 9000 + "\n末尾命令",
    }
    response = client.post(BASE + "/interactions", headers=h, json=long_request)
    assert response.status_code == 422
    assert response.json()["code"] == "SESSION_INTERACTION_TOO_LARGE"
    assert len(deliveries(a)) == before
    assert client.get(BASE + "/interactions/" + long_request["id"], headers=h).status_code == 404


def test_reaction_cannot_authorize_a_legacy_partial_card(client, app):
    h, _, a, body = interaction(client, app, "permission")
    delivered(a, body["id"])
    with a.db.transaction() as c:
        c.execute(
            """INSERT INTO notifications.session_deliveries
            (id,user_id,generation,message_key,group_key,content,device_id,card,interaction_id)
            SELECT %s,user_id,generation,message_key||'-remaining',group_key,content,device_id,card,interaction_id
            FROM notifications.session_deliveries WHERE interaction_id=%s""",
            (uuid4(), body["id"]),
        )
    transport = Reactions({"CheckMark"})
    assert not a.session_relay.receive_reaction(reaction(), transport)
    assert not transport.calls
    assert client.get(BASE + "/interactions/" + body["id"], headers=h).json()["state"] == "pending"


def test_permission_with_mention_markup_is_not_shown_as_a_modified_command(client, app):
    h, _, a, body = interaction(client, app, "permission")
    before = len(deliveries(a))
    request = {
        **body,
        "id": str(uuid4()),
        "prompt": "```bash\\nprintf '<person>value</person>'\\n```",
    }
    response = client.post(BASE + "/interactions", headers=h, json=request)
    assert response.status_code == 422
    assert response.json()["code"] == "SESSION_INTERACTION_UNDISPLAYABLE"
    assert len(deliveries(a)) == before
