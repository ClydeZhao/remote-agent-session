# Session relay backend

Python 3.11+ and PostgreSQL 15+ run locally. The installer and lifecycle commands described in the root README manage the normal installation. This directory is the development entry point.

Install dependencies with `uv sync`. Set `REMOTE_AGENT_SESSION_DATABASE_URL`, then run `uv run alembic upgrade head` from this directory. The independent initial migration creates only identity, session relay, session notification, and environment tables. It incorporates the retained schema changes from the extraction baseline. Existing installations of the source product are not imported or modified.

Runtime configuration:

| Variable | Meaning |
| --- | --- |
| `REMOTE_AGENT_SESSION_MODE` | Required `live` or explicit `offline` simulation |
| `REMOTE_AGENT_SESSION_DATABASE_URL` | Private local PostgreSQL connection URL |
| `REMOTE_AGENT_SESSION_KEY_FILE` | Owned private regular file containing a Fernet key |
| `REMOTE_AGENT_SESSION_FEISHU_CONFIG` | Live mode only: owned private regular file with literal `LARK_APP_ID` and `LARK_APP_SECRET` assignments |
| `REMOTE_AGENT_SESSION_INSTANCE_ID` | Optional nonsecret startup nonce used by the supervisor to distinguish this API instance from another process on the same port |
| `REMOTE_AGENT_SESSION_BUILD_REVISION` | Optional safe build identifier |

Configuration files must have no group or other permissions. Their contents are parsed literally, never executed. Secrets are excluded from settings representations and request diagnostics.

The three processes use the same configuration:

- API: `uv run uvicorn remote_agent_session.api:create_app --factory --host 127.0.0.1 --port 8765`
- Notifications and expiry: `uv run python -m remote_agent_session.monitor`
- Feishu outbound WebSocket receiver: `uv run python -m remote_agent_session.feishu_events`

`GET /health` verifies database access. It does not prove Feishu connectivity, host readiness, message delivery, or phone rendering. API routes require paired device authentication except device pairing and health. The runtime-aware bridge sends `X-Remote-Agent-Session-Relay-Runtime: 1`.

Live inbound messages require Feishu `message.create_time` in milliseconds. Messages older than five minutes, over thirty seconds in the future, or with missing/invalid timestamps are persisted as rejected input and receive a deduplicated explanation. They cannot submit work or answer questions; expired pairing messages cannot authorize a device. Keep the local clock synchronized. An offline interval is not a promise of replay: reconnect and resend intentionally. Permission reactions remain subject to the exact active interaction, owner, generation, and expiry checks.

The bridge persists the full result and its pending summary job in one transaction before requesting an auxiliary summary. Unclaimed jobs survive process restarts. A runtime worker claims a job with a stable UUID; replaying that claim returns the same live job, and completed or empty claims never allocate another job. The claim has a two-minute lease and is never reassigned after it might have started a model call. Lease expiry emits one summary-unavailable notice rather than invoking the model again. A process crash after claiming can therefore leave the summary unavailable; the full answer remains available.

The summary callback accepts only its exact claimed completed message on the current active binding; retries preserve its digest. Detach, host/session switches, logout, and authentication expiry cancel outstanding jobs. Notification ordering keeps all body parts ahead of the summary. A missing summary does not cancel or resend the body.

PostgreSQL transactions, row locks, advisory locks, JSONB, and `SKIP LOCKED` are part of the durability contract. Notification platform calls occur outside their database transaction; unknown external outcomes are reconciled without automatic duplicate sends. Each monitor and event receiver owns a database advisory lock. That lock cannot coordinate another installation using a different database or a third-party consumer of the same Feishu app: use one receiver per app.

Run `uv run pytest` here. Tests create and stop a temporary isolated PostgreSQL cluster with Unix sockets only; `pg_config` must resolve its server binaries. Run `uv run ruff check .` for lint. From the repository root, `uv run --project server python -m remote_agent_session.openapi --check` verifies the generated contract; omit `--check` to regenerate it.

Tests use synthetic transport boundaries and actual PostgreSQL persistence. They do not establish compatibility with every Codex/DSH version or actual Feishu delivery. Host and phone acceptance require their documented real environment checks.
