# Local setup

On macOS with Homebrew installed, prepare local dependencies before running the installer:

```sh
brew install python@3.13 postgresql@15
export REMOTE_AGENT_SESSION_PYTHON="$(brew --prefix python@3.13)/bin/python3.13"
export REMOTE_AGENT_SESSION_PG_BIN="$(brew --prefix postgresql@15)/bin"
```

Source builds additionally need `brew install go`. DSH uses the Node runtime required by its host. Do not enable Homebrew's shared PostgreSQL service for this product: the supervisor owns its own cluster. Retain the PostgreSQL bin setting when invoking lifecycle commands; login autostart records its resolved binary directory.

Use a separate self-built Feishu application for this installation. Reusing an application that another process already consumes risks lost or split events. The supervisor locks the app ID across this product's local installations; it cannot lock unrelated software or another computer. Never stop or reconfigure another product to resolve this silently.

Create `~/.remote-agent-session-service` with permissions 0700 and its `feishu.env` with permissions 0600. Write credentials with a local editor or secret manager; do not paste them into shell command arguments, chat, logs or Git. The file is parsed as literal `KEY=value` data, never sourced as shell code:

```text
LARK_APP_ID=your_application_id
LARK_APP_SECRET=your_application_secret
```

The service reads only this private file. Use `REMOTE_AGENT_SESSION_SERVICE_STATE_DIR` or lifecycle `--service-state-dir` consistently for a different backend directory. `REMOTE_AGENT_SESSION_STATE_DIR` and global CLI `--state-dir` select client credentials and bridge state only; they never select backend storage. The installer generates a separate encryption key and private PostgreSQL cluster at first startup; no BuddyBio state, login, binding or key is imported.

In the Feishu application console:

1. Enable bot capability and allow the intended user to access the application.
2. Enable long-connection event delivery. Subscribe to `im.message.receive_v1`, `im.message.reaction.created_v1`, and `im.message.reaction.deleted_v1`.
3. Grant bot message sending (`im:message:send_as_bot`), private-chat receive/read (`im:message.p2p_msg:readonly`) and reaction access (`im:message.reactions:read`, `im:message.reactions:write_only`), and message read (`im:message:readonly`) for quoted-message resolution; add only further scopes explicitly required by the application console for these APIs. Verify the published application version contains the scopes and subscriptions.
4. Start the local service and complete `remote-agent-session login`, then send the displayed pairing code to the bot in **your private chat**.

The receiver connects outbound; no public IP, tunnel, domain or inbound web port is required. See [Feishu long-connection documentation](https://open.feishu.cn/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-subscription-configure-). Console setup, event connection, successful pairing and receipt of a host answer are distinct checks.

The client directory defaults to `~/.remote-agent-session`. Keep the backend and client roots separate: equality, ancestors, descendants and symlink aliases are rejected for known client roots and registered DSH client grants. Never grant the backend directory or a common parent to an Agent. The client permission planner also checks the fixed private `~/.remote-agent-session-runtime/service-roots.json` registry before offering/applying a write grant, including custom service roots started from an earlier shell. Granted client roots are likewise recorded in fixed `client-roots.json` before native permission changes, so a later service startup does not depend on the original custom `DSH_HOME` still being exported. Registered roots remain protected after stop or grant removal while historical data/grants may remain. Pre-registry candidates require the explicit profile metadata refresh described in [migration](operations.md#migrating-an-earlier-combined-directory). These directories share an OS user; this separation does not create read isolation from processes already allowed to read that user's home.

The API listens on `127.0.0.1:18481`; authenticated routes retain device bearer authentication. PostgreSQL accepts only Unix-socket connections inside the private state directory. Its local trust authentication is limited by filesystem permissions; processes running as the same OS user can access this data. The local service is not a sandbox against that user.

An offline development start is `remote-agent-session start --mode offline`. It does not connect to Feishu and simulated results do not establish real delivery. Stop it before starting live mode. Host setup remains a separate step.

Live inbound messages require a valid Feishu creation timestamp and are rejected when older than five minutes or more than thirty seconds ahead of the local clock. Keep the computer clock synchronized. After an outage, resend the intended instruction explicitly when the service is ready; do not assume Feishu will replay every missed event. Old approval cards remain subject to their original expiry and binding checks.
