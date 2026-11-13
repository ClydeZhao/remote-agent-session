import json
import logging
import re
from uuid import NAMESPACE_URL, uuid4, uuid5

from remote_agent_session.errors import DomainError
from remote_agent_session.feishu import FeishuError, FeishuTransportError
from remote_agent_session.notifications.cards import session_card


def session_source(binding):
    agent = {
        "codex": "Codex",
        "deepseek-harness": "DSH",
    }.get(binding.get("runtime_kind", "codex"))
    name = binding.get("session_name") or ""
    session = binding.get("native_session")
    if session:
        name += "（" + session[-6:] + "）"
    label = " · ".join(
        value
        for value in (binding.get("device_name"), agent, binding.get("project"), name)
        if value
    )
    return label.replace("\n", " ").replace("\r", " ").replace("<", "&lt;").replace(">", "&gt;")


class SessionNotifications:
    def __init__(self, db, identity):
        self.db, self.identity = db, identity

    def login_notice_exists(self, c, user_id, event_id):
        return bool(
            c.execute(
                "SELECT 1 FROM notifications.session_deliveries WHERE user_id=%s AND message_key=%s AND kind='login_required'",
                (user_id, "session-login:" + event_id),
            ).fetchone()
        )

    def login_required(self, c, user_id, event_id):
        key = "session-login:" + event_id
        c.execute(
            """INSERT INTO notifications.session_deliveries
            (id,user_id,generation,message_key,group_key,content,kind)
            VALUES (%s,%s,0,%s,%s,%s,'login_required') ON CONFLICT(message_key) DO NOTHING""",
            (
                uuid5(NAMESPACE_URL, key),
                user_id,
                key,
                key,
                "这台电脑已退出登录或登录已过期，本条消息未交给 Agent。请在本地让 Agent 帮你重新登录 Remote Agent Session、连接原会话，再发送这条消息。",
            ),
        )

    def working(self, c, message):
        if message["platform_message_id"]:
            c.execute(
                "INSERT INTO notifications.session_activity(message_id,platform_message_id) VALUES (%s,%s) ON CONFLICT DO NOTHING",
                (message["id"], message["platform_message_id"]),
            )

    def activity_once(self, transport):
        with self.db.transaction() as c:
            if not c.execute("SELECT pg_try_advisory_xact_lock(763918206) AS held").fetchone()[
                "held"
            ]:
                return False
            row = c.execute("""WITH activity AS (
                SELECT a.*,COALESCE(m.state='dispatching' AND b.generation=m.generation
                AND b.device_id=m.device_id AND b.desired AND b.status='ready'
                AND b.heartbeat>clock_timestamp()-interval '45 seconds'
                AND NOT s.revoked AND s.expires_at>clock_timestamp()
                AND NOT EXISTS (
                    SELECT 1 FROM notifications.session_activity newer
                    JOIN session_relay.messages nm ON nm.id=newer.message_id
                    WHERE nm.user_id=m.user_id AND nm.device_id=m.device_id
                    AND nm.generation=m.generation
                    AND (nm.dispatched_at,nm.created_at,nm.id)>(m.dispatched_at,m.created_at,m.id)
                ),false) AS active
                FROM notifications.session_activity a
                JOIN session_relay.messages m ON m.id=a.message_id
                LEFT JOIN session_relay.bindings b ON b.user_id=m.user_id
                LEFT JOIN identity.sessions s ON s.id=m.device_id
            ) SELECT * FROM activity WHERE applied IS DISTINCT FROM active
            AND next_attempt<=clock_timestamp() ORDER BY active,next_attempt LIMIT 1""").fetchone()
            if not row:
                return False
            lease_id = uuid4()
            c.execute(
                "UPDATE notifications.session_activity SET applied=NULL,lease_id=%s,next_attempt=clock_timestamp()+interval '10 minutes' WHERE message_id=%s",
                (lease_id, row["message_id"]),
            )
        applied = None
        try:
            transport.set_typing(row["platform_message_id"], row["active"])
            applied = row["active"]
        except (FeishuError, ConnectionError) as error:
            diagnostic = (
                "platform_rejected:" + str(error.code)
                if isinstance(error, FeishuError)
                else error.diagnostic
                if isinstance(error, FeishuTransportError)
                else "transport_failure"
            )
            logging.warning("Session typing sync failed: %s", diagnostic)
        with self.db.transaction() as c:
            c.execute(
                "UPDATE notifications.session_activity SET applied=%s,lease_id=NULL,next_attempt=clock_timestamp()+(%s * interval '1 second') WHERE message_id=%s AND lease_id=%s",
                (applied, 30 if applied is None else 0, row["message_id"], lease_id),
            )
        return True

    def enqueue(self, c, binding, key, content, card_title=None):
        footer = "> " + session_source(binding)

        def payload_size(text):
            if card_title:
                payload = session_card(card_title, text, footer)
            else:
                payload = {
                    "zh_cn": {"content": [[{"tag": "md", "text": text + "\n\n---\n" + footer}]]}
                }
            return len(
                json.dumps(
                    {"content": json.dumps(payload, ensure_ascii=False)}, ensure_ascii=False
                ).encode()
            )

        stripped = re.sub(r"<\s*/?\s*(at|person)\b[^>]*>", "", content, flags=re.I)
        if binding.get("interaction_id"):
            if card_title == "需要授权" and stripped != content:
                raise DomainError(
                    "SESSION_INTERACTION_UNDISPLAYABLE",
                    "这条请求含有飞书会识别为提及的内容，无法完整展示。请回原会话核对后再发起。",
                    "user_action",
                    422,
                )
            if payload_size(stripped) > 24000:
                raise DomainError(
                    "SESSION_INTERACTION_TOO_LARGE",
                    "这条请求无法完整放进一张飞书卡片。请回原会话核对后再发起。",
                    "user_action",
                    422,
                )
            parts = [stripped]
        else:
            content = stripped
            parts = []
            current = ""
            fence = ""
            for line in content.splitlines(keepends=True):
                segments = [line[i : i + 1000] for i in range(0, len(line), 1000)] or [line]
                for segment in segments:
                    if payload_size(current + segment + "\n```") > 24000:
                        parts.append(current + ("\n```" if fence else ""))
                        current = fence + "\n" if fence else ""
                    current += segment
                if line.lstrip().startswith("```"):
                    fence = "" if fence else line.strip()
            if current or not parts:
                parts.append(current + ("\n```" if fence else ""))
        for i, part in enumerate(parts):
            message_key = f"{key}:{i:06d}"
            c.execute(
                """INSERT INTO notifications.session_deliveries
                (id,user_id,generation,message_key,group_key,content,device_id,card,interaction_id) VALUES (%s,%s,%s,%s,%s,%s,%s,%s::jsonb,%s)
                ON CONFLICT(message_key) DO NOTHING""",
                (
                    uuid5(NAMESPACE_URL, "session:" + message_key),
                    binding["user_id"],
                    binding["generation"],
                    message_key,
                    key.split(":")[0],
                    part + "\n\n---\n" + footer,
                    binding.get("delivery_device_id"),
                    json.dumps(
                        session_card(card_title, part, footer),
                        ensure_ascii=False,
                    )
                    if card_title
                    else None,
                    binding.get("interaction_id"),
                ),
            )

    def once(self, transport):
        with self.db.transaction() as c:
            if not c.execute("SELECT pg_try_advisory_xact_lock(763918205) AS held").fetchone()[
                "held"
            ]:
                return False
            c.execute("""UPDATE notifications.session_deliveries SET state='unknown'
                WHERE state='sending' AND started_at < clock_timestamp()-interval '30 seconds'""")
            c.execute(
                "UPDATE notifications.session_deliveries SET state='unconfirmed',last_error=CASE WHEN last_error LIKE 'delivery_unknown:%' THEN last_error ELSE 'delivery_unknown' END WHERE state='unknown' AND started_at<clock_timestamp()-interval '30 seconds'"
            )
            c.execute(
                "UPDATE notifications.session_deliveries d SET state='cancelled',last_error='earlier_part_failed' WHERE d.state='pending' AND EXISTS (SELECT 1 FROM notifications.session_deliveries f WHERE f.user_id=d.user_id AND f.generation=d.generation AND f.group_key=d.group_key AND f.state IN ('failed','unconfirmed'))"
            )
            c.execute("""UPDATE notifications.session_deliveries d SET state='cancelled'
                WHERE d.state='pending' AND d.interaction_id IS NOT NULL AND NOT EXISTS (
                    SELECT 1 FROM session_relay.interactions i WHERE i.id=d.interaction_id
                    AND i.state='pending' AND i.expires_at>clock_timestamp()
                    AND NOT EXISTS (SELECT 1 FROM session_relay.messages m WHERE m.id=ANY(i.message_ids) AND NOT (m.state='dispatching' OR (i.async_question AND m.state IN ('completed','empty_output')))))""")
            if c.execute(
                "SELECT 1 FROM notifications.session_deliveries WHERE started_at>clock_timestamp()-interval '1 second' LIMIT 1"
            ).fetchone():
                return False
            c.execute("""UPDATE notifications.session_deliveries d SET state='cancelled'
                WHERE d.state='pending' AND d.kind<>'login_required' AND d.not_before<=clock_timestamp() AND ((d.generation=0 AND NOT EXISTS (SELECT 1 FROM identity.sessions s WHERE s.id=d.device_id AND s.user_id=d.user_id AND NOT s.revoked AND s.expires_at>clock_timestamp())) OR (d.generation>0 AND NOT EXISTS (
                    SELECT 1 FROM session_relay.bindings b JOIN identity.sessions s ON s.id=b.device_id
                    WHERE b.user_id=d.user_id AND b.generation=d.generation AND NOT s.revoked AND s.expires_at>clock_timestamp())))""")
            row = c.execute("""SELECT d.* FROM notifications.session_deliveries d
                WHERE d.state='pending' AND d.not_before<=clock_timestamp() AND NOT EXISTS (
                    SELECT 1 FROM notifications.session_deliveries earlier
                    WHERE earlier.user_id=d.user_id AND earlier.generation=d.generation
                    AND earlier.group_key=d.group_key
                    AND (earlier.created_at,earlier.message_key)<(d.created_at,d.message_key)
                    AND earlier.state IN ('pending','sending','unknown'))
                ORDER BY d.created_at,d.message_key FOR UPDATE SKIP LOCKED LIMIT 1""").fetchone()
            if not row:
                return False
            target = self.identity.binding(c, row["user_id"])
            c.execute(
                "UPDATE notifications.session_deliveries SET state='sending',started_at=clock_timestamp(),attempts=attempts+1 WHERE id=%s",
                (row["id"],),
            )
        try:
            if row["card"] is not None:
                receipt = transport.send_card(target["subject"], row["card"], str(row["id"]))
            else:
                receipt = transport.send_post(
                    target["subject"],
                    {"zh_cn": {"content": [[{"tag": "md", "text": row["content"]}]]}},
                    str(row["id"]),
                )
            message_id = receipt["message_id"]
        except FeishuError as error:
            with self.db.transaction() as c:
                c.execute(
                    "UPDATE notifications.session_deliveries SET state=CASE WHEN attempts>=3 THEN 'failed' ELSE 'pending' END,last_error=%s,not_before=clock_timestamp()+interval '5 seconds' WHERE id=%s AND state='sending'",
                    ("platform_rejected:" + str(error.code), row["id"]),
                )
            return True
        except Exception as error:
            diagnostic = (
                error.diagnostic if isinstance(error, FeishuTransportError) else "delivery_unknown"
            )
            with self.db.transaction() as c:
                c.execute(
                    "UPDATE notifications.session_deliveries SET state='unknown',last_error=%s WHERE id=%s AND state='sending'",
                    (diagnostic, row["id"]),
                )
            return True
        with self.db.transaction() as c:
            c.execute(
                "UPDATE notifications.session_deliveries SET state='accepted',receipt=%s WHERE id=%s AND state IN ('sending','unknown')",
                (message_id, row["id"]),
            )
        return True

    def attention_delivery_state(self, c, user_id, interaction_id, *, cancel_pending=False):
        row = c.execute(
            """SELECT state,receipt FROM notifications.session_deliveries
            WHERE user_id=%s AND interaction_id=%s FOR UPDATE""",
            (user_id, interaction_id),
        ).fetchone()
        if not row:
            return None
        state = row["state"]
        if state == "accepted" and not (row["receipt"] or "").startswith("om_"):
            return "unconfirmed"
        if cancel_pending and state == "pending":
            c.execute(
                """UPDATE notifications.session_deliveries SET state='cancelled'
                WHERE user_id=%s AND interaction_id=%s AND state='pending'""",
                (user_id, interaction_id),
            )
            return "cancelled"
        return state

    def interaction_receipt(self, c, user_id, interaction_id):
        return (
            c.execute(
                """SELECT 1 FROM notifications.session_deliveries
            WHERE user_id=%s AND interaction_id=%s AND state='accepted' AND left(receipt, 3) = 'om_' LIMIT 1""",
                (user_id, interaction_id),
            ).fetchone()
            is not None
        )

    def interaction_for_receipt(self, c, user_id, receipt):
        return c.execute(
            """SELECT interaction_id FROM notifications.session_deliveries
            WHERE user_id=%s AND receipt=%s AND state='accepted' AND interaction_id IS NOT NULL""",
            (user_id, receipt),
        ).fetchone()

    def has_issue(self, c, row):
        return bool(
            c.execute(
                "SELECT 1 FROM notifications.session_deliveries WHERE user_id=%s AND generation=%s AND state IN ('unknown','unconfirmed','failed') LIMIT 1",
                (row["user_id"], row["generation"]),
            ).fetchone()
        )
