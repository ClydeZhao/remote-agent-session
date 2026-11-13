import hashlib
from uuid import UUID, uuid4

from psycopg.types.json import Jsonb

from remote_agent_session.errors import DomainError
from remote_agent_session.event_time import message_is_fresh
from remote_agent_session.notifications.session import session_source
from remote_agent_session.session_relay.message_text import chat_text


class SessionRelay:
    def __init__(self, db, notifications, quote_reader=None, require_event_time=False):
        self.db, self.notifications = db, notifications
        self.quote_reader = quote_reader
        self.require_event_time = require_event_time

    def _row(self, c, user, device=True):
        row = c.execute(
            """SELECT *,heartbeat>clock_timestamp()-interval '45 seconds' AS online
            FROM session_relay.bindings WHERE user_id=%s FOR UPDATE""",
            (user["user_id"],),
        ).fetchone()
        if not row:
            raise DomainError(
                "SESSION_NOT_ATTACHED",
                "尚未连接会话。请在本地 Agent 会话里对 Agent 说「把当前会话连到飞书」。",
                "user_action",
                404,
            )
        self._require_runtime_client(user, row)
        if device and row["device_id"] != user["session_id"]:
            raise DomainError(
                "SESSION_OTHER_DEVICE",
                "当前登录无法操作这条连接。请在已连接的原会话操作；如需改连当前会话，请让本地 Agent 连接飞书。",
                "user_action",
                403,
            )
        row["delivery_warning"] = self.notifications.has_issue(c, row)
        return row

    def _require_runtime_client(self, user, row):
        if (
            not user.get("relay_runtime_aware", True)
            and row.get("runtime_kind", "codex") != "codex"
        ):
            raise DomainError(
                "SESSION_CLIENT_UPGRADE_REQUIRED",
                "当前会话需要新版 Remote Agent Session CLI，请在原应用更新后继续。",
                "user_action",
                409,
            )

    def status_message(self, row):
        status = row["status"]
        if status == "revoked":
            message = "这台电脑的登录已失效。请在本地让 Agent 帮你重新登录 Remote Agent Session，再连接会话。"
        elif status == "replaced":
            message = "当前连接已被替换，不再转发消息。请在飞书查看新连接的确认消息；如需切回，请在目标会话里让 Agent 重新连接。"
        elif not row["desired"]:
            message = "已断开飞书对话，已开始的工作继续运行。"
            message += (
                "需要继续时，在飞书发送 /session resume 恢复上次会话。"
                if row.get("online", False)
                else "需要继续时，先打开电脑和原 Agent，让本地 Agent 重新连接飞书。"
            )
        elif not row.get("online", False) or status == "offline":
            message = "暂时联系不到本地会话。请打开电脑和原 Agent、恢复网络；仍未恢复时，在本地让 Agent 检查并重新连接飞书。"
        elif status == "connecting":
            message = (
                "正在连接本地会话，请等待连接确认；若本地已提示超时，请让 Agent 检查后重新连接。"
            )
        elif status == "unavailable":
            message = "未能连接到原会话。请在本地打开原 Agent 会话，让 Agent 检查后重新连接飞书。"
        elif status == "ready":
            message = "已连接，可以在飞书继续对话。"
        else:
            message = "暂时无法确认连接情况。请在本地让 Agent 查看连接状态后再操作。"
        if row.get("delivery_warning", False):
            message += (
                "\n此前有消息未能确认送达，请先在本地查看原会话内容，不要为了取回回复重复提交任务。"
            )
        return message

    def _view(self, row, messages=(), cancellations=()):
        return {
            "user_id": row["user_id"],
            **{
                k: row[k]
                for k in (
                    "native_session",
                    "device_name",
                    "project",
                    "session_name",
                    "generation",
                    "desired",
                    "status",
                    "online",
                )
            },
            "runtime_kind": row.get("runtime_kind", "codex"),
            "runtime_instance": row.get("runtime_instance", ""),
            "delivery_warning": row.get("delivery_warning", False),
            "guidance": self.status_message(row),
            "messages": list(messages),
            "cancellations": list(cancellations),
        }

    def _cancel_old(self, c, user):
        c.execute(
            "UPDATE session_relay.messages SET state=CASE WHEN state='pending' THEN 'cancelled' ELSE 'cancel_requested' END WHERE user_id=%s AND state IN ('pending','dispatching')",
            (user,),
        )
        c.execute(
            """UPDATE session_relay.interactions SET state='cancelled',answer=NULL
            WHERE user_id=%s AND state IN ('pending','answered') AND generation<(
                SELECT generation FROM session_relay.bindings WHERE user_id=%s
            )""",
            (user, user),
        )

    def origin(self, user, body):
        ids = set(body.message_ids)
        valid_ids = []
        for ident in ids:
            try:
                valid_ids.append(UUID(ident))
            except ValueError:
                pass
        with self.db.transaction() as c:
            row = self._row(c, user)
            known = c.execute(
                "SELECT id,generation,device_id,native_session,state FROM session_relay.messages WHERE user_id=%s AND id=ANY(%s)",
                (user["user_id"], valid_ids),
            ).fetchall()
            if not known:
                return {"scope": "local"}
            if len(known) != len(ids):
                return {"scope": "mixed"}
            active = (
                row["desired"]
                and row["online"]
                and row["status"] == "ready"
                and row["generation"] == body.generation
                and row["native_session"] == str(body.native_session)
                and all(
                    item["generation"] == row["generation"]
                    and item["device_id"] == user["session_id"]
                    and item["native_session"] == row["native_session"]
                    and (
                        item["state"] == "dispatching"
                        or (body.for_result and item["state"] in ("completed", "empty_output"))
                    )
                    for item in known
                )
            )
            return {"scope": "remote" if active else "inactive"}

    def open_interaction(self, user, body):
        digest = hashlib.sha256(body.model_dump_json(exclude_none=True).encode()).hexdigest()
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            if (
                not row["desired"]
                or not row["online"]
                or row["status"] != "ready"
                or row["native_session"] != str(body.native_session)
            ):
                raise DomainError(
                    "SESSION_CHANGED",
                    "当前连接已变化，本次请求未执行。请确认飞书显示的会话后，再发起需要的操作。",
                    "user_action",
                    409,
                )
            work = c.execute(
                "SELECT id FROM session_relay.messages WHERE user_id=%s AND generation=%s AND device_id=%s AND native_session=%s AND (state='dispatching' OR (%s AND state IN ('completed','empty_output'))) AND id=ANY(%s)",
                (
                    user["user_id"],
                    row["generation"],
                    row["device_id"],
                    row["native_session"],
                    body.async_question,
                    body.message_ids,
                ),
            ).fetchall()
            if {item["id"] for item in work} != set(body.message_ids):
                raise DomainError(
                    "SESSION_ORIGIN_INVALID",
                    "这不是当前飞书对话的请求，未发起远程交互。请在原会话处理。",
                    "user_action",
                    409,
                )
            if self._interaction_cancelled(c, user, body.generation, body.id):
                return {"id": body.id, "state": "cancelled", "answer": None}
            old = c.execute(
                "SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions WHERE id=%s",
                (body.id,),
            ).fetchone()
            if old:
                if old["user_id"] != user["user_id"] or old["request_digest"] != digest:
                    raise DomainError(
                        "SESSION_REQUEST_CONFLICT",
                        "这个请求编号已用于其他内容，本次未执行。请让本地 Agent 核对原请求，不要重复授权。",
                        "user_action",
                        409,
                    )
                return self._interaction_view(c, row, old)
            if body.kind == "attention":
                existing = self._reusable_attention(c, row, body)
                if existing:
                    return existing
            c.execute(
                """INSERT INTO session_relay.interactions(id,user_id,device_id,generation,native_session,turn_id,kind,prompt,request_digest,message_ids,reaction_enabled,choices,async_question)
                VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)""",
                (
                    body.id,
                    user["user_id"],
                    user["session_id"],
                    row["generation"],
                    str(body.native_session),
                    body.turn_id,
                    body.kind,
                    body.prompt,
                    digest,
                    body.message_ids,
                    body.kind == "permission",
                    Jsonb(body.choices) if body.choices is not None else None,
                    body.async_question,
                ),
            )
            if body.kind == "permission":
                title = "需要授权"
                instruction = "**在本卡片添加表情：✅ 仅允许本次 · ❌ 拒绝**\n5 分钟内有效。勾叉同时存在时等待明确选择；已提交的决定不因移除表情而撤销。"
            elif body.kind == "attention":
                title = "可能需要在 Codex 确认"
                instruction = ""
            else:
                title = "需要你回答"
                if body.choices:
                    instruction = "**直接回复你的答案，也可以发选项字母。** 有多个待答问题时，请回复对应卡片。5 分钟内有效。"
                else:
                    instruction = (
                        "**直接回复你的答案。** 有多个待答问题时，请回复对应卡片。5 分钟内有效。"
                    )
            content = body.prompt + ("\n\n" + instruction if instruction else "")
            self.notifications.enqueue(
                c,
                {**row, "interaction_id": body.id},
                "interaction-" + str(body.id),
                content,
                card_title=title,
            )
            return {"id": body.id, "state": "pending", "answer": None}

    def async_answer(self, user, body):
        digest = hashlib.sha256(
            (
                str(user["user_id"])
                + ":"
                + str(body.generation)
                + ":"
                + ",".join(sorted({str(i) for i in body.interaction_ids}))
            ).encode()
        ).hexdigest()
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            interactions = c.execute(
                "SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions WHERE id=ANY(%s) AND user_id=%s FOR UPDATE",
                (body.interaction_ids, user["user_id"]),
            ).fetchall()
            if (
                {i["id"] for i in interactions} != set(body.interaction_ids)
                or any(
                    not i["async_question"]
                    or i["state"] != "answered"
                    or not self._interaction_active(c, row, i)
                    for i in interactions
                )
                or len({i["turn_id"] for i in interactions}) != 1
            ):
                raise DomainError(
                    "SESSION_REQUEST_INVALID",
                    "异步问题已失效，答案未交给原会话。",
                    "user_action",
                    409,
                )
            event_id = "async-answer:" + digest
            existing = c.execute(
                "SELECT * FROM session_relay.messages WHERE id=%s OR event_id=%s",
                (body.id, event_id),
            ).fetchone()
            if existing:
                if (
                    existing["id"] != body.id
                    or existing["event_id"] != event_id
                    or existing["user_id"] != user["user_id"]
                    or existing["device_id"] != user["session_id"]
                ):
                    raise DomainError(
                        "SESSION_REQUEST_CONFLICT",
                        "答案提交身份不匹配，未重复发送。",
                        "user_action",
                        409,
                    )
                return {"dispatch": False}
            c.execute(
                """INSERT INTO session_relay.messages(id,event_id,user_id,generation,kind,content,state,device_id,native_session,runtime_kind,runtime_instance,dispatched_at)
                VALUES (%s,%s,%s,%s,'async_answer','异步问题回答','dispatching',%s,%s,%s,%s,clock_timestamp())""",
                (
                    body.id,
                    event_id,
                    user["user_id"],
                    row["generation"],
                    user["session_id"],
                    row["native_session"],
                    row["runtime_kind"],
                    row["runtime_instance"],
                ),
            )
            return {"dispatch": True}

    def _interaction_origin_active(self, c, work):
        count = c.execute(
            """SELECT count(*) AS n FROM session_relay.messages
            WHERE user_id=%s AND generation=%s AND native_session=%s AND device_id=%s AND (state='dispatching' OR (%s AND state IN ('completed','empty_output'))) AND id=ANY(%s)""",
            (
                work["user_id"],
                work["generation"],
                work["native_session"],
                work["device_id"],
                work["async_question"],
                work["message_ids"],
            ),
        ).fetchone()["n"]
        return count == len(work["message_ids"])

    def interaction(self, user, ident):
        with self.db.transaction() as c:
            binding = self._row(c, user)
            work = c.execute(
                "SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions WHERE id=%s AND user_id=%s AND device_id=%s FOR UPDATE",
                (ident, user["user_id"], user["session_id"]),
            ).fetchone()
            if not work and self._interaction_cancelled(c, user, binding["generation"], ident):
                return {"id": ident, "state": "cancelled", "answer": None}
            if not work:
                raise DomainError(
                    "SESSION_REQUEST_INVALID",
                    "找不到这个请求。请查看飞书里的最新卡片或原会话。",
                    "user_action",
                    404,
                )
            return self._interaction_view(c, binding, work)

    def _reusable_attention(self, c, row, body):
        existing = c.execute(
            """SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions
            WHERE user_id=%s AND device_id=%s AND generation=%s AND native_session=%s
            AND kind='attention' AND turn_id=%s AND prompt=%s AND state='pending'
            AND message_ids @> %s AND message_ids <@ %s ORDER BY expires_at DESC LIMIT 1""",
            (
                row["user_id"],
                row["device_id"],
                row["generation"],
                row["native_session"],
                body.turn_id,
                body.prompt,
                body.message_ids,
                body.message_ids,
            ),
        ).fetchone()
        if existing:
            view = self._interaction_view(c, row, existing)
            if view["state"] in ("pending", "delivered", "unconfirmed"):
                return view
        return None

    def _interaction_view(self, c, binding, work):
        ident = work["id"]
        if work["state"] in ("resolved", "cancelled"):
            return {"id": ident, "state": work["state"], "answer": None}
        state = work["state"]
        in_flight = False
        if work["kind"] == "attention":
            delivery = self.notifications.attention_delivery_state(c, work["user_id"], ident)
            in_flight = delivery in ("sending", "unknown", "unconfirmed", "accepted")
            state = {
                "accepted": "delivered",
                "unconfirmed": "unconfirmed",
                "failed": "cancelled",
                "cancelled": "cancelled",
            }.get(delivery, state)
        if state == "cancelled" or not self._interaction_active(
            c, binding, work, delivered=in_flight
        ):
            c.execute(
                "UPDATE session_relay.interactions SET state='cancelled',answer=NULL WHERE id=%s",
                (ident,),
            )
            return {"id": ident, "state": "cancelled", "answer": None}
        return {
            "id": ident,
            "state": state,
            "answer": work["answer"],
        }

    def _interaction_active(self, c, row, work, *, delivered=False):
        return (
            row["desired"]
            and row["online"]
            and row["status"] == "ready"
            and row["generation"] == work["generation"]
            and row["device_id"] == work["device_id"]
            and row["native_session"] == work["native_session"]
            and (work["valid"] or delivered)
            and self._interaction_origin_active(c, work)
        )

    def _interaction_cancelled(self, c, user, generation, ident):
        return (
            c.execute(
                "SELECT 1 FROM session_relay.interaction_cancellations WHERE user_id=%s AND device_id=%s AND generation=%s AND id=%s",
                (user["user_id"], user["session_id"], generation, ident),
            ).fetchone()
            is not None
        )

    def close_interaction(self, user, ident, body):
        with self.db.transaction() as c:
            binding = self._row(c, user)
            self._generation(binding, body.generation)
            work = c.execute(
                "SELECT * FROM session_relay.interactions WHERE id=%s AND user_id=%s AND device_id=%s AND generation=%s FOR UPDATE",
                (ident, user["user_id"], user["session_id"], body.generation),
            ).fetchone()
            if not work and self._interaction_cancelled(c, user, body.generation, ident):
                return {"id": ident, "state": "cancelled", "answer": None}
            if not work and body.state == "cancelled":
                c.execute(
                    "INSERT INTO session_relay.interaction_cancellations(user_id,device_id,generation,id) VALUES (%s,%s,%s,%s) ON CONFLICT DO NOTHING",
                    (user["user_id"], user["session_id"], body.generation, ident),
                )
                return {"id": ident, "state": "cancelled", "answer": None}
            if not work:
                raise DomainError(
                    "SESSION_REQUEST_INVALID",
                    "找不到当前连接中的问题，本次操作未生效。",
                    "user_action",
                    404,
                )
            state = work["state"]
            if work["kind"] == "attention" and state == "pending" and body.state == "cancelled":
                delivery = self.notifications.attention_delivery_state(
                    c, work["user_id"], ident, cancel_pending=True
                )
                if delivery in ("sending", "unknown", "unconfirmed", "accepted"):
                    return {"id": ident, "state": state, "answer": None}
            if state in ("pending", "answered"):
                state = body.state
                c.execute(
                    "UPDATE session_relay.interactions SET state=%s,answer=NULL WHERE id=%s",
                    (state, ident),
                )
            return {"id": ident, "state": state, "answer": None}

    def answer_interaction(self, c, row, text, key):
        if not text.startswith("/session answer"):
            return False
        fields = text.split(maxsplit=3)
        try:
            ident = UUID(fields[2])
            answer = fields[3].strip()
        except (ValueError, IndexError):
            self.notifications.enqueue(
                c,
                row,
                key,
                "未识别到回答。请按最新卡片上的说明回复。",
            )
            return True
        work = c.execute(
            "SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions WHERE id=%s AND user_id=%s FOR UPDATE",
            (ident, row["user_id"]),
        ).fetchone()
        self._record_interaction_answer(c, row, work, answer, key)
        return True

    def _record_interaction_answer(self, c, row, work, answer, key, *, resolve_choice=True):
        if not work or work["state"] != "pending" or not self._interaction_active(c, row, work):
            message = "这个问题已结束或已回答，本次回复未执行。若仍需处理，请查看会话的新回复，或发消息说明你接下来想做什么。"
        elif work["kind"] == "attention":
            message = "这条提醒无需回复。若想跳过，请直接发一条新消息。"
        elif work["kind"] == "permission" and answer not in ("allow", "deny"):
            message = "本次未获得有效授权。请在权限卡片上添加 ✅ 或 ❌。"
        elif not answer or len(answer) > 8000:
            message = "本次回答为空或超过 8000 字。请在问题仍有效时缩短回答，再发送一次。"
        else:
            if resolve_choice and work["choices"] is not None:
                answer = work["choices"].get(answer.upper(), answer)
            c.execute(
                "UPDATE session_relay.interactions SET state='answered',answer=%s WHERE id=%s",
                (answer, work["id"]),
            )
            return
        self.notifications.enqueue(c, row, key, message)

    def _answer_question_reply(self, c, row, text, key, parent):
        answer = text
        free_answer = text.startswith(("回答：", "回答:"))
        if free_answer:
            answer = text[3:].strip()
        if parent:
            target = self.notifications.interaction_for_receipt(c, row["user_id"], parent)
            if target:
                work = c.execute(
                    """SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions
                    WHERE id=%s AND user_id=%s AND kind='question' FOR UPDATE""",
                    (target["interaction_id"], row["user_id"]),
                ).fetchone()
                if work:
                    self._record_interaction_answer(
                        c, row, work, answer, key, resolve_choice=not free_answer
                    )
                    return True
            if c.execute(
                """SELECT 1 FROM notifications.session_deliveries d
                JOIN session_relay.interactions i ON i.id=d.interaction_id
                WHERE d.user_id=%s AND i.generation=%s AND i.kind='question' AND i.state='pending'
                AND i.expires_at>clock_timestamp() AND d.state IN ('sending','unknown','unconfirmed') LIMIT 1""",
                (row["user_id"], row["generation"]),
            ).fetchone():
                c.execute(
                    "UPDATE session_relay.messages SET state='answer_pending' WHERE id=%s", (key,)
                )
                return True
            if free_answer:
                self.notifications.enqueue(
                    c, row, key, "这条引用不是问题卡片。请回复对应的问题卡片。"
                )
                return True
            return False

        option = len(text) == 1 and text.isascii() and text.isalpha()
        active = []
        if row["desired"] and row["online"] and row["status"] == "ready":
            questions = c.execute(
                """SELECT *,true AS valid FROM session_relay.interactions
                WHERE user_id=%s AND generation=%s AND device_id=%s AND native_session=%s
                AND kind='question' AND state='pending'
                AND expires_at>clock_timestamp() FOR UPDATE""",
                (row["user_id"], row["generation"], row["device_id"], row["native_session"]),
            ).fetchall()
            active = [work for work in questions if self._interaction_origin_active(c, work)]
        if not active:
            if not option and not free_answer:
                return False
            message = "现在没有等待回答的问题。想开始新任务，可以直接发完整指令。"
        elif len(active) > 1:
            message = "有多条问题正在等回复，请长按对应卡片并选择「回复」。"
        elif not self.notifications.interaction_receipt(c, row["user_id"], active[0]["id"]):
            c.execute(
                "UPDATE session_relay.messages SET state='answer_pending',answer_target=%s WHERE id=%s",
                (active[0]["id"], key),
            )
            return True
        else:
            self._record_interaction_answer(
                c, row, active[0], answer, key, resolve_choice=not free_answer
            )
            return True
        self.notifications.enqueue(c, row, key, message)
        return True

    def attach(self, user, body):
        with self.db.transaction() as c:
            c.execute(
                "SELECT pg_advisory_xact_lock(hashtext(%s))",
                ("session-relay:" + str(user["user_id"]),),
            )
            row = c.execute(
                "SELECT *,heartbeat>clock_timestamp()-interval '45 seconds' AS online FROM session_relay.bindings WHERE user_id=%s FOR UPDATE",
                (user["user_id"],),
            ).fetchone()
            if (
                row
                and row["device_id"] == user["session_id"]
                and row["native_session"] == str(body.native_session)
                and row["runtime_kind"] == body.runtime_kind
                and row["runtime_instance"] == body.runtime_instance
                and row["device_name"] == body.device_name
                and row["desired"]
                and row["online"]
                and row["status"] in ("connecting", "ready")
            ):
                return self._view(self._row(c, user))
            previous = row["previous_binding"] if row else None
            if row and (
                row["native_session"] != str(body.native_session)
                or row["runtime_kind"] != body.runtime_kind
                or row["runtime_instance"] != body.runtime_instance
                or row["device_name"] != body.device_name
            ):
                previous = {
                    key: row[key]
                    for key in (
                        "native_session",
                        "device_name",
                        "project",
                        "session_name",
                        "runtime_kind",
                        "runtime_instance",
                    )
                }
            generation = row["generation"] + 1 if row else 1
            row = c.execute(
                """INSERT INTO session_relay.bindings(user_id,device_id,native_session,device_name,project,session_name,generation,previous_binding,runtime_kind,runtime_instance)
                VALUES (%s,%s,%s,%s,%s,%s,%s,%s,%s,%s) ON CONFLICT(user_id) DO UPDATE SET
                device_id=EXCLUDED.device_id,native_session=EXCLUDED.native_session,device_name=EXCLUDED.device_name,
                project=EXCLUDED.project,session_name=EXCLUDED.session_name,generation=EXCLUDED.generation,
                previous_binding=EXCLUDED.previous_binding,runtime_kind=EXCLUDED.runtime_kind,runtime_instance=EXCLUDED.runtime_instance,
                desired=true,status='connecting',heartbeat=clock_timestamp(),updated_at=clock_timestamp()
                RETURNING *,true AS online""",
                (
                    user["user_id"],
                    user["session_id"],
                    str(body.native_session),
                    body.device_name,
                    body.project,
                    body.session_name,
                    generation,
                    Jsonb(previous) if previous else None,
                    body.runtime_kind,
                    body.runtime_instance,
                ),
            ).fetchone()
            self._cancel_old(c, user["user_id"])
            return self._view(row)

    def status(self, user):
        with self.db.transaction() as c:
            return self._view(self._row(c, user, False))

    def _control(self, c, row, action, key):
        if action == "status":
            self.notifications.enqueue(
                c,
                row,
                key,
                self.status_message(row),
            )
            return row
        if action == "resume" and not row["online"]:
            self.notifications.enqueue(c, row, key, self.status_message(row))
            return row
        desired = action == "resume"
        if row["desired"] == desired and row["status"] != "unavailable":
            self.notifications.enqueue(c, row, key, self.status_message(row))
            return row
        row = c.execute(
            """UPDATE session_relay.bindings SET desired=%s,status=%s,generation=generation+1,updated_at=clock_timestamp()
            WHERE user_id=%s RETURNING *,heartbeat>clock_timestamp()-interval '45 seconds' AS online""",
            (desired, "connecting" if desired else "detached", row["user_id"]),
        ).fetchone()
        self._cancel_old(c, row["user_id"])
        if not desired:
            self.notifications.enqueue(
                c,
                row,
                key,
                self.status_message(row),
            )
        return row

    def detach(self, user, body):
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            return self._view(
                self._control(
                    c, row, "detach", "detach:" + str(user["user_id"]) + ":" + str(body.generation)
                )
            )

    def _generation(self, row, generation):
        if row["generation"] != generation:
            raise DomainError(
                "SESSION_CHANGED",
                "当前连接已变化，本次更新未生效。请先查看 session status，确认现在连接的会话；原任务的结果请到原会话核对。",
                "user_action",
                409,
            )

    def poll(self, user):
        with self.db.transaction() as c:
            row = self._row(c, user, False)
            if row["device_id"] != user["session_id"]:
                cancellations = c.execute(
                    "SELECT id,native_session,runtime_kind,runtime_instance FROM session_relay.messages WHERE user_id=%s AND device_id=%s AND state='cancel_requested' ORDER BY created_at LIMIT 10",
                    (user["user_id"], user["session_id"]),
                ).fetchall()
                view = self._view(
                    row,
                    (),
                    [
                        item
                        for item in cancellations
                        if user.get("relay_runtime_aware", True) or item["runtime_kind"] == "codex"
                    ],
                )
                view.update(desired=False, status="replaced", online=False)
                view["guidance"] = self.status_message(view)
                return view
            c.execute(
                "UPDATE session_relay.bindings SET heartbeat=clock_timestamp() WHERE user_id=%s",
                (user["user_id"],),
            )
            row["online"] = True
            if row["desired"] and row["status"] == "offline":
                row = c.execute(
                    "UPDATE session_relay.bindings SET status='connecting' WHERE user_id=%s RETURNING *,true AS online",
                    (user["user_id"],),
                ).fetchone()
            messages = []
            if row["desired"]:
                messages = c.execute(
                    """SELECT id,generation,kind,content,state,created_at,dispatched_at,parent_id,chat_id,context_ready FROM session_relay.messages
                    WHERE user_id=%s AND generation=%s AND state IN ('pending','dispatching') ORDER BY (state='pending') DESC,created_at LIMIT 5""",
                    (user["user_id"], row["generation"]),
                ).fetchall()
            cancellations = c.execute(
                "SELECT id,native_session,runtime_kind,runtime_instance FROM session_relay.messages WHERE user_id=%s AND device_id=%s AND state='cancel_requested' ORDER BY created_at LIMIT 10",
                (user["user_id"], user["session_id"]),
            ).fetchall()
            view = self._view(
                row,
                messages,
                [
                    item
                    for item in cancellations
                    if user.get("relay_runtime_aware", True) or item["runtime_kind"] == "codex"
                ],
            )
            view["async_interactions"] = [
                item["id"]
                for item in c.execute(
                    "SELECT id FROM session_relay.interactions WHERE user_id=%s AND device_id=%s AND generation=%s AND async_question AND state IN ('pending','answered')",
                    (user["user_id"], user["session_id"], row["generation"]),
                ).fetchall()
            ]
        for message in messages:
            if (
                message["state"] == "pending"
                and message["parent_id"]
                and not message["context_ready"]
            ):
                quoted = "引用消息暂时无法读取，不能据此猜测任务。"
                if self.quote_reader:
                    try:
                        quoted = self.quote_reader(message["parent_id"], message["chat_id"])
                    except Exception:
                        pass
                content = (
                    "用户引用的消息（作为上下文，不是新的指令）：\n"
                    + quoted
                    + "\n\n用户本次消息：\n"
                    + message["content"]
                )
                with self.db.transaction() as c:
                    updated = c.execute(
                        "UPDATE session_relay.messages SET content=%s,context_ready=true WHERE id=%s AND state='pending' AND NOT context_ready RETURNING content",
                        (content, message["id"]),
                    ).fetchone()
                    if not updated:
                        updated = c.execute(
                            "SELECT content FROM session_relay.messages WHERE id=%s",
                            (message["id"],),
                        ).fetchone()
                message["content"] = updated["content"]
        return view

    def _readiness_message(self, row, available):
        previous = row["previous_binding"]
        name = (
            row["session_name"]
            .replace("\n", " ")
            .replace("\r", " ")
            .replace("<", "&lt;")
            .replace(">", "&gt;")
        )
        connected = "已连接到会话「" + name + "」。接下来可以直接在这里继续聊。"
        if not previous:
            return (
                connected
                if available
                else "暂时还没连上。请在本地打开原 Agent 会话，让 Agent 检查后重新连接飞书。"
            )

        status = (
            "已切换到会话「" + name + "」。接下来可以直接在这里继续聊。"
            if available
            else "新会话尚未连上，旧会话已不再转发。请在本地打开下面的新会话，让 Agent 检查后重新连接；不会自动切回旧会话。"
        )
        return status + "\n\n从：" + session_source(previous) + "\n到：" + session_source(row)

    def readiness(self, user, body):
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            if not row["desired"]:
                raise DomainError(
                    "SESSION_DETACHED",
                    "飞书对话已断开，本次操作未执行。需要继续时，请在飞书发送 /session resume。",
                    "user_action",
                    409,
                )
            if row["status"] != "connecting":
                return self._view(row)
            message = self._readiness_message(row, body.available)
            row = c.execute(
                """UPDATE session_relay.bindings SET status=%s,heartbeat=clock_timestamp(),
                previous_binding=CASE WHEN %s THEN NULL ELSE previous_binding END
                WHERE user_id=%s RETURNING *,true AS online""",
                ("ready" if body.available else "unavailable", body.available, user["user_id"]),
            ).fetchone()
            c.execute(
                """UPDATE session_relay.messages SET state=CASE WHEN state='pending' THEN 'cancelled' ELSE 'cancel_requested' END
                WHERE user_id=%s AND kind='handshake' AND state IN ('pending','dispatching')""",
                (user["user_id"],),
            )
            self.notifications.enqueue(
                c,
                row,
                "readiness-" + str(uuid4()),
                message,
            )
            return self._view(row)

    def dispatch(self, user, message, generation):
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, generation)
            if not row["desired"]:
                raise DomainError(
                    "SESSION_DETACHED",
                    "飞书对话已断开，本次操作未执行。需要继续时，请在飞书发送 /session resume。",
                    "user_action",
                    409,
                )
            result = c.execute(
                """UPDATE session_relay.messages SET state='dispatching',dispatched_at=clock_timestamp() WHERE id=%s AND user_id=%s AND generation=%s AND state='pending'
                RETURNING id""",
                (message, user["user_id"], generation),
            ).fetchone()
            return {"dispatch": bool(result)}

    def working(self, user, message, generation):
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, generation)
            work = c.execute(
                "SELECT * FROM session_relay.messages WHERE id=%s AND user_id=%s AND generation=%s AND state='dispatching'",
                (message, user["user_id"], generation),
            ).fetchone()
            if not work or not row["desired"] or not row["online"] or row["status"] != "ready":
                raise DomainError(
                    "SESSION_MESSAGE_INACTIVE",
                    "当前连接已不再接收这条消息的更新，本次更新未生效。请到原会话核对结果，不要直接重发任务。",
                    "user_action",
                    409,
                )
            self.notifications.working(c, work)
            return {"accepted": True}

    def result(self, user, message, body):
        digest = hashlib.sha256(
            body.model_dump_json(
                exclude=set() if body.summary_pending else {"summary_pending"}
            ).encode()
        ).hexdigest()
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            work = c.execute(
                "SELECT * FROM session_relay.messages WHERE id=%s AND user_id=%s FOR UPDATE",
                (message, user["user_id"]),
            ).fetchone()
            if not work or work["generation"] != row["generation"]:
                raise DomainError(
                    "SESSION_MESSAGE_INVALID",
                    "这条消息不属于当前连接，本次更新未生效。请先确认正在操作的会话，并到原会话核对任务结果。",
                    "user_action",
                    404,
                )
            if work["result_digest"]:
                if work["result_digest"] != digest:
                    raise DomainError(
                        "SESSION_RESULT_CONFLICT",
                        "同一条消息的结果发生冲突，未覆盖已保存的结果。请在本地核对原会话，不要重新执行任务。",
                        "user_action",
                        409,
                    )
                return {"accepted": True}
            if work["state"] != "dispatching" or not row["desired"]:
                raise DomainError(
                    "SESSION_MESSAGE_INACTIVE",
                    "当前连接已不再接收这条消息的更新，本次更新未生效。请到原会话核对结果，不要直接重发任务。",
                    "user_action",
                    409,
                )
            result_ids = [message]
            if body.message_ids and body.outcome != "mixed":
                try:
                    result_ids = sorted({UUID(value) for value in body.message_ids})
                except ValueError:
                    result_ids = []
                sources = c.execute(
                    "SELECT * FROM session_relay.messages WHERE id=ANY(%s) FOR UPDATE",
                    (result_ids,),
                ).fetchall()
                if (
                    message not in result_ids
                    or len(sources) != len(result_ids)
                    or any(
                        source["user_id"] != user["user_id"]
                        or source["device_id"] != user["session_id"]
                        or source["generation"] != row["generation"]
                        or source["native_session"] != row["native_session"]
                        or source["state"] not in ("dispatching", "completed", "empty_output")
                        for source in sources
                    )
                ):
                    raise DomainError(
                        "SESSION_ORIGIN_INVALID",
                        "无法确认整轮回答均来自当前飞书会话，未转发。",
                        "user_action",
                        409,
                    )
            c.execute(
                "UPDATE session_relay.messages SET state=%s,result_digest=%s WHERE id=ANY(%s) AND state='dispatching'",
                (body.outcome, digest, result_ids),
            )
            if body.outcome == "completed":
                if not body.text:
                    raise DomainError(
                        "SESSION_EMPTY_OUTPUT",
                        "没有可发送的回答正文。请让本地 Agent 核对原会话结果，不要重新执行任务。",
                        "user_action",
                        422,
                    )
                self.notifications.enqueue(c, row, str(message) + ":0-body", body.text)
                if body.summary_pending:
                    c.execute(
                        """INSERT INTO session_relay.summary_jobs
                        (message_id,user_id,device_id,generation,text) VALUES (%s,%s,%s,%s,%s)
                        ON CONFLICT(message_id) DO NOTHING""",
                        (
                            message,
                            user["user_id"],
                            user["session_id"],
                            row["generation"],
                            body.text,
                        ),
                    )
                if body.summary:
                    c.execute(
                        "UPDATE session_relay.messages SET summary_digest=%s WHERE id=%s",
                        (hashlib.sha256(body.summary.encode()).hexdigest(), message),
                    )
                    self.notifications.enqueue(
                        c, row, str(message) + ":1-summary", "TL;DR\n" + body.summary
                    )
            elif body.outcome != "interrupted":
                text = {
                    "mixed": "本地输入介入了本轮，回复保留在本地，没有转发。请先在本地查看结果；之后可以从飞书继续提问，不要重复提交已经执行的任务。",
                    "failed": "原会话本轮执行失败。请在本地查看错误和已完成的步骤，再决定是否重试。",
                    "interrupt_unavailable": "这条消息未交给 Agent，上一轮可能已经中断。请到电脑端核对原会话。",
                    "not_submitted": "这条消息未交给 Agent。请在电脑端检查连接，恢复后可以重发这条消息。",
                    "unknown": "无法确认消息执行结果，未自动重发。请先在本地查看原会话，确认是否已执行；核对前不要重复发送这项任务。",
                    "empty_output": "原会话本轮已完成，但没有可转发的正文。可以继续发送消息；如需查看工具结果，请回到本地会话。",
                    "output_too_large": "回答已完成，但正文超过转发上限，请回本地查看完整回答。后续对话可继续。",
                }[body.outcome]
                self.notifications.enqueue(c, row, str(message), text)
            return {"accepted": True}

    def expire_summaries(self):
        with self.db.transaction() as c:
            cancelled = c.execute(
                """UPDATE session_relay.summary_jobs j SET state='cancelled',text=''
                WHERE j.state IN ('pending','claimed') AND NOT EXISTS (
                    SELECT 1 FROM session_relay.bindings b JOIN identity.sessions s ON s.id=b.device_id
                    WHERE b.user_id=j.user_id AND b.device_id=j.device_id
                    AND b.generation=j.generation AND b.desired
                    AND NOT s.revoked AND s.expires_at>clock_timestamp()) RETURNING j.message_id"""
            ).fetchall()
            jobs = c.execute(
                """SELECT * FROM session_relay.summary_jobs WHERE state='claimed'
                AND lease_until<=clock_timestamp() FOR UPDATE SKIP LOCKED LIMIT 100"""
            ).fetchall()
            for job in jobs:
                row = c.execute(
                    "SELECT * FROM session_relay.bindings WHERE user_id=%s",
                    (job["user_id"],),
                ).fetchone()
                if (
                    not row
                    or not row["desired"]
                    or row["generation"] != job["generation"]
                    or row["device_id"] != job["device_id"]
                ):
                    c.execute(
                        "UPDATE session_relay.summary_jobs SET state='cancelled',text='' WHERE message_id=%s",
                        (job["message_id"],),
                    )
                    continue
                c.execute(
                    "UPDATE session_relay.summary_jobs SET state='expired',text='' WHERE message_id=%s",
                    (job["message_id"],),
                )
                self.notifications.enqueue(
                    c,
                    row,
                    str(job["message_id"]) + ":1-summary",
                    "TL;DR 暂不可用，完整回答已保留；请查看上方正文。",
                )
            return bool(cancelled or jobs)

    def claim_summary(self, user, body):
        self.expire_summaries()
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            if not row["desired"]:
                raise DomainError(
                    "SESSION_DETACHED", "连接已断开，未领取摘要。", "user_action", 409
                )
            previous = c.execute(
                "SELECT * FROM session_relay.summary_claims WHERE claim_id=%s",
                (body.claim_id,),
            ).fetchone()
            if previous:
                if (
                    previous["user_id"] != user["user_id"]
                    or previous["device_id"] != user["session_id"]
                    or previous["generation"] != body.generation
                ):
                    raise DomainError(
                        "SESSION_SUMMARY_CLAIM_CONFLICT",
                        "摘要领取编号不属于当前连接。",
                        "user_action",
                        409,
                    )
                job = (
                    c.execute(
                        "SELECT * FROM session_relay.summary_jobs WHERE message_id=%s AND state='claimed' AND lease_until>clock_timestamp()",
                        (previous["message_id"],),
                    ).fetchone()
                    if previous["message_id"]
                    else None
                )
            else:
                job = c.execute(
                    """SELECT * FROM session_relay.summary_jobs WHERE user_id=%s AND device_id=%s
                    AND generation=%s AND state='pending' ORDER BY created_at,message_id
                    FOR UPDATE SKIP LOCKED LIMIT 1""",
                    (user["user_id"], user["session_id"], body.generation),
                ).fetchone()
                c.execute(
                    "INSERT INTO session_relay.summary_claims(claim_id,user_id,device_id,generation,message_id) VALUES (%s,%s,%s,%s,%s)",
                    (
                        body.claim_id,
                        user["user_id"],
                        user["session_id"],
                        body.generation,
                        job["message_id"] if job else None,
                    ),
                )
                if job:
                    c.execute(
                        "UPDATE session_relay.summary_jobs SET state='claimed',claim_id=%s,lease_until=clock_timestamp()+interval '2 minutes' WHERE message_id=%s",
                        (body.claim_id, job["message_id"]),
                    )
            if not job:
                return {"job": None}
            return {
                "job": {
                    "message_id": job["message_id"],
                    "generation": job["generation"],
                    "text": job["text"],
                }
            }

    def summary(self, user, message, body):
        self.expire_summaries()
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, body.generation)
            work = c.execute(
                "SELECT * FROM session_relay.messages WHERE id=%s AND user_id=%s FOR UPDATE",
                (message, user["user_id"]),
            ).fetchone()
            if (
                not work
                or work["generation"] != row["generation"]
                or work["device_id"] != user["session_id"]
                or work["state"] != "completed"
                or not row["desired"]
            ):
                raise DomainError(
                    "SESSION_MESSAGE_INACTIVE",
                    "当前连接不再接收这条消息的摘要，请在原会话查看正文。",
                    "user_action",
                    409,
                )
            job = c.execute(
                "SELECT *,lease_until>clock_timestamp() AS lease_valid FROM session_relay.summary_jobs WHERE message_id=%s FOR UPDATE",
                (message,),
            ).fetchone()
            if job and (
                body.claim_id != job["claim_id"]
                or job["state"] not in ("claimed", "completed")
                or (job["state"] == "claimed" and not job["lease_valid"])
            ):
                raise DomainError(
                    "SESSION_SUMMARY_CLAIM_INACTIVE",
                    "摘要领取已失效，未重新生成或转发。",
                    "user_action",
                    409,
                )
            if body.claim_id and not job:
                raise DomainError(
                    "SESSION_SUMMARY_CLAIM_INACTIVE", "摘要领取不存在，未转发。", "user_action", 409
                )
            digest = hashlib.sha256(body.summary.encode()).hexdigest()
            if work["summary_digest"] and work["summary_digest"] != digest:
                raise DomainError(
                    "SESSION_SUMMARY_CONFLICT",
                    "已保存的摘要与本次不同，未覆盖原摘要。请在原会话核对。",
                    "user_action",
                    409,
                )
            c.execute(
                "UPDATE session_relay.messages SET summary_digest=%s WHERE id=%s",
                (digest, message),
            )
            if job:
                c.execute(
                    "UPDATE session_relay.summary_jobs SET state='completed',text='' WHERE message_id=%s",
                    (message,),
                )
            self.notifications.enqueue(
                c, row, str(message) + ":1-summary", "TL;DR\n" + body.summary
            )
            return {"accepted": True}

    def receive_reaction(self, data, transport):
        header, event = data.get("header", {}), data.get("event", {})
        if (
            header.get("app_id") != transport.app_id
            or header.get("event_type")
            not in ("im.message.reaction.created_v1", "im.message.reaction.deleted_v1")
            or not header.get("event_id")
            or event.get("operator_type") != "user"
            or event.get("reaction_type", {}).get("emoji_type") not in ("CheckMark", "CrossMark")
        ):
            return False
        message = event.get("message_id")
        subject = event.get("user_id", {}).get("open_id")
        tenant = header.get("tenant_key")
        if (
            not isinstance(message, str)
            or not message.startswith("om_")
            or not subject
            or not tenant
        ):
            return False
        return self._permission_reaction(transport, message, subject, tenant)

    def permission_reactions_once(self, transport):
        return self._permission_reaction(transport)

    def _permission_reaction(self, transport, message=None, subject=None, tenant=None):
        from remote_agent_session.feishu import FeishuError

        with self.db.transaction() as c:
            candidate = c.execute(
                """SELECT i.id,i.user_id,i.device_id,i.generation,u.subject,u.tenant,d.receipt
                FROM session_relay.interactions i
                JOIN notifications.session_deliveries d ON d.interaction_id=i.id AND d.state='accepted'
                JOIN identity.users u ON u.id=i.user_id
                JOIN session_relay.bindings b ON b.user_id=i.user_id
                JOIN identity.sessions s ON s.id=i.device_id
                WHERE i.kind='permission' AND i.reaction_enabled AND i.state='pending'
                AND i.expires_at>clock_timestamp() AND d.receipt IS NOT NULL
                AND NOT EXISTS (
                    SELECT 1 FROM notifications.session_deliveries other
                    WHERE other.interaction_id=i.id AND other.id<>d.id
                )
                AND u.app=%s AND b.generation=i.generation AND b.device_id=i.device_id
                AND b.native_session=i.native_session AND b.desired AND b.status='ready'
                AND b.heartbeat>clock_timestamp()-interval '45 seconds'
                AND NOT s.revoked AND s.expires_at>clock_timestamp()
                AND NOT EXISTS (SELECT 1 FROM session_relay.messages m WHERE m.id=ANY(i.message_ids) AND m.state<>'dispatching')
                AND (%s::text IS NULL OR d.receipt=%s)
                AND (%s::text IS NULL OR u.subject=%s)
                AND (%s::text IS NULL OR u.tenant=%s)
                AND (%s::text IS NOT NULL OR i.reaction_next_check<=clock_timestamp())
                ORDER BY i.reaction_next_check,i.id LIMIT 1 FOR UPDATE OF i SKIP LOCKED""",
                (transport.app_id, message, message, subject, subject, tenant, tenant, message),
            ).fetchone()
            if not candidate:
                return False
            c.execute(
                "UPDATE session_relay.interactions SET reaction_next_check=clock_timestamp()+interval '5 seconds' WHERE id=%s",
                (candidate["id"],),
            )
        try:
            choices = transport.permission_reactions(candidate["receipt"], candidate["subject"])
        except (FeishuError, ConnectionError):
            return False
        if choices not in ({"CheckMark"}, {"CrossMark"}):
            return False
        answer = "allow" if choices == {"CheckMark"} else "deny"
        with self.db.transaction() as c:
            user = {"user_id": candidate["user_id"], "session_id": candidate["device_id"]}
            try:
                row = self._row(c, user)
            except DomainError:
                return False
            active = c.execute(
                "SELECT 1 FROM identity.sessions WHERE id=%s AND NOT revoked AND expires_at>clock_timestamp()",
                (candidate["device_id"],),
            ).fetchone()
            work = c.execute(
                "SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions WHERE id=%s FOR UPDATE",
                (candidate["id"],),
            ).fetchone()
            if (
                not active
                or not row["desired"]
                or not row["online"]
                or row["status"] != "ready"
                or row["generation"] != candidate["generation"]
                or not work
                or not work["valid"]
                or work["state"] != "pending"
                or not work["reaction_enabled"]
                or work["kind"] != "permission"
                or not self._interaction_origin_active(c, work)
            ):
                return False
            c.execute(
                "UPDATE session_relay.interactions SET state='answered',answer=%s WHERE id=%s",
                (answer, work["id"]),
            )
            return True

    def receive(self, data, app_id):
        h = data.get("header", {})
        e = data.get("event", {})
        s = e.get("sender", {})
        m = e.get("message", {})
        if (
            h.get("app_id") != app_id
            or h.get("event_type") != "im.message.receive_v1"
            or s.get("sender_type") != "user"
            or m.get("chat_type") != "p2p"
        ):
            return
        tenant = h.get("tenant_key") or s.get("tenant_key")
        subject = s.get("sender_id", {}).get("open_id")
        if not tenant or (s.get("tenant_key") and s["tenant_key"] != tenant) or not subject:
            return
        unsupported = False
        try:
            text = chat_text(m)
        except (ValueError, TypeError):
            text = "[不支持的消息内容]"
            unsupported = True
        new_task = text.startswith("/session new ") and bool(text[len("/session new ") :].strip())
        if new_task:
            text = text[len("/session new ") :].strip()
        event_id = h.get("event_id")
        if not event_id:
            return
        with self.db.transaction() as c:
            user = c.execute(
                """SELECT u.id AS user_id,b.device_id AS session_id,NOT s.revoked AND s.expires_at>clock_timestamp() AS device_active FROM identity.users u
                JOIN session_relay.bindings b ON b.user_id=u.id JOIN identity.sessions s ON s.id=b.device_id
                WHERE u.app=%s AND u.tenant=%s AND u.subject=%s""",
                (app_id, tenant, subject),
            ).fetchone()
            if not user:
                return
            c.execute("SELECT pg_advisory_xact_lock(hashtext(%s))", ("relay-event:" + event_id,))
            if c.execute(
                "SELECT 1 FROM session_relay.messages WHERE user_id=%s AND event_id=%s",
                (user["user_id"], event_id),
            ).fetchone():
                return
            if self.notifications.login_notice_exists(c, user["user_id"], event_id):
                return
            if not user["device_active"]:
                self.notifications.login_required(c, user["user_id"], event_id)
                return
            row = self._row(c, user)
            inserted = c.execute(
                """INSERT INTO session_relay.messages(id,event_id,user_id,generation,kind,content,state,device_id,native_session,runtime_kind,runtime_instance,parent_id,chat_id,platform_message_id)
                VALUES (%s,%s,%s,%s,'chat',%s,'received',%s,%s,%s,%s,%s,%s,%s) ON CONFLICT(event_id) DO NOTHING RETURNING id""",
                (
                    uuid4(),
                    event_id,
                    row["user_id"],
                    row["generation"],
                    text,
                    row["device_id"],
                    row["native_session"],
                    row["runtime_kind"],
                    row["runtime_instance"],
                    m.get("parent_id"),
                    m.get("chat_id"),
                    m.get("message_id")
                    if isinstance(m.get("message_id"), str)
                    and m["message_id"].startswith("om_")
                    and len(m["message_id"]) <= 200
                    else None,
                ),
            ).fetchone()
            if not inserted:
                return
            key = str(inserted["id"])
            if self.require_event_time and not message_is_fresh(data):
                self.notifications.enqueue(
                    c,
                    row,
                    key,
                    "这条消息已过期或无法确认发送时间，未交给 Agent 执行。请确认连接后重新发送。",
                )
                return
            if unsupported:
                self.notifications.enqueue(
                    c,
                    row,
                    key,
                    "这条消息包含暂不支持的内容、格式异常或超过长度上限，未交给 agent 执行。请改用纯文本或文字富文本重新发送；图片、附件及删除线内容暂不支持。",
                )
                return
            if not new_task and self.answer_interaction(c, row, text, key):
                return
            action = {
                "/session status": "status",
                "/session detach": "detach",
                "/session resume": "resume",
            }.get(text)
            if action and not new_task:
                self._control(c, row, action, key)
                return
            if text.startswith("/session") and not new_task:
                self.notifications.enqueue(
                    c,
                    row,
                    key,
                    "未识别到这个命令，本条未交给 Agent。查看连接用 /session status，断开用 /session detach，恢复上次会话用 /session resume。",
                )
            elif not new_task and self._answer_question_reply(
                c, row, text, key, m.get("parent_id")
            ):
                return
            elif not row["desired"]:
                self.notifications.enqueue(
                    c, row, key, "本条消息未交给 Agent。" + self.status_message(row)
                )
            elif not row["online"] or row["status"] != "ready":
                self.notifications.enqueue(
                    c,
                    row,
                    key,
                    "本条消息未交给 Agent。"
                    + self.status_message(row)
                    + "连接恢复后，再发送这条消息。",
                )
            elif (
                c.execute(
                    "SELECT count(*) AS n FROM session_relay.messages WHERE user_id=%s AND generation=%s AND state IN ('pending','dispatching')",
                    (row["user_id"], row["generation"]),
                ).fetchone()["n"]
                >= 5
            ):
                self.notifications.enqueue(
                    c,
                    row,
                    key,
                    "已有多条消息等待处理，本条未受理。请等已有消息得到回复后，再发送这一条。",
                )
            else:
                c.execute(
                    "UPDATE session_relay.messages SET state='pending' WHERE id=%s",
                    (inserted["id"],),
                )

    def _resolve_pending_answers(self, c):
        messages = c.execute(
            """SELECT *,created_at<clock_timestamp()-interval '60 seconds' AS expired
            FROM session_relay.messages WHERE state='answer_pending' ORDER BY created_at LIMIT 100"""
        ).fetchall()
        for message in messages:
            row = self._row(c, {"user_id": message["user_id"]}, device=False)
            current = c.execute(
                "SELECT state FROM session_relay.messages WHERE id=%s FOR UPDATE", (message["id"],)
            ).fetchone()
            if current["state"] != "answer_pending":
                continue
            active = (
                row["generation"] == message["generation"]
                and row["device_id"] == message["device_id"]
                and row["desired"]
            )
            target = self.notifications.interaction_for_receipt(
                c, message["user_id"], message["parent_id"]
            )
            if active and message["answer_target"]:
                target = self.notifications.interaction_receipt(
                    c, message["user_id"], message["answer_target"]
                )
                if target:
                    work = c.execute(
                        "SELECT *,expires_at>clock_timestamp() AS valid FROM session_relay.interactions WHERE id=%s AND user_id=%s FOR UPDATE",
                        (message["answer_target"], message["user_id"]),
                    ).fetchone()
                    self._record_interaction_answer(
                        c,
                        row,
                        work,
                        message["content"][3:].strip()
                        if message["content"].startswith(("回答：", "回答:"))
                        else message["content"],
                        str(message["id"]),
                        resolve_choice=not message["content"].startswith(("回答：", "回答:")),
                    )
                    c.execute(
                        "UPDATE session_relay.messages SET state='received' WHERE id=%s",
                        (message["id"],),
                    )
                    continue
            if active and target and not message["answer_target"]:
                self._answer_question_reply(
                    c, row, message["content"], str(message["id"]), message["parent_id"]
                )
            elif not message["expired"] and active:
                continue
            elif active:
                self.notifications.enqueue(
                    c,
                    row,
                    str(message["id"]),
                    "未能确认这条回复对应的问题，本条未交给 Agent 执行。请查看最新问题卡片后重新回答。",
                )
            c.execute(
                "UPDATE session_relay.messages SET state='received' WHERE id=%s", (message["id"],)
            )

    def expire(self):
        summaries_expired = self.expire_summaries()
        with self.db.transaction() as c:
            self._resolve_pending_answers(c)
            rows = c.execute("""UPDATE session_relay.bindings SET status='offline' WHERE desired AND status IN ('ready','connecting')
                AND heartbeat<clock_timestamp()-interval '45 seconds' RETURNING *""").fetchall()
            for row in rows:
                self.notifications.enqueue(
                    c,
                    row,
                    "offline-" + str(uuid4()),
                    self.status_message(row),
                )
            return bool(rows) or summaries_expired

    def _message_cancellation(self, c, user, message):
        row = c.execute(
            "SELECT state,result_digest,runtime_kind FROM session_relay.messages WHERE id=%s AND user_id=%s FOR UPDATE",
            (message, user["user_id"]),
        ).fetchone()
        if not row:
            raise DomainError(
                "SESSION_MESSAGE_INVALID",
                "无法操作这条消息。请用原来的登录身份查看对应会话。",
                "user_action",
                404,
            )
        self._require_runtime_client(user, row)
        return {
            "requested": row["state"] == "cancel_requested",
            "settled": row["state"] == "cancelled" or row["result_digest"] is not None,
        }

    def cancellation(self, user, message):
        with self.db.transaction() as c:
            return self._message_cancellation(c, user, message)

    def cancelled(self, user, message):
        with self.db.transaction() as c:
            status = self._message_cancellation(c, user, message)
            if status["requested"]:
                c.execute(
                    "UPDATE session_relay.messages SET state='cancelled' WHERE id=%s", (message,)
                )
            elif not status["settled"]:
                raise DomainError(
                    "SESSION_CANCELLATION_NOT_REQUESTED",
                    "这条消息没有待处理的撤回请求。请先查看原会话结果，不要重复提交或撤回。",
                    "user_action",
                    409,
                )
        return {"accepted": True}

    def waiting(self, user, message, generation):
        with self.db.transaction() as c:
            row = self._row(c, user)
            self._generation(row, generation)
        return {"accepted": True}

    def revoke(self, user):
        with self.db.transaction() as c:
            row = c.execute(
                "UPDATE session_relay.bindings SET desired=false,status='revoked',generation=generation+1 WHERE user_id=%s AND device_id=%s RETURNING user_id",
                (user["user_id"], user["session_id"]),
            ).fetchone()
            if row:
                self._cancel_old(c, row["user_id"])
