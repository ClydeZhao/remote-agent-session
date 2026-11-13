from uuid import uuid4

import pytest

from remote_agent_session.session_relay.service import SessionRelay

from .helpers import login
from .test_session_relay import BASE, deliveries, event
from .test_summary import completed


def claim(client, headers, view, ident=None):
    body = {"generation": view["generation"], "claim_id": str(ident or uuid4())}
    response = client.post(BASE + "/summaries/claim", headers=headers, json=body)
    assert response.status_code == 200, response.text
    return body, response.json()["job"]


def test_pending_summary_is_atomic_with_body_and_result_retry(client, app):
    headers, view, service, path = completed(client, app, summary_pending=True)
    body = {
        "generation": view["generation"],
        "outcome": "completed",
        "text": "完整正文",
        "summary_pending": True,
    }
    for _ in range(2):
        assert client.post(path + "/result", headers=headers, json=body).status_code == 200
    with service.db.transaction() as c:
        jobs = c.execute("SELECT * FROM session_relay.summary_jobs").fetchall()
    assert len(jobs) == 1 and jobs[0]["state"] == "pending"
    assert len([d for d in deliveries(service) if "完整正文" in d["content"]]) == 1
    restarted = SessionRelay(service.db, service.session_notifications)
    user = service.identity.authenticate(headers["Authorization"].removeprefix("Bearer "))
    from remote_agent_session.session_relay.models import RelaySummaryClaim

    claimed = restarted.claim_summary(
        user, RelaySummaryClaim(generation=view["generation"], claim_id=uuid4())
    )
    assert claimed["job"]["text"] == "完整正文"


def test_claim_and_summary_lost_ack_retry_are_idempotent(client, app):
    headers, view, service, path = completed(client, app, summary_pending=True)
    request, job = claim(client, headers, view)
    _, retry = claim(client, headers, view, request["claim_id"])
    assert retry == job and str(job["message_id"]) == path.split("/")[-1]
    body = {**request, "summary": "已完成，仍待用户核对"}
    for _ in range(2):
        assert client.post(path + "/summary", headers=headers, json=body).status_code == 200
    _, replay = claim(client, headers, view, request["claim_id"])
    assert replay is None
    assert len([d for d in deliveries(service) if "已完成，仍待用户核对" in d["content"]]) == 1
    assert (
        client.post(
            path + "/summary", headers=headers, json={**body, "summary": "变更"}
        ).status_code
        == 409
    )
    with service.db.transaction() as c:
        job = c.execute("SELECT state,text FROM session_relay.summary_jobs").fetchone()
    assert job == {"state": "completed", "text": ""}


def test_expired_claim_never_reassigned_or_revived_and_fallback_once(client, app):
    headers, view, service, path = completed(client, app, summary_pending=True)
    request, job = claim(client, headers, view)
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.summary_jobs SET lease_until=clock_timestamp()-interval '1 second'"
        )
    for _ in range(2):
        assert claim(client, headers, view, request["claim_id"])[1] is None
        service.session_relay.expire()
    assert claim(client, headers, view)[1] is None
    assert (
        client.post(
            path + "/summary", headers=headers, json={**request, "summary": "迟到结果"}
        ).status_code
        == 409
    )
    notices = [d for d in deliveries(service) if "TL;DR 暂不可用" in d["content"]]
    assert len(notices) == 1
    assert len([d for d in deliveries(service) if "完整正文" in d["content"]]) == 1


@pytest.mark.parametrize("claimed", [False, True])
@pytest.mark.parametrize("action", ["detach", "switch", "logout"])
def test_stale_summary_jobs_do_not_leak_or_reappear(client, app, claimed, action):
    headers, view, service, path = completed(client, app, summary_pending=True)
    request = {"generation": view["generation"], "claim_id": str(uuid4())}
    if claimed:
        request, _ = claim(client, headers, view)
    if action == "detach":
        assert (
            client.post(
                BASE + "/detach", headers=headers, json={"generation": view["generation"]}
            ).status_code
            == 200
        )
    elif action == "switch":
        target = {key: view[key] for key in ("device_name", "project", "session_name")}
        target["native_session"] = str(uuid4())
        assert client.post(BASE + "/attach", headers=headers, json=target).status_code == 200
    else:
        assert client.delete("/v1/auth/session", headers=headers).status_code == 204
    service.session_relay.expire_summaries()
    assert client.post(BASE + "/summaries/claim", headers=headers, json=request).status_code in (
        401,
        409,
    )
    assert client.post(
        path + "/summary", headers=headers, json={**request, "summary": "不该外发"}
    ).status_code in (401, 409)
    with service.db.transaction() as c:
        job = c.execute("SELECT state,text FROM session_relay.summary_jobs").fetchone()
    assert job == {"state": "cancelled", "text": ""}
    assert not any(
        "不该外发" in d["content"] or "TL;DR 暂不可用" in d["content"] for d in deliveries(service)
    )


