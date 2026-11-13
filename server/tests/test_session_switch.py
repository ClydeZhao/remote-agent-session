import os
import subprocess
from urllib.parse import urlsplit, urlunsplit
from uuid import uuid4

import psycopg
import pytest
from psycopg import sql

from remote_agent_session.session_relay.service import SessionRelay

from .helpers import login
from .test_session_relay import BASE, attached, deliveries, event, ready


def attach_target(client, headers, target):
    response = client.post(BASE + "/attach", headers=headers, json=target)
    assert response.status_code == 200, response.text
    return response.json()


def target_body(view):
    return {key: view[key] for key in ("native_session", "device_name", "project", "session_name")}


def new_target():
    return dict(
        native_session=str(uuid4()),
        device_name="第二台电脑",
        project="assembly",
        session_name="组装检查",
    )


def assert_switch(content, previous, current):
    assert (
        "已切换到会话「" + current["session_name"] + "」。接下来可以直接在这里继续聊。" in content
    )
    assert "从：" in content and "到：" in content
    for target in (previous, current):
        for key in ("device_name", "project", "session_name"):
            assert target[key] in content
        assert target["native_session"][-6:] in content


@pytest.mark.parametrize("other_device", [False, True])
def test_switch_ack_identifies_previous_and_current_targets(client, app, other_device):
    headers, previous, service = attached(client, app)
    ready(client, headers)
    if other_device:
        headers = login(client, app, "alice")
    target = new_target()
    current = attach_target(client, headers, target)
    assert len(deliveries(service)) == 1
    ready(client, headers)
    assert_switch(deliveries(service)[-1]["content"], previous, current)
    ready(client, headers)
    attach_target(client, headers, target)
    assert len(deliveries(service)) == 2


@pytest.mark.parametrize("retry", ["attach", "resume"])
def test_failed_switch_preserves_previous_target_across_retry_and_service_restart(
    client, app, retry
):
    headers, previous, service = attached(client, app)
    ready(client, headers)
    target = new_target()
    current = attach_target(client, headers, target)
    failed = client.post(
        BASE + "/readiness",
        headers=headers,
        json={"generation": current["generation"], "available": False},
    )
    assert failed.status_code == 200
    failure = deliveries(service)[-1]["content"]
    assert "旧会话已不再转发" in failure and "新会话尚未连上" in failure
    assert previous["session_name"] in failure and target["session_name"] in failure
    service.session_relay = SessionRelay(service.db, service.session_notifications)
    if retry == "attach":
        current = attach_target(client, headers, target)
    else:
        service.session_relay.receive(event("/session resume"), "offline-test-app")
    ready(client, headers)
    assert_switch(deliveries(service)[-1]["content"], previous, target)
    with service.db.transaction() as c:
        c.execute(
            "UPDATE session_relay.bindings SET heartbeat=clock_timestamp()-interval '2 minutes'"
        )
    service.session_relay.expire()
    ready(client, headers)
    assert "已切换到会话" not in deliveries(service)[-1]["content"]


def test_same_visible_target_after_login_does_not_report_switch(client, app):
    headers, previous, service = attached(client, app)
    ready(client, headers)
    headers = login(client, app, "alice")
    target = target_body(previous)
    target.update(project="renamed project", session_name="renamed session")
    attach_target(client, headers, target)
    ready(client, headers)
    assert "已切换到会话" not in deliveries(service)[-1]["content"]


def test_latest_replacement_is_used_and_stale_readiness_cannot_ack(client, app):
    headers, previous, service = attached(client, app)
    ready(client, headers)
    middle = attach_target(client, headers, new_target())
    final_target = {**new_target(), "session_name": "第三个会话"}
    current = attach_target(client, headers, final_target)
    stale = client.post(
        BASE + "/readiness",
        headers=headers,
        json={"generation": middle["generation"], "available": True},
    )
    assert stale.status_code == 409
    assert len(deliveries(service)) == 1
    ready(client, headers)
    assert_switch(deliveries(service)[-1]["content"], middle, current)
    assert previous["session_name"] not in deliveries(service)[-1]["content"]


@pytest.mark.parametrize("detached", [False, True])
def test_host_change_with_same_session_is_a_target_switch(client, app, detached):
    headers, previous, service = attached(client, app)
    ready(client, headers)
    if detached:
        client.post(BASE + "/detach", headers=headers, json={"generation": previous["generation"]})
    target = {**target_body(previous), "device_name": "另一台电脑"}
    current = attach_target(client, headers, target)
    assert current["generation"] > previous["generation"]
    ready(client, headers)
    assert_switch(deliveries(service)[-1]["content"], previous, current)


def test_summary_migration_preserves_existing_binding(pg):
    database = "switch_" + uuid4().hex
    url = urlsplit(pg["url"])
    migrated_url = urlunsplit(url._replace(path="/" + database))
    with psycopg.connect(pg["url"], autocommit=True) as c:
        c.execute(sql.SQL("CREATE DATABASE {}").format(sql.Identifier(database)))
    try:
        env = {**os.environ, "REMOTE_AGENT_SESSION_DATABASE_URL": migrated_url}
        subprocess.run(["alembic", "upgrade", "0001"], env=env, check=True, capture_output=True)
        user, device, native = uuid4(), uuid4(), str(uuid4())
        with psycopg.connect(migrated_url) as c:
            c.execute(
                "INSERT INTO identity.users(id,app,tenant,subject) VALUES (%s,'test','test','test')",
                (user,),
            )
            c.execute(
                "INSERT INTO identity.sessions(id,user_id,token_hash,expires_at) VALUES (%s,%s,'synthetic',clock_timestamp()+interval '1 day')",
                (device, user),
            )
            c.execute(
                "INSERT INTO session_relay.bindings(user_id,device_id,native_session,device_name,project,session_name,status) VALUES (%s,%s,%s,'host','project','session','ready')",
                (user, device, native),
            )
        subprocess.run(["alembic", "upgrade", "head"], env=env, check=True, capture_output=True)
        with psycopg.connect(migrated_url) as c:
            row = c.execute(
                "SELECT native_session,device_name,project,session_name,status,previous_binding FROM session_relay.bindings WHERE user_id=%s",
                (user,),
            ).fetchone()
            assert row == (native, "host", "project", "session", "ready", None)
    finally:
        with psycopg.connect(pg["url"], autocommit=True) as c:
            c.execute(sql.SQL("DROP DATABASE {} WITH (FORCE)").format(sql.Identifier(database)))
