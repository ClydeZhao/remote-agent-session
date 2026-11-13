import pytest

from .test_session_relay import BASE, attached, deliveries, event, ready


def completed(client, app, outcome="completed", summary_pending=False):
    headers, view, service = attached(client, app)
    ready(client, headers)
    service.session_relay.receive(event("继续任务"), "offline-test-app")
    message = client.post(BASE + "/poll", headers=headers).json()["messages"][0]
    path = BASE + "/messages/" + message["id"]
    body = {"generation": view["generation"]}
    assert client.post(path + "/dispatch", headers=headers, json=body).status_code == 200
    result = client.post(
        path + "/result",
        headers=headers,
        json={
            **body,
            "outcome": outcome,
            "summary_pending": summary_pending,
            "text": "完整正文" if outcome == "completed" else "",
        },
    )
    assert result.status_code == 200
    return headers, view, service, path


def test_body_available_before_summary_and_summary_is_idempotent(client, app):
    headers, view, service, path = completed(client, app)
    output = [d for d in deliveries(service) if "完整正文" in d["content"]]
    assert len(output) == 1
    assert not any("TL;DR" in d["content"] for d in deliveries(service))
    body = {"generation": view["generation"], "summary": "原结论与不确定性"}
    for _ in range(2):
        assert client.post(path + "/summary", headers=headers, json=body).status_code == 200
    output = [d for d in deliveries(service) if "TL;DR" in d["content"]]
    assert len(output) == 1
    assert (
        client.post(
            path + "/summary", headers=headers, json={**body, "summary": "不同结论"}
        ).status_code
        == 409
    )
    with service.db.transaction() as c:
        rows = c.execute(
            "SELECT message_key,group_key FROM notifications.session_deliveries WHERE group_key=%s ORDER BY created_at,message_key",
            (path.split("/")[-1],),
        ).fetchall()
    assert ":0-body:" in rows[0]["message_key"]
    assert ":1-summary:" in rows[-1]["message_key"]


@pytest.mark.parametrize("outcome", ["mixed", "empty_output", "interrupted", "unknown"])
def test_summary_cannot_publish_body_from_unaccepted_origin(client, app, outcome):
    headers, view, service, path = completed(client, app, outcome)
    assert (
        client.post(
            path + "/summary",
            headers=headers,
            json={"generation": view["generation"], "summary": "不得外发的摘要标记"},
        ).status_code
        == 409
    )
    assert not any("不得外发的摘要标记" in d["content"] for d in deliveries(service))


def test_summary_rejected_after_detach(client, app):
    headers, view, service, path = completed(client, app)
    assert (
        client.post(
            BASE + "/detach", headers=headers, json={"generation": view["generation"]}
        ).status_code
        == 200
    )
    assert (
        client.post(
            path + "/summary",
            headers=headers,
            json={"generation": view["generation"], "summary": "迟到摘要"},
        ).status_code
        == 409
    )
    assert not any("迟到摘要" in d["content"] for d in deliveries(service))
