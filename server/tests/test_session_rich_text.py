import json

import pytest

from .test_session_relay import BASE, attached, deliveries, event, ready


def rich_event(body):
    incoming = event("")
    incoming["event"]["message"].update(message_type="post", content=json.dumps(body))
    return incoming


def test_formatted_text_arrives_once_with_lines_preserved(client, app):
    headers, _, service = attached(client, app)
    ready(client, headers)
    rows = [
        [{"tag": "text", "text": "请调整 footer", "style": ["bold"]}],
        [],
        [{"tag": "text", "text": "Host · Agent · Project · Session", "style": []}],
    ]
    incoming = rich_event({"title": "", "content": rows, "content_v2": rows})
    service.session_relay.receive(incoming, "offline-test-app")
    service.session_relay.receive(incoming, "offline-test-app")
    messages = client.post(BASE + "/poll", headers=headers).json()["messages"]
    assert len(messages) == 1
    assert messages[0]["content"] == "请调整 footer\n\nHost · Agent · Project · Session"


def test_post_title_links_code_and_locale_preserved(client, app):
    headers, _, service = attached(client, app)
    ready(client, headers)
    service.session_relay.receive(
        rich_event(
            {
                "zh_cn": {
                    "title": "任务",
                    "content": [
                        [{"tag": "a", "text": "参考", "href": "https://example.invalid/doc"}],
                        [{"tag": "code_block", "language": "python", "text": "print(1)"}],
                    ],
                }
            }
        ),
        "offline-test-app",
    )
    content = client.post(BASE + "/poll", headers=headers).json()["messages"][0]["content"]
    assert "任务" in content and "https://example.invalid/doc" in content and "print(1)" in content


@pytest.mark.parametrize(
    "body", [{"content": [[{"tag": "img", "image_key": "synthetic"}]]}, {"content": "invalid"}]
)
def test_unsupported_post_reports_instead_of_executing_partial_text(client, app, body):
    headers, _, service = attached(client, app)
    ready(client, headers)
    service.session_relay.receive(rich_event(body), "offline-test-app")
    assert client.post(BASE + "/poll", headers=headers).json()["messages"] == []
    assert any("未交给 agent" in item["content"] for item in deliveries(service))


def test_rich_permission_answer_resolves_request_without_queueing(client, app):
    from .test_session_interactions import interaction

    headers, _, service, body = interaction(client, app, "permission")
    service.session_relay.receive(
        rich_event(
            {"content": [[{"tag": "text", "text": "/session answer " + body["id"] + " deny"}]]}
        ),
        "offline-test-app",
    )
    answer = client.get(BASE + "/interactions/" + body["id"], headers=headers).json()
    assert answer["state"] == "answered" and answer["answer"] == "deny"
    assert len(client.post(BASE + "/poll", headers=headers).json()["messages"]) == 1


def test_struck_out_control_is_not_executed(client, app):
    headers, _, service = attached(client, app)
    ready(client, headers)
    incoming = rich_event(
        {"content": [[{"tag": "text", "text": "/session detach", "style": ["lineThrough"]}]]}
    )
    service.session_relay.receive(incoming, "offline-test-app")
    service.session_relay.receive(incoming, "offline-test-app")
    view = client.post(BASE + "/poll", headers=headers).json()
    assert view["desired"] and view["messages"] == []
    assert sum("未交给 agent" in d["content"] for d in deliveries(service)) == 1