def test_empty_claim_id_is_tombstoned_and_does_not_consume_next_job(client, app):
    headers, view, service, path = completed(client, app)
    old, job = claim(client, headers, view)
    assert job is None
    with service.db.transaction() as c:
        c.execute(
            "INSERT INTO session_relay.summary_jobs(message_id,user_id,device_id,generation,text) SELECT id,user_id,device_id,generation,'待摘要' FROM session_relay.messages WHERE id=%s",
            (path.split("/")[-1],),
        )
    assert claim(client, headers, view, old["claim_id"])[1] is None
    assert claim(client, headers, view)[1]["text"] == "待摘要"


def test_claim_does_not_cross_user_or_device(client, app):
    headers, view, service, path = completed(client, app, summary_pending=True)
    other = login(client, app)
    response = client.post(
        BASE + "/summaries/claim",
        headers=other,
        json={"generation": view["generation"], "claim_id": str(uuid4())},
    )
    assert response.status_code == 403
    assert claim(client, headers, view)[1]["text"] == "完整正文"


def test_multiple_jobs_have_separate_claims_and_body_before_summary(client, app):
    headers, view, service, path = completed(client, app, summary_pending=True)
    service.session_relay.receive(event("下一轮"), "offline-test-app")
    msg = client.post(BASE + "/poll", headers=headers).json()["messages"][0]
    second = BASE + "/messages/" + msg["id"]
    gen = {"generation": view["generation"]}
    assert client.post(second + "/dispatch", headers=headers, json=gen).status_code == 200
    assert (
        client.post(
            second + "/result",
            headers=headers,
            json={**gen, "outcome": "completed", "text": "第二轮正文", "summary_pending": True},
        ).status_code
        == 200
    )
    req1, job1 = claim(client, headers, view)
    req2, job2 = claim(client, headers, view)
    assert job1["message_id"] != job2["message_id"]
    assert claim(client, headers, view, req1["claim_id"])[1] == job1
    assert (
        client.post(
            path + "/summary", headers=headers, json={**req2, "summary": "串错"}
        ).status_code
        == 409
    )
    assert (
        client.post(
            path + "/summary", headers=headers, json={**req1, "summary": "第一轮摘要"}
        ).status_code
        == 200
    )
    with service.db.transaction() as c:
        rows = c.execute(
            "SELECT message_key FROM notifications.session_deliveries WHERE group_key=%s ORDER BY created_at,message_key",
            (job1["message_id"],),
        ).fetchall()
    assert ":0-body:" in rows[0]["message_key"] and ":1-summary:" in rows[-1]["message_key"]


@pytest.mark.parametrize("outcome", ["mixed", "empty_output", "unknown"])
def test_pending_summary_rejected_without_remote_completed_body(client, app, outcome):
    headers, view, service, path = completed(client, app)
    response = client.post(
        path + "/result",
        headers=headers,
        json={"generation": view["generation"], "outcome": outcome, "summary_pending": True},
    )
    assert response.status_code == 422
    with app.state.application.db.transaction() as c:
        assert not c.execute("SELECT 1 FROM session_relay.summary_jobs").fetchall()


def test_summary_checks_lease_even_when_background_expiry_has_not_run(client, app, monkeypatch):
    headers, view, service, path = completed(client, app, summary_pending=True)
    request, job = claim(client, headers, view)
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.summary_jobs SET lease_until=clock_timestamp()-interval '1 second'"
        )
    monkeypatch.setattr(service.session_relay, "expire_summaries", lambda: False)
    response = client.post(
        path + "/summary", headers=headers, json={**request, "summary": "逾期不接受"}
    )
    assert response.status_code == 409
    assert not any("逾期不接受" in d["content"] for d in deliveries(service))
