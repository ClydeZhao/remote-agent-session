# Original host setup

Run setup in the exact conversation you want to continue. A readable history file does not prove that the host can accept input. Keep native task approvals; never enable global Full Access to bypass a write error.

## Codex

Use `remote-agent-session session attach --agent codex` from the original session. Review and trust only the hooks belonging to this installation when requested. If the current terminal conversation is not loaded into the shared app-server backend, preserve its ID and follow the CLI's supported reconnect instructions. Do not resume it in a second executor while the original client is running.

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
