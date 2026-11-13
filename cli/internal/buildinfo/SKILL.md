---
name: remote-agent-session
description: 将当前 Codex 或 DeepSeek Harness 原会话接到飞书，管理配对、连接、问题与单次审批。
---

先读取 [setup](references/setup.md) 完成独立本机服务与登录，再按 [session](references/session.md) 操作确切原会话。

只支持 Codex 和 DeepSeek Harness。用户授权连接后完成必要检查并执行，不猜最近会话，不新建替代执行器。保留原生问题和任务审批，不要求全局 Full Access。

配对确认、CLI 领取登录、插件加载、原会话可控制、飞书实际送达分别判断。不得把 API accepted 或进程启动当作手机实收。凭据不输出到聊天、日志或命令参数。

电脑、服务、原宿主及网络需持续在线。断开保留已开始的工作；结果不明按原请求核对，不用新 ID 重发。
