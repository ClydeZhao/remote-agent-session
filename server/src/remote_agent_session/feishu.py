import json
import math
import threading
import time

import httpx


class FeishuError(Exception):
    def __init__(self, code):
        self.code = code if type(code) is int and 0 < code < 1000000000 else None
        super().__init__("Feishu request rejected (code " + str(self.code) + ")")


class FeishuTransportError(ConnectionError):
    def __init__(self, operation, stage, error, status=None):
        self.operation = operation if operation in ("token", "message", "request") else "request"
        self.stage = stage if stage in ("transport", "http_status", "json", "schema") else "schema"
        allowed = {
            "ConnectTimeout",
            "ReadTimeout",
            "WriteTimeout",
            "PoolTimeout",
            "ConnectError",
            "ReadError",
            "WriteError",
            "RemoteProtocolError",
            "LocalProtocolError",
            "HTTPStatusError",
            "JSONDecodeError",
            "ValueError",
            "KeyError",
            "TypeError",
        }
        self.exception_type = (
            type(error).__name__ if type(error).__name__ in allowed else "OtherError"
        )
        self.http_status = status if type(status) is int and 100 <= status <= 599 else None
        self.diagnostic = ":".join(
            ("delivery_unknown", self.operation, self.stage, self.exception_type)
        )
        if self.http_status is not None:
            self.diagnostic += ":" + str(self.http_status)
        super().__init__(self.diagnostic)


class Feishu:
    def __init__(self, app_id, secret):
        self.app_id, self._secret = app_id, secret
        self._token, self._expires, self._wall_expires = "", 0, 0
        self._lock = threading.Lock()
        self.client = httpx.Client(
            base_url="https://open.feishu.cn/open-apis",
            timeout=12,
            trust_env=False,
            follow_redirects=False,
        )

    def _exchange(self, operation, method, path, **kwargs):
        stage, status = "transport", None
        try:
            response = self.client.request(method, path, **kwargs)
            stage, status = "http_status", response.status_code
            if 400 <= status < 500:
                try:
                    rejection = response.json()
                except ValueError:
                    rejection = None
                if isinstance(rejection, dict):
                    code = rejection.get("code")
                    if type(code) is int and 0 < code < 1000000000:
                        raise FeishuError(code)
            response.raise_for_status()
            stage, status = "json", None
            data = response.json()
            stage = "schema"
            if not isinstance(data, dict) or type(data.get("code")) is not int:
                raise TypeError()
            if data["code"] != 0:
                if not 0 < data["code"] < 1000000000:
                    raise TypeError()
                raise FeishuError(data["code"])
            return data
        except (httpx.HTTPError, ValueError, KeyError, TypeError) as error:
            raise FeishuTransportError(operation, stage, error, status) from None

    def _access_token(self, rejected=None):
        with self._lock:
            if rejected is not None and self._token == rejected:
                self._token = ""
            if (
                not self._token
                or time.monotonic() >= self._expires
                or time.time() >= self._wall_expires
            ):
                started, wall_started = time.monotonic(), time.time()
                data = self._exchange(
                    "token",
                    "POST",
                    "/auth/v3/tenant_access_token/internal",
                    json={"app_id": self.app_id, "app_secret": self._secret},
                )
                token, expire = data.get("tenant_access_token"), data.get("expire")
                if (
                    not isinstance(token, str)
                    or not token
                    or type(expire) not in (int, float)
                    or not math.isfinite(expire)
                    or expire <= 0
                ):
                    raise FeishuTransportError("token", "schema", TypeError())
                self._token = token
                self._expires = started + max(0, expire - 120)
                self._wall_expires = wall_started + max(0, expire - 120)
            return self._token

    def request(self, method, path, **kwargs):
        operation = "message" if path.split("?", 1)[0].startswith("/im/v1/messages") else "request"
        token = self._access_token()
        for attempt in range(2):
            try:
                data = self._exchange(
                    operation, method, path, headers={"Authorization": "Bearer " + token}, **kwargs
                )
                return data.get("data", data)
            except FeishuError as error:
                if error.code != 99991663 or attempt:
                    raise
                token = self._access_token(rejected=token)

    def send_text(self, subject, text, key):
        return self.request(
            "POST",
            "/im/v1/messages",
            params={"receive_id_type": "open_id"},
            json={
                "receive_id": subject,
                "msg_type": "text",
                "content": json.dumps({"text": text}, ensure_ascii=False),
                "uuid": key,
            },
        )

    def send_post(self, subject, content, key):
        return self.request(
            "POST",
            "/im/v1/messages",
            params={"receive_id_type": "open_id"},
            json={
                "receive_id": subject,
                "msg_type": "post",
                "content": json.dumps(content, ensure_ascii=False),
                "uuid": key,
            },
        )

    def send_card(self, subject, card, key):
        return self.request(
            "POST",
            "/im/v1/messages",
            params={"receive_id_type": "open_id"},
            json={
                "receive_id": subject,
                "msg_type": "interactive",
                "content": json.dumps(card, ensure_ascii=False),
                "uuid": key,
            },
        )

    def set_typing(self, message_id, active):
        path = "/im/v1/messages/" + message_id + "/reactions"
        params = {"reaction_type": "Typing", "page_size": 50}
        own = []
        for _ in range(20):
            data = self.request("GET", path, params=params)
            own.extend(
                item["reaction_id"]
                for item in data.get("items", [])
                if item.get("operator", {}).get("operator_type") == "app"
                and item["operator"].get("operator_id") == self.app_id
            )
            if not data.get("has_more"):
                break
            token = data.get("page_token")
            if not token or token == params.get("page_token"):
                raise ConnectionError("Incomplete reaction history")
            params["page_token"] = token
        else:
            raise ConnectionError("Incomplete reaction history")
        if active and not own:
            self.request("POST", path, json={"reaction_type": {"emoji_type": "Typing"}})
        if not active:
            for ident in own:
                self.request("DELETE", path + "/" + ident)

    def permission_reactions(self, message_id, subject):
        choices = set()
        params = {"page_size": 50, "user_id_type": "open_id"}
        for _ in range(20):
            data = self.request(
                "GET", "/im/v1/messages/" + message_id + "/reactions", params=params
            )
            for item in data.get("items", []):
                operator = item.get("operator", {})
                emoji = item.get("reaction_type", {}).get("emoji_type")
                if (
                    operator.get("operator_type") == "user"
                    and operator.get("operator_id") == subject
                    and emoji in ("CheckMark", "CrossMark")
                ):
                    choices.add(emoji)
            if not data.get("has_more"):
                return choices
            token = data.get("page_token")
            if not token or token == params.get("page_token"):
                raise ConnectionError("Incomplete reaction history")
            params["page_token"] = token
        raise ConnectionError("Incomplete reaction history")

    def user_name(self, subject):
        try:
            data = self.request(
                "GET", "/contact/v3/users/" + subject, params={"user_id_type": "open_id"}
            )
            return data.get("user", {}).get("name")
        except (FeishuError, ConnectionError):
            return None

    def quote_message(self, message_id, chat_id):
        if not isinstance(message_id, str) or not message_id.startswith("om_") or not chat_id:
            raise ValueError("Invalid reply reference")
        data = self.request("GET", "/im/v1/messages/" + message_id)
        items = data.get("items", [])
        if len(items) != 1 or items[0].get("chat_id") != chat_id:
            raise ValueError("Reply reference does not belong to this chat")
        content = items[0].get("body", {}).get("content", "")
        if not isinstance(content, str) or len(content) > 8000:
            raise ValueError("Reply reference is unavailable or too large")
        return content
