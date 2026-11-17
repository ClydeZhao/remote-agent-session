# Operation and removal

The normal commands are `remote-agent-session start`, `stop`, `restart`, `status`, and `doctor`. All accept `--service-state-dir`, `--mode live|offline`, and `--port`; defaults are `~/.remote-agent-session-service`, live, and 18481. `REMOTE_AGENT_SESSION_SERVICE_STATE_DIR` selects the same backend root. Global CLI `--state-dir` / `REMOTE_AGENT_SESSION_STATE_DIR` remain client-only (default `~/.remote-agent-session`) and are not forwarded as service storage. Use the same service values for every lifecycle command. Lifecycle commands return JSON and never print credentials.

Startup uses one monotonic 180-second readiness budget across initialization, migrations and complete database-health responses; cleanup can finish afterward. A private `startup-status.json` records the current instance's final startup phase and specific failure category. Startup distinguishes an exited component from a health deadline and stops its own supervisor on failure rather than leaving a late background startup.

The supervisor starts its owned PostgreSQL cluster, runs schema migrations, then starts the loopback API, notification monitor and, in live mode, Feishu event consumer. If a managed component exits, it shuts down its siblings. A repeated start returns the existing service rather than starting another consumer. A mode change requires restart; status reports the running mode. The supervisor does not start Codex or DSH, attach sessions, or replay host actions itself.

`service-install` enables macOS login autostart through a per-state LaunchAgent. Stop a manually started service first. `service-remove` removes only the matching installation's LaunchAgent. Launchd retries abnormal service exits with a delay; manual `start` does not install automatic restart. A normal `stop` exits successfully and remains stopped until an explicit start or later login. Neither command enables Full Access or modifies host profiles.

Private `service.log` and `supervisor.log` live in the backend service directory. Read only the relevant bounded tail when diagnosing startup. Do not upload raw logs without checking for private conversation content. `doctor` checks local prerequisites and service status; it does not prove host control, bot delivery or mobile receipt.

Failures to check:

- Missing PostgreSQL executable: put its bin directory on PATH or set `REMOTE_AGENT_SESSION_PG_BIN` to that directory before start.
- Very long custom service paths: choose a shorter private directory; PostgreSQL Unix-socket pathname limits apply after resolving symlinks (especially the macOS `/private` prefix).
- Port in use: select a free `--port` and use the matching API URL in CLI configuration; never kill an unknown listener.
- Another consumer owns the app: stop the owning installation intentionally or configure an independent Feishu application. A database lock cannot coordinate unrelated applications.
- Host unavailable: reopen the original host/session and inspect the matching integration. Do not create a new executor to impersonate recovery.
- Stale or uncertain command: reconcile the original operation ID. Never resend merely because an acknowledgement was lost. Expired instructions and approvals must not execute after restart.

## Updates and uninstall

Build/install each candidate in a new directory. Back up the service state, encryption key and separate client credentials securely, stop the old installation, then start the new installation against the same service state only after reviewing migration compatibility. Reuse the client state explicitly through its own CLI global option/environment. Downgrade is not automatically supported; preserve the backup rather than rerunning an old schema against migrated data. No in-place auto-update or public download URL is provided.

Before removal, detach sessions owned by the installation being removed and use the CLI's host integration removal/permission-review commands to remove only this installation's hooks, plugin and owned write roots. Do not remove shared or user-created settings. From the installed directory run `sh install/uninstall.sh` (add `--service-state-dir PATH` for a custom backend root); it removes only its own login entry and asks the supervisor to stop only when its canonical installation owner matches. An older installation cannot stop a replacement installation sharing the same service state through this cleanup path; mismatched or unverifiable ownership is preserved. Explicit `remote-agent-session stop` remains the deliberate service-wide stop command. Use a matching CLI/control protocol: new supervisors reject unqualified legacy stop requests, and uninstall never falls back to one when ownership cannot be verified. It prints the owned installation directory for explicit deletion. Both client and service state and their backups are retained. Delete them only after confirming there is no remaining binding, unknown request or needed history.

The fixed private directory `~/.remote-agent-session-runtime` holds metadata-only `service-roots.json` and `client-roots.json` registries of canonical service paths and granted client paths, plus their shared ownership lock. Lifecycle owns service registration; the CLI permission owner records client roots before changing host grants. Registration happens before backend secrets are created. Client permission changes and service registration use the same lock and recheck each other before committing; a later shell cannot grant an ancestor of an already registered custom service root merely because its service environment variable is unset. Neither registry has a location override. Client roots remain registered conservatively after grant removal; a new backend should use a separate path rather than repurpose a formerly granted client root. Runtime sockets and process locks default to this directory; a test-specific runtime socket override does not move the registry. Do not delete registry/lock files to bypass an overlap error.

