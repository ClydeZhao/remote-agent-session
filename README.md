# Remote Agent Session

Version 0.1.0.

Continue the **same running Codex or DeepSeek Harness conversation** from your own Feishu bot's private chat. The original host retains its context and task approvals; the local bridge forwards remote instructions, full answers, TL;DR summaries, supported questions and one-time permission decisions.

Remote Agent Session is licensed under the [MIT License](LICENSE). Publication of the extracted relay code is authorized by the rights holder. Repository: [ClydeZhao/remote-agent-session](https://github.com/ClydeZhao/remote-agent-session). Third-party components retain their own licenses; see [third-party notices](THIRD_PARTY_NOTICES.md).

The computer, host application and local service must remain awake and online. Feishu and the model provider require network access. A stopped host cannot be restarted by `/session resume`. This product does not include HPC, SSH, research analysis, WorkBuddy or a hosted service.

## Install

macOS is the first target. Linux lifecycle code is present but requires native end-to-end verification. Windows supports native Codex CLI through an explicitly shared foreground app-server; Codex Desktop and DeepSeek Harness on Windows are excluded. Native Windows host and Feishu acceptance remain separate from automated checks. Install Python 3.11+, PostgreSQL 15+ tools (`postgres`, `initdb`, `psql`, `createdb`, `pg_isready`, and `pg_ctl` for development tests); building from source also requires Go matching `cli/go.mod`. DSH requires Node >=22 and a compatible installed host. Do not run as root.

From a checked-out source tree or an unpacked matching release:

```sh
sh install/install.sh
```

The default installation is `~/.local/share/remote-agent-session`. The installer creates a private Python environment, installs pinned backend dependencies, and includes the CLI, backend migrations, host plugin and product skill. It does not edit shell profiles, existing host profiles or BuddyBio installations. Add the printed `bin` directory to PATH, or use its absolute executable path. Use `REMOTE_AGENT_SESSION_INSTALL_DIR` for a new alternate location; existing directories are never overwritten.

On Windows, use an ordinary, non-elevated PowerShell terminal with Python 3.11+ and PostgreSQL 15+ tools available. A source installation additionally needs Go matching `cli/go.mod`:

```powershell
.\install\install.ps1
```

The default is `%LOCALAPPDATA%\remote-agent-session`. The installation has a private owner ACL, an ownership marker, `.venv\Scripts\python.exe` and `bin\remote-agent-session.exe`. It does not install a Windows service, scheduled task, global PATH or profile setting. Follow the complete [Windows host workflow](docs/hosts.md#windows-native-codex-cli) before attaching; a standalone or Desktop session cannot be attached by guessing its ID.

See [local setup](docs/setup.md), [host setup](docs/hosts.md), [operation and removal](docs/operations.md), and [compatibility](docs/compatibility.md).

## Start and connect

Client credentials live in `~/.remote-agent-session`; backend credentials, database and logs live separately in `~/.remote-agent-session-service`. Grant a host access only to the client state root. Create the private Feishu configuration as described in [setup](docs/setup.md), then run:

```sh
remote-agent-session start
remote-agent-session doctor
remote-agent-session login
```

Log in and pair through your own bot. In the **chosen original host session**, install/review the matching integration and attach that exact session:

```sh
remote-agent-session session attach --agent codex
remote-agent-session session status
```

For DSH use `--agent deepseek-harness` after following [host setup](docs/hosts.md). Do not select a guessed recent session. Check the connection acknowledgement in Feishu separately from local bridge readiness. Local-only conversation content is not automatically sent to Feishu.

In Feishu use `/session status`, `/session detach`, `/session resume`, or send a new instruction. Detaching stops forwarding and leaves already-started host work running. Supported questions arrive as cards. React ✅ to allow **this request once**, or ❌ to reject; ordinary text approval does not grant permission. Remote decisions cannot override a resolved, expired or invalidated request.

Full answers are delivered before auxiliary summaries. Summary calls consume the selected host's model quota; failures must leave the full answer available. DSH summaries must not require Codex. Native host compatibility and real Feishu receipt require separate acceptance; successful installation is not that acceptance.

## Development

```sh
uv sync --project server --group dev
(cd server && uv run pytest tests)
cd cli
go test ./...
```

Build a native candidate with `python3 tools/build_release.py --output /private/output/path`. It emits a bundled archive and SHA-256 checksum without publishing. See [release preparation](install/README.md). Keep logs, credentials and one-off acceptance evidence outside Git.
