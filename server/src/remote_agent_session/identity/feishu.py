import re
from uuid import NAMESPACE_URL, uuid5

from remote_agent_session.errors import DomainError
from remote_agent_session.event_time import message_is_fresh
from remote_agent_session.identity.message_text import command_text
from remote_agent_session.identity.models import VerifiedPairingEvent


class FeishuPairing:
    def __init__(self, identity, transport, require_event_time=False):
        self.identity, self.transport = identity, transport
        self.require_event_time = require_event_time

    def receive(self, data):
        header, event = data.get("header", {}), data.get("event", {})
        message, sender = event.get("message", {}), event.get("sender", {})
        if (
            header.get("app_id") != self.transport.app_id
            or header.get("event_type") != "im.message.receive_v1"
        ):
            return
        if sender.get("sender_type") != "user" or message.get("chat_type") != "p2p":
            return
        tenant = header.get("tenant_key") or sender.get("tenant_key")
        if not tenant or (sender.get("tenant_key") and tenant != sender["tenant_key"]):
            return
        subject = sender.get("sender_id", {}).get("open_id")
        text = command_text(message)
        if text is None:
            return
        match = re.fullmatch(r"(?:绑定\s+)?([A-Fa-f0-9]{12})", text)
        confirm = re.fullmatch(r"确认\s+([A-Fa-f0-9]{12})\s+([A-Za-z0-9_-]{43})", text)
        if not subject or not (match or confirm):
            return
        if self.require_event_time and not message_is_fresh(data):
            ident = header.get("event_id")
            if not ident:
                return True
            with self.identity.db.transaction() as c:
                inserted = c.execute(
                    "INSERT INTO identity.events(id) VALUES (%s) ON CONFLICT DO NOTHING RETURNING id",
                    (ident,),
                ).fetchone()
            if inserted:
                self.transport.send_text(
                    subject,
                    "这条登录消息已过期或无法确认发送时间。请回终端获取最新配对码后重新发送。",
                    str(uuid5(NAMESPACE_URL, "pair-expired:" + ident)),
                )
            return True
        code = (match or confirm)[1].upper()
        try:
            challenge = self.identity.pairing_event(
                VerifiedPairingEvent(
                    event_id=header["event_id"],
                    app=header["app_id"],
                    tenant=tenant,
                    subject=subject,
                    private_chat=True,
                    user_code=code,
                    confirmation=confirm[2] if confirm else None,
                )
            )
        except DomainError as exc:
            if exc.body.code in ("PAIRING_INVALID", "CONFIRMATION_REQUIRED"):
                self.transport.send_text(
                    subject,
                    "未能确认登录。请回到原终端按提示重试，使用最新配对码和完整确认指令。",
                    str(uuid5(NAMESPACE_URL, "pair-error:" + header["event_id"])),
                )
            return True
        key = str(uuid5(NAMESPACE_URL, "pair:" + header["event_id"]))
        if challenge:
            self.transport.send_post(
                subject,
                {
                    "zh_cn": {
                        "title": "确认 CLI 登录",
                        "content": [
                            [
                                {
                                    "tag": "text",
                                    "text": "只复制下面代码块里的完整一行，发回此私聊。",
                                }
                            ],
                            [
                                {
                                    "tag": "code_block",
                                    "language": "PlainText",
                                    "text": f"确认 {code} {challenge}",
                                }
                            ],
                            [
                                {
                                    "tag": "text",
                                    "text": "非本人操作请忽略。",
                                }
                            ],
                        ],
                    }
                },
                key,
            )
        elif confirm:
            if hasattr(self.transport, "user_name"):
                self.identity.display_name(tenant, subject, self.transport.user_name(subject))
            self.transport.send_text(
                subject,
                "登录已确认，请回到终端继续。",
                key,
            )
        return True
