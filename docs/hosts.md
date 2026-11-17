# Original host setup

Run setup in the exact conversation you want to continue. A readable history file does not prove that the host can accept input. Keep native task approvals; never enable global Full Access to bypass a write error.

## Codex

Use `remote-agent-session session attach --agent codex` from the original session. Review and trust only the hooks belonging to this installation when requested. If the current terminal conversation is not loaded into the shared app-server backend, preserve its ID and follow the CLI's supported reconnect instructions. Do not resume it in a second executor while the original client is running.

Other blocking `PermissionRequest` hooks, such as CodeIsland, can independently keep native execution waiting after Feishu approval has been accepted. If execution remains gated, inspect the original host’s pending approval and hook configuration, then choose the intended approval route there. Nonblocking or inactive hooks can coexist. The installer does not disable other hooks or bypass native approvals.

The integration uses evolving Codex IPC/control APIs. Check the [compatibility boundary](compatibility.md). Local readiness, source identity, actual input execution and a received Feishu answer each require verification.

## DeepSeek Harness

Install the bundled plugin using:

```sh
remote-agent-session session plugin install --agent deepseek-harness --profile desktop
```

Use the actual profile name when different; `--dsh` selects an explicit official DSH CLI path. Enable `remote-agent-session-deepseek-harness` in the original application's Plugins page, then reload it after updates. Installation success does not mean the plugin is loaded.

From the original conversation, request the permission plan:

```sh
remote-agent-session session plugin install --agent deepseek-harness --profile desktop --permissions
```

Review the exact profile, physical client state directory, shared write scope and before/after settings. The client default is `~/.remote-agent-session`; the separate backend default `~/.remote-agent-session-service` must never be included in this grant, directly or through a common parent or symlink. Run the returned `apply_command` unchanged only after the native review. Reload the profile as required, then verify real reads, writes, locks, atomic replacement and temporary cleanup. A saved setting does not prove runtime access. A matching already-owned scope requires no repeated approval; newly added scope still requires review.

Check the live plugin and then attach:

```sh
remote-agent-session session plugin check --agent deepseek-harness
remote-agent-session session attach --agent deepseek-harness
```

Use the original `DSH_SESSION_ID` or an explicit user-selected `--session`; never guess the latest session. Keep the original `DSH_HOME`. Plugin checks do not establish Feishu login or pairing. The plugin controls the loaded original owner and must not start a replacement executor.

To revoke owned directory permissions, detach first, request `session plugin remove --agent deepseek-harness --profile desktop --permissions`, review/apply its returned command and reload. Remove the plugin separately without `--permissions` only when no profile needs it. Preserve pre-existing roots and other owners.

## Windows native Codex CLI

Only a native Codex CLI session already running on an explicitly shared foreground app-server is supported. Codex Desktop, standalone CLI executors, DeepSeek Harness and WSL/native mixing are excluded on Windows. Never resume an active standalone conversation in a second executor to manufacture an attachment. Close its original executor deliberately before any supported migration, and preserve the user-selected session ID.

Use ordinary, non-elevated terminals. Select one Codex home containing the intended authentication/configuration and a short local socket path (canonical length below 108 bytes). Let Codex create its `app-server-control` parent with the correct private Windows ACL; do not pre-create a generic inherited-ACL socket directory. In terminal A, set both values explicitly and keep the app-server running in the foreground:

```powershell
$env:CODEX_HOME = Join-Path $HOME '.codex'
$env:REMOTE_AGENT_SESSION_CODEX_SOCKET = Join-Path $env:CODEX_HOME 'app-server-control\app-server-control.sock'
codex app-server --listen "unix://$env:REMOTE_AGENT_SESSION_CODEX_SOCKET"
```

In terminal B, set the identical `CODEX_HOME` and `REMOTE_AGENT_SESSION_CODEX_SOCKET` values before starting the original conversation:

```powershell
$env:CODEX_HOME = Join-Path $HOME '.codex'
$env:REMOTE_AGENT_SESSION_CODEX_SOCKET = Join-Path $env:CODEX_HOME 'app-server-control\app-server-control.sock'
codex --remote "unix://$env:REMOTE_AGENT_SESSION_CODEX_SOCKET"
```

The host and relay must use the same Windows user, native Codex executable, Codex home and socket endpoint. Do not expose the app-server over a public or unauthenticated network listener. The relay deliberately refuses Windows auto-discovery and isolated `--stdio` fallback. A socket left by another process or installation is not permission to delete or replace it; investigate its owner first.

Start the local relay service and log in as described in [operations](operations.md#windows-operation). In the exact original Codex conversation, ask the Agent to run:

```powershell
remote-agent-session.exe session attach --agent codex
remote-agent-session.exe session status
```

If attaching from a separate terminal, export the same two variables there and supply `--session` with the exact original session ID explicitly selected by the user. `CODEX_THREAD_ID` from the original conversation may identify that conversation; a recently modified history file does not. The shared app-server must already own that thread. Environment variables set in another terminal do not propagate into an existing host or relay process: set them before launching each one, and detach/restart the owned relay after changing endpoints.

Review and trust only installation-owned hooks when Codex requests it. Keep the original native sandbox and task approvals; no Full Access, home-directory write grant or global approval bypass is required. Verify original-session identity, local input execution, native approval decisions and actual Feishu receipt separately. Automated transport/CI tests do not establish acceptance of an installed native Codex version or phone delivery.
