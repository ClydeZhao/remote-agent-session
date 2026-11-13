import httpx
import pytest

from remote_agent_session.feishu import Feishu


@pytest.mark.parametrize(
    "failure,operation,stage,kind,status",
    [
        ("token_timeout", "token", "transport", "ReadTimeout", None),
        ("message_status", "message", "http_status", "HTTPStatusError", 503),
        ("message_json", "message", "json", "JSONDecodeError", None),
        ("message_schema", "message", "schema", "TypeError", None),
    ],
)
def test_feishu_transport_diagnostics_exclude_secrets(failure, operation, stage, kind, status):
    secret = "synthetic-private-token-body-url"
    feishu = Feishu("synthetic", secret)
    feishu.client.close()

    def respond(request):
        if request.url.path.endswith("tenant_access_token/internal"):
            if failure == "token_timeout":
                raise httpx.ReadTimeout(secret, request=request)
            return httpx.Response(
                200, json={"code": 0, "tenant_access_token": secret, "expire": 3600}
            )
        if failure == "message_status":
            return httpx.Response(503, text=secret)
        if failure == "message_json":
            return httpx.Response(200, text=secret)
        return httpx.Response(200, json=[secret])

    feishu.client = httpx.Client(
        base_url="https://example.invalid", transport=httpx.MockTransport(respond)
    )
    try:
        with pytest.raises(ConnectionError) as caught:
            feishu.send_post("synthetic", {"text": secret}, "synthetic")
        error = caught.value
        assert error.operation == operation and error.stage == stage
        assert error.exception_type == kind and error.http_status == status
        assert secret not in str(error) and "example.invalid" not in str(error)
        assert str(error) == error.diagnostic
    finally:
        feishu.client.close()
