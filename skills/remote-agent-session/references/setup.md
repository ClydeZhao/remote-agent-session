# 安装、登录与诊断

从本项目 README 的安装入口准备独立安装。CLI 默认状态目录为 `~/.remote-agent-session`；服务私有状态单独位于 `~/.remote-agent-session-service`，由 `REMOTE_AGENT_SESSION_SERVICE_STATE_DIR` 或 `--service-state-dir` 指定。两者不能重叠。与其他产品的登录、插件、hooks 和后台完全分开，不迁移旧凭据。

运行 `remote-agent-session start` 启动本机服务，`remote-agent-session doctor` 核对本机依赖和状态。首次 live 配置按项目 README 设置飞书自建应用；App Secret 由服务私有目录中的 `feishu.env` 保存，不交给 Agent 或放命令参数。API 默认仅监听 `127.0.0.1:18481`。

用 `remote-agent-session config set --server http://127.0.0.1:18481 --allow-loopback-http` 设置地址。运行 `remote-agent-session login`，将公开配对码和机器人入口交给本人完成确认。本人确认后，用相同 CLI 和状态目录运行 `remote-agent-session login finish`，核实本机凭据已保存。待确认时保留原配对并稍后领取，不重新生成登录请求。不要打印或复制 credential 文件。

`remote-agent-session logout` 撤销本设备认证；断开接续，不代表原宿主工作已停止。未确认注销结果时保留原记录并核对，不清库。

宿主准备和连接按 [session](session.md)。DSH-only 安装与摘要均不需要 Codex。插件安装不等于启用；目录权限保存不等于原 profile 已加载；请保持原生审批并实际检查读写状态。

`start|stop|restart|status|doctor` 管理本机服务。电脑及全部后台离线时机器人无法即时回复；重新上线也不会执行过期消息或旧授权。

DSH 只审阅 CLI 状态目录。不要把服务目录、飞书凭据、加密密钥或 PostgreSQL 纳入宿主写根；检测到服务路径重叠或服务文件标记时，权限计划会拒绝。全局 `--state-dir` 只选 CLI 状态，不改变本机服务路径。
