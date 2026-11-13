import http.client
import json
from pathlib import Path

from remote_agent_session.api import create_app


def test_contract_is_stable_when_python_changes_http_status_wording(monkeypatch):
    monkeypatch.setitem(http.client.responses, 422, "Unprocessable Content")
    actual = create_app(schema_only=True).openapi()
    expected = json.loads(
        (Path(__file__).resolve().parents[2] / "contracts/openapi.json").read_text()
    )
    assert actual == expected
    responses = [
        operation["responses"]["422"]
        for methods in actual["paths"].values()
        for operation in methods.values()
    ]
    assert responses
    assert all(response["description"] == "Unprocessable Entity" for response in responses)
