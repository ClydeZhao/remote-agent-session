# Compatibility and acceptance boundaries

| Component | Requirement | Claim boundary |
| --- | --- | --- |
| macOS | Python >=3.11, PostgreSQL >=15, Go toolchain for source build | First release target; a candidate still requires clean install and real end-to-end acceptance |
| Linux | Same backend prerequisites, Unix sockets | Not a validated release platform; login autostart is not implemented |
| Windows | Native Python >=3.11, PostgreSQL >=15 tools, Go for source builds; non-elevated terminal and NTFS owner ACLs | Native Codex CLI only, explicitly shared foreground app-server and exact session ID; Desktop, DSH, WSL mixing and autostart excluded. Native host/Feishu acceptance pending |
| Codex on macOS | CLI 0.167.1 with daemon 0.168.0, default shared-backend path | Original-session main path verified; experimental interfaces, no blanket version promise |
| DeepSeek Harness | Native 0.2.2-rc.2, Node >=22; openai/gpt-6.1-terra configuration | Original-session main path, permissions, interruption, long answers and TL;DR covered; timed asynchronous questions, quoted replies, multi-select and recovery across backend restart/plugin reload covered |
| Feishu | Independent self-built app, private chat, long connection and required scopes | API acceptance is distinct from mobile receipt |

Keep CLI, backend, plugin and product skill from one release candidate. The DSH plugin retains its independent integration version. No public release or all-platform support is implied by this table. Native Codex 0.168.0 sidecar approval allow/deny is covered through Feishu reactions and corresponding file outcomes. Shared-backend 0.168.0 covers the main path, asynchronous questions and interruption. This table makes no blanket compatibility promise for later daemon versions, including 0.169.0. Global host hooks are not disabled by installation.

Repeatable acceptance must cover each host's original-session identity, full answers and host-specific TL;DR, question and permission round trips, busy-turn interruption, local intervention privacy, duplicate/late event rejection, explicit detach, host switching, supervisor/bridge/plugin restart and reconnect after idle. Test DSH summary generation without Codex installed or authenticated. Real phone receipt and native host approvals require manual observation. Keep one-off logs/results outside this repository.
