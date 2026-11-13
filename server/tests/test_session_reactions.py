import pytest

from remote_agent_session.feishu import Feishu


def test_typing_reconciles_unknown_add_and_delete_without_touching_others():
    feishu = Feishu("own-app", "synthetic")
    records = [
        {"reaction_id": "other", "operator": {"operator_type": "app", "operator_id": "other-app"}}
    ]
    calls = []
    lose_reply = True

    def request(method, path, **kwargs):
        nonlocal lose_reply
        calls.append(method)
        if method == "GET":
            return {"items": list(records), "has_more": False}
        if method == "POST":
            assert kwargs["json"] == {"reaction_type": {"emoji_type": "Typing"}}
            records.append(
                {
                    "reaction_id": "own",
                    "operator": {"operator_type": "app", "operator_id": "own-app"},
                }
            )
        if method == "DELETE":
            assert path.endswith("/own")
            records[:] = [r for r in records if r["reaction_id"] != "own"]
        if lose_reply:
            lose_reply = False
            raise ConnectionError("reply lost after successful mutation")
        return {}

    feishu.request = request
    with pytest.raises(ConnectionError):
        feishu.set_typing("om_test", True)
    feishu.set_typing("om_test", True)
    assert calls.count("POST") == 1
    lose_reply = True
    with pytest.raises(ConnectionError):
        feishu.set_typing("om_test", False)
    feishu.set_typing("om_test", False)
    assert calls.count("DELETE") == 1
    assert [r["reaction_id"] for r in records] == ["other"]
    feishu.client.close()


def test_typing_follows_pages_before_deciding_to_add():
    feishu = Feishu("own-app", "synthetic")
    tokens = []

    def request(method, path, **kwargs):
        assert method == "GET"
        token = kwargs["params"].get("page_token")
        tokens.append(token)
        if token is None:
            return {"items": [], "has_more": True, "page_token": "next"}
        return {
            "items": [
                {
                    "reaction_id": "own",
                    "operator": {"operator_type": "app", "operator_id": "own-app"},
                }
            ],
            "has_more": False,
        }

    feishu.request = request
    feishu.set_typing("om_test", True)
    assert tokens == [None, "next"]
    feishu.client.close()


def test_permission_reactions_only_use_paired_human_and_all_pages():
    feishu = Feishu("own-app", "synthetic")
    tokens = []

    def request(method, path, **kwargs):
        assert method == "GET" and path.endswith("/om_permission/reactions")
        assert kwargs["params"]["user_id_type"] == "open_id"
        token = kwargs["params"].get("page_token")
        tokens.append(token)
        if token is None:
            return {
                "items": [
                    {
                        "operator": {"operator_type": "app", "operator_id": "alice"},
                        "reaction_type": {"emoji_type": "CheckMark"},
                    },
                    {
                        "operator": {"operator_type": "user", "operator_id": "other"},
                        "reaction_type": {"emoji_type": "CheckMark"},
                    },
                    {
                        "operator": {"operator_type": "user", "operator_id": "alice"},
                        "reaction_type": {"emoji_type": "CrossMark"},
                    },
                ],
                "has_more": True,
                "page_token": "next",
            }
        return {
            "items": [
                {
                    "operator": {"operator_type": "user", "operator_id": "alice"},
                    "reaction_type": {"emoji_type": "CheckMark"},
                }
            ],
            "has_more": False,
        }

    feishu.request = request
    assert feishu.permission_reactions("om_permission", "alice") == {"CrossMark", "CheckMark"}
    assert tokens == [None, "next"]
    feishu.client.close()


def test_permission_reactions_reject_incomplete_history():
    feishu = Feishu("own-app", "synthetic")
    feishu.request = lambda *args, **kwargs: {"items": [], "has_more": True}
    with pytest.raises(ConnectionError):
        feishu.permission_reactions("om_permission", "alice")
    feishu.client.close()