Stopping a service, removing login autostart and uninstalling binaries retain registered service roots because private data remains. After explicitly retiring its private data and ensuring its service has stopped, use `remote-agent-session service-forget --service-state-dir PATH` to remove only its registry entry. This command refuses a nonempty data directory; it never deletes data or secrets.

## Migrating an earlier combined directory

Earlier local candidates placed backend data inside the client root. The product does not automatically move data or reinterpret a client path as service storage. Starting against an overlapping root now fails before backend files are created; stopping or inspecting the old supervisor remains available for cleanup.

1. Identify the exact old installation, combined root, client permission grants and running bindings. Earlier candidates had no global client-root registry: for every previously used custom `DSH_HOME`, run the current matching client permission setup in the original profile and review/apply its ownership-metadata update before starting services from another shell. An existing grant missing this registry is not treated as fully configured; one explicit apply registers it without widening its native write scope. Arbitrary former custom profiles cannot be discovered automatically. Detach and stop its bridges deliberately. Revoke the old installation's owned host write grants through its native permission-review path if they cover a common parent; preserve other owners and user-created settings.
2. Stop and remove login autostart using the **owning old installation**, then verify its service and owned PostgreSQL processes have exited. Back up the whole old state privately, including its encryption key. Never copy a running PostgreSQL data directory as a backup.
3. Create a private separate service directory outside every client grant. Move only backend-owned `feishu.env`, `encryption.key`, `postgres`, `pgsocket`, `startup-status.json`, `.remote-agent-session-service-root` if present, and service/supervisor/launchd logs into it. Retain client `config.json`, credentials, bridge ownership, request references and other client files in the client root. Do not move the whole directory, generate a replacement encryption key, or reset its database. The key and database must remain a matching pair.
4. Start the reviewed new installation with `--service-state-dir` pointing to the separate backend root and the original client global `--state-dir` where needed. Check readiness, login/binding status, and each original host before enabling its login autostart. Treat unknown/expired requests according to their original operation IDs; migration does not authorize replay.

Custom paths must remain disjoint after resolving symlink ancestors, including client roots recorded in known DSH profile permission ownership files and service roots retained in the fixed metadata registry. An unrecognized custom DSH home must be supplied as `DSH_HOME`; the service cannot discover arbitrary profiles elsewhere on disk. Do not work around overlap errors by widening a host's permissions or deleting its ownership records.

## Windows operation

Run the installed `remote-agent-session.exe` from an ordinary terminal. Export `REMOTE_AGENT_SESSION_PG_BIN` when PostgreSQL is outside PATH and retain any explicit service/client root overrides. Before session commands export the same `CODEX_HOME` and `REMOTE_AGENT_SESSION_CODEX_SOCKET` used by the foreground Codex backend; see [host setup](hosts.md#windows-native-codex-cli).

```powershell
remote-agent-session.exe start --mode offline
remote-agent-session.exe status --mode offline
remote-agent-session.exe doctor --mode offline
remote-agent-session.exe stop --mode offline
remote-agent-session.exe start
remote-agent-session.exe login
remote-agent-session.exe status
remote-agent-session.exe stop
```

The local service supervisor owns its database and children; it does not start the foreground Codex backend. Leave the Codex app-server terminal and original `codex --remote` session open while connected. Windows has no service/scheduled-task/login autostart implementation; `service-install` and related autostart operations report unsupported. Repeated start must reuse the owned service. Stop/restart use its authenticated private control endpoint and leave unknown listeners alone. Private logs and state retain owner ACLs. PostgreSQL uses an owned loopback port with private SCRAM credentials; it never reconfigures a shared system cluster.

For uninstall, detach only sessions belonging to this installation and remove its owned host hooks through the supported host workflow. Run the installed `.\install\uninstall.ps1`, appending `--service-state-dir PATH` for a custom root. It verifies the installation marker and requests only `stop-owned`; a replacement installation with the same service root is preserved. It retains the installation directory for reviewed explicit deletion and always retains separate client/backend state and credentials. It does not modify Windows profiles, registry startup entries, unrelated services or BuddyBio. Back up private state securely before updates; install every candidate into a fresh directory.
