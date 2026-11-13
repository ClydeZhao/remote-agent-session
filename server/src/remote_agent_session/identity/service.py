import hashlib
import json
import secrets
from uuid import uuid4

from cryptography.fernet import Fernet
from psycopg.types.json import Jsonb

from remote_agent_session.errors import DomainError

from .models import VerifiedPairingEvent


def digest(value: str) -> str:
    return hashlib.sha256(value.encode()).hexdigest()


class Identity:
    def __init__(self, db, key, app="offline-test-app", simulated=True):
        self.db, self.cipher = db, Fernet(key)
        self.app, self.simulated = app, simulated

    def start(self, label):
        code, claim, device = secrets.token_hex(6).upper(), secrets.token_urlsafe(32), uuid4()
        with self.db.transaction() as c:
            row = c.execute(
                """INSERT INTO identity.devices(id,label,code_hash,claim_hash,expires_at)
                VALUES (%s,%s,%s,%s,clock_timestamp()+interval '10 minutes') RETURNING expires_at""",
                (device, label, digest(code), digest(claim)),
            ).fetchone()
        return dict(
            device_id=device,
            user_code=code,
            claim_secret=claim,
            **row,
            verification_uri="urn:remote_agent_session:offline-pairing"
            if self.simulated
            else f"https://applink.feishu.cn/client/bot/open?appId={self.app}",
            simulated=self.simulated,
        )

    def rate_limit(self, scope, limit=10):
        with self.db.transaction() as c:
            row = c.execute(
                """INSERT INTO identity.rate_limits VALUES (%s,clock_timestamp(),1)
                ON CONFLICT(scope) DO UPDATE SET
                attempts=CASE WHEN identity.rate_limits.window_started_at<clock_timestamp()-interval '1 minute'
                    THEN 1 ELSE identity.rate_limits.attempts+1 END,
                window_started_at=CASE WHEN identity.rate_limits.window_started_at<clock_timestamp()-interval '1 minute'
                    THEN clock_timestamp() ELSE identity.rate_limits.window_started_at END RETURNING attempts""",
                (digest(scope),),
            ).fetchone()
        if row["attempts"] > limit:
            error = DomainError("RATE_LIMITED", "Too many attempts.", "same_request", 429)
            error.body.retry_after = 60
            raise error

    def pairing_event(self, event: VerifiedPairingEvent):
        self.rate_limit("pair:" + event.user_code)

        if (
            not event.private_chat
            or event.app != self.app
            or (self.simulated and event.tenant != "offline-test-tenant")
        ):
            raise DomainError("PAIRING_SOURCE_DENIED", "Untrusted pairing source.", status=403)
        with self.db.transaction() as c:
            if not c.execute(
                "INSERT INTO identity.events VALUES (%s) ON CONFLICT DO NOTHING RETURNING id",
                (event.event_id,),
            ).fetchone():
                return None
            row = c.execute(
                """SELECT * FROM identity.devices WHERE code_hash=%s
                AND expires_at>clock_timestamp() FOR UPDATE""",
                (digest(event.user_code),),
            ).fetchone()
            if not row or row["state"] == "authorized":
                raise DomainError(
                    "PAIRING_INVALID", "Pairing code expired or consumed.", "user_action"
                )
            candidate = dict(app=event.app, tenant=event.tenant, subject=event.subject)
            if row["state"] == "pending":
                if event.confirmation:
                    raise DomainError("CONFIRMATION_REQUIRED", "Send the code before confirming.")
                challenge = secrets.token_urlsafe(32)
                c.execute(
                    """UPDATE identity.devices SET state='awaiting_confirmation',candidate=%s,
                    confirmation_hash=%s WHERE id=%s""",
                    (Jsonb(candidate), digest(challenge), row["id"]),
                )
                return challenge
            if row["candidate"] == candidate and not event.confirmation:
                challenge = secrets.token_urlsafe(32)
                c.execute(
                    "UPDATE identity.devices SET confirmation_hash=%s WHERE id=%s",
                    (digest(challenge), row["id"]),
                )
                return challenge
            if (
                row["candidate"] != candidate
                or not event.confirmation
                or not secrets.compare_digest(row["confirmation_hash"], digest(event.confirmation))
            ):
                raise DomainError(
                    "PAIRING_INVALID", "Confirmation does not match.", "user_action", 403
                )
            user = c.execute(
                """INSERT INTO identity.users(id,app,tenant,subject) VALUES (%s,%s,%s,%s)
                ON CONFLICT(app,tenant,subject) DO UPDATE SET app=EXCLUDED.app RETURNING id""",
                (uuid4(), event.app, event.tenant, event.subject),
            ).fetchone()["id"]
            session, token = uuid4(), secrets.token_urlsafe(48)
            c.execute(
                """INSERT INTO identity.sessions(id,user_id,token_hash,expires_at)
                VALUES (%s,%s,%s,clock_timestamp()+interval '30 days')""",
                (session, user, digest(token)),
            )
            envelope = json.dumps(dict(device=str(row["id"]), token=token)).encode()
            c.execute(
                """UPDATE identity.devices SET state='authorized',user_id=%s,session_id=%s,
                token_cipher=%s WHERE id=%s""",
                (user, session, self.cipher.encrypt(envelope).decode(), row["id"]),
            )
        return None

    def claim(self, device, secret):
        self.rate_limit("claim:" + str(device), 30)
        with self.db.transaction() as c:
            row = c.execute(
                """SELECT d.*,s.expires_at AS token_expires_at,s.revoked,
                d.expires_at <= clock_timestamp() AS expired FROM identity.devices d
                LEFT JOIN identity.sessions s ON s.id=d.session_id WHERE d.id=%s""",
                (device,),
            ).fetchone()
        if not row or not secrets.compare_digest(row["claim_hash"], digest(secret)):
            raise DomainError("AUTH_INVALID", "Invalid device claim.", "user_action", 401)
        if row["expired"] or row["revoked"]:
            return dict(state="expired")
        if row["state"] != "authorized":
            return dict(state=row["state"])
        value = json.loads(self.cipher.decrypt(row["token_cipher"].encode()))
        if value["device"] != str(device):
            raise DomainError("AUTH_INVALID", "Invalid device binding.", "user_action", 401)
        return dict(
            state="authorized", access_token=value["token"], expires_at=row["token_expires_at"]
        )

    def authenticate(self, token):
        with self.db.transaction() as c:
            row = c.execute(
                """SELECT s.id AS session_id,s.user_id,u.display_name,u.subject AS feishu_account FROM identity.sessions s
                JOIN identity.users u ON u.id=s.user_id
                WHERE s.token_hash=%s AND NOT s.revoked AND s.expires_at>clock_timestamp()""",
                (digest(token),),
            ).fetchone()
        if not row:
            raise DomainError("AUTH_REQUIRED", "Login required.", "user_action", 401)
        return row

    def logout(self, session):
        with self.db.transaction() as c:
            c.execute("UPDATE identity.sessions SET revoked=true WHERE id=%s", (session,))

    def binding(self, c, user):
        return c.execute(
            "SELECT app,tenant,subject FROM identity.users WHERE id=%s", (user,)
        ).fetchone()

    def display_name(self, tenant, subject, name):
        if not isinstance(name, str) or not name.strip() or len(name) > 200:
            return
        with self.db.transaction() as c:
            c.execute(
                "UPDATE identity.users SET display_name=%s WHERE app=%s AND tenant=%s AND subject=%s",
                (name, self.app, tenant, subject),
            )
