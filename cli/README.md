# Remote Agent Session CLI

Go CLI 与本地桥接，接续确切原 Codex 或 DeepSeek Harness 会话。安装与飞书配置见 [项目说明](../README.md)，宿主操作见 [接续指引](internal/buildinfo/references/session.md)。

运行 `remote-agent-session help` 查看命令。全局 `--json`、`--state-dir`、`--timeout` 放命令前。默认状态目录 `~/.remote-agent-session`；`REMOTE_AGENT_SESSION_STATE_DIR` 可指定独立状态目录。

`login` 发起或复用配对，`login finish` 领取认证；`session attach|status|detach` 处理原会话。恢复同一最近目标使用飞书 `/session resume`。`session hooks` 仅生成需原生信任的 Codex 配置；DSH 使用自己的匹配插件。

正文经过完整终态及全部来源校验后发送，长回答另调用宿主原生模型生成 TL;DR。摘要失败保留正文，DSH 不依赖 Codex。连接、模型执行和飞书实收分别核对。
