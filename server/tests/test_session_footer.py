from uuid import uuid4

import pytest

from .helpers import login
from .test_session_relay import BASE, deliveries, event, ready


@pytest.mark.parametrize("runtime,label", [("codex", "Codex"), ("deepseek-harness", "DSH")])
def test_relay_footer_uses_runtime_and_device_across_delivery_paths(client, app, runtime, label):
    headers = {**login(client, app, "alice"), "X-Remote-Agent-Session-Relay-Runtime": "1"}
    body = dict(
        native_session="session-123456",
        device_name="lab-machine.local",
        project="demo",
        session_name="结果分析",
        runtime_kind=runtime,
        runtime_instance="profile" if runtime != "codex" else "",
    )
    if runtime == "codex":
        body["native_session"] = str(uuid4())
    response = client.post(BASE + "/attach", headers=headers, json=body)
    assert response.status_code == 200, response.text
    view = ready(client, headers)
    service = app.state.application
    service.session_relay.receive(event("开始分析"), "offline-test-app")
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    message = messages[0]
    path = BASE + "/messages/" + message["id"]
    assert client.post(
        path + "/dispatch", headers=headers, json={"generation": view["generation"]}
    ).json()["dispatch"]
    for kind in ("permission", "question"):
        interaction = dict(
            id=str(uuid4()),
            generation=view["generation"],
            native_session=body["native_session"],
            turn_id=str(uuid4()),
            message_ids=[message["id"]],
            kind=kind,
            prompt="需要确认",
            choices=None,
        )
        response = client.post(BASE + "/interactions", headers=headers, json=interaction)
        assert response.status_code == 200, response.text
    response = client.post(
        path + "/result",
        headers=headers,
        json={"generation": view["generation"], "outcome": "completed", "text": "分析完成"},
    )
    assert response.status_code == 200, response.text
    footer = f"> lab-machine.local · {label} · demo · 结果分析（{body['native_session'][-6:]}）"
    rows = deliveries(service)
    assert len(rows) == 4
    assert all(row["content"].endswith(footer) for row in rows)
    assert all(
        row["card"]["body"]["elements"][-1]["content"] == footer[2:] for row in rows if row["card"]
    )
    replacement = {
        **body,
        "native_session": str(uuid4()),
        "device_name": "second-machine",
        "session_name": "后续分析",
    }
    response = client.post(BASE + "/attach", headers=headers, json=replacement)
    assert response.status_code == 200, response.text
    ready(client, headers)
    switched = deliveries(service)[-1]["content"]
    assert "从：" + footer[2:] in switched
    assert f"到：second-machine · {label} · demo · 后续分析" in switched
