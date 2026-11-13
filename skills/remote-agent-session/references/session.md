# 飞书接续原 Agent 会话

先确认用户正在使用的原宿主。Codex 使用下方 hooks 流程；DeepSeek Harness 使用[DSH 插件流程](#deepseek-harness-接续)。仅支持这两个宿主的确切原会话，不创建替代执行器。

## 在飞书接续当前 Codex 会话

用户明确要求“在飞书接着聊”时，在当前 Codex 会话运行 `remote-agent-session session attach`。CLI 使用当前会话身份并自动准备后台。首次接续先用 `remote-agent-session session hooks` 生成与当前 CLI/登录态匹配的配置，让用户在原 Codex `/hooks` 中信任；不能自动批准 hook 信任，不能仅为接续要求关闭全部权限保护。命令返回 ready 后，带上会话名称，简短告诉用户已连接到飞书，可以打开飞书继续聊。飞书 ACK 会显示已连接的会话名称并引导用户直接在这里发消息；切换会话时还会列出前后目标。连接只做后台协议检查，不需要模型探针或结束当前轮次。命令超时或失败时如实说明未就绪，按提示修复后直接重试 attach；不要把连接中说成成功。

尚未登录时先按 [登录指引](setup.md) 完成 `login` 和 `login finish`。用户完成飞书确认后，Agent 仍须使用相同 CLI 和状态目录领取登录结果，确认 CLI 保存成功后再准备 hooks 和 attach；遇到待领取配对时先 finish，不重新发起配对。

### 配置权限、问答和本地确认预告

`remote-agent-session session hooks` 只输出配置，不会安装或信任。Agent 按以下步骤完成准备，用户只负责原生信任：

1. 确认当前 Codex 使用的配置目录：默认 `~/.codex`，设置了 `CODEX_HOME` 时使用该目录。默认将配置写入此目录的 `hooks.json`，可供同一用户的会话使用；只有用户要求限定项目时，才写入该项目的 `.codex/hooks.json`，并确认项目已被 Codex 信任。
2. 在当前安装及登录环境运行 `remote-agent-session session hooks`，解析输出 JSON。读取目标文件；不存在则从空对象开始。仅合并返回的 `PermissionRequest` 与 `PreToolUse` 匹配组，保留其他事件、匹配组及其他工具的处理器；同一事件、匹配组和命令只保留一份，不能覆盖整份配置。
3. 检查用户级、项目级 `hooks.json` 和 `config.toml` 的 inline hooks：它们会叠加执行，不会互相覆盖。不同安装或状态目录的 Remote Agent Session 处理器可能属于其他实例；不要替换或删除，若不能确认同时运行安全则停止并说明冲突。遇到无法识别的自定义包装命令先说明，不猜测删除。保存前校验 JSON，保留原文件权限。
4. 请用户在原 Codex 的 `/hooks`，或 Desktop 的 Settings → Hooks 中审阅并信任 Remote Agent Session 的处理器。不写信任记录、不使用跳过信任的开关。
5. 再运行 `remote-agent-session session attach`，由实际客户端检查命令、匹配规则、启用和信任状态。未通过则根据缺口修正；不能以配置文件存在代替连接成功。

配置位置与信任机制参考 [Codex hooks 官方文档](https://learn.chatgpt.com/docs/hooks)。受管理配置不允许这些 hooks 时说明限制，不修改组织策略。

指定 `REMOTE_AGENT_SESSION_CODEX_SOCKET` 也必须确认该后台已加载目标会话；能读取历史不代表持有原执行者。返回“后台尚未加载当前会话”时，不重试同一错误端点，也不通过旁路 resume 启动第二执行者。请用户先结束当前终端客户端，再用 `codex --remote unix://后台socket绝对路径 resume 原会话UUID` 接入同一后台，之后重新 attach。后台地址从 `codex app-server daemon version` 的 `socketPath` 取得；不要猜路径或最近会话。

首次 attach 必须由本地发起。普通终端用 `--session 会话UUID` 明确目标，不猜最近会话。ACK 和消息显示电脑、项目、会话名称和短 ID；不得让模型伪造标识。仅飞书发起的回答转发，本地轮次留在本地；共享上下文不等于全量同步。

本地 `remote-agent-session session detach` 与飞书 `/session detach` 都停止转发并保留最近目标；飞书 `/session resume` 恢复，`/session status` 查询。原生队列中的待执行消息会尝试撤回，已开始的工作继续。

电脑及原客户端需保持运行。没有待答问题时，新飞书指令优先打断活跃轮次，依赖 Desktop IPC 或原执行者 control socket；无法确认打断时明确拒绝，不悄悄排队。Typing 表情表示已观察到开始处理，终态或离线后清除。登录失效时飞书提示重新登录。受信任 hooks 将支持的权限请求和 question 交给飞书。权限卡片以本人添加 ✅（仅允许本次）或 ❌（拒绝）为主要入口；勾叉并存时需移除一个，已提交决定不会因移除表情而撤销。收到问题卡后，直接回复你的答案，也可以发 A、B 等选项字母。有多个待答问题时，回复对应卡片。待答期间要开始新任务，发 `/session new 新指令`；没有待答问题时直接说事即可。按请求编号 allow/deny 保留兼容，普通“同意”不是授权回执。未覆盖的工具路径仍需本地处理。连接中、离线、执行结果不明时遵循 CLI 状态，不重复输入。

## DeepSeek Harness 接续

在原 DSH 会话中操作，保持该应用和原执行器运行。宿主原生环境提供 DSH_SESSION_ID；普通终端必须用 `--session` 显式指定用户选择的原生会话 ID，不能猜最近会话或新建执行器替代。自定义 DSH_HOME 必须对应原应用，首次 attach 后后台保存已核对的实例目录，恢复时不随其他 profile 漂移。

1. 使用所选 Remote Agent Session 环境的匹配 CLI。首次准备或插件需更新时运行 `remote-agent-session session plugin install --agent deepseek-harness --profile desktop`；desktop 只适用于默认桌面 profile，自定义 profile 使用其真实名称。找不到 DSH CLI 时可用 `--dsh` 指定官方 CLI 的绝对路径。未托管的源码二进制或旧发行没有匹配插件资源时，先按 setup 安装完整发行版，不猜下载地址。
2. 在原应用 Plugins 确认并启用 remote-agent-session-deepseek-harness；更新已加载插件后关闭再启用。必要的原生信任由本人完成。普通 CLI 更新不会自动改宿主 profile，安装命令成功也不代表插件已加载。
3. 在原会话运行 `remote-agent-session session plugin install --agent deepseek-harness --profile desktop --permissions` 生成目录权限计划。该路径不重复安装插件，也不要求先打开 CLI 状态锁。把默认文本中的 profile、物理状态目录、共享范围、前后写根及操作交给用户一次审阅；已有具体范围授权时直接沿用。确认后原样执行输出的 `apply_command`，不自行拼路径或 --apply 编号。宿主须保持 workspace-write，命令不会改权限模式。同范围及本安装归属已存在时，返回 `already_configured`、`review_required=false` 且没有 apply_command，无需再次向用户确认或应用，实际访问另验。写根预先存在但首次新增归属时仍需 review，不能只看 configuration_changes=false 跳过。
4. 保存返回 `configuration_saved`、`runtime_access_verified=false`，只说明配置已保存。按原应用要求重载 profile 或重启应用，再从原会话验证状态锁、状态保存、原子替换与临时清理，同范围普通调用不再逐次扩权。目录访问由该 profile 全部 workspace-write 会话和工具共享，也可修改 credential/config；默认读取能力没有因此获得程序级隔离。只为可信任务 profile 授权，不展示或复制凭据，不改全局 Full Access。
5. 运行 `remote-agent-session session plugin check --agent deepseek-harness`。该命令只核对原执行器、接口、队列和历史，不发模型输入、不修改飞书绑定；`local_runtime_available=true` 不等于飞书已连接，local_state_access=not_checked 不等于状态目录验证通过。按检查项修复缺口，不用另一个执行器绕过。
6. 未登录时先完成 setup 中的 login/finish，再运行 `remote-agent-session session attach --agent deepseek-harness`。返回 ready 且 online 后，带上真实会话名告诉用户可以在飞书继续。连接失败按实际提示修复，不将插件安装、配置保存或 check 通过当作 attach 成功。

DSH 执行 CLI 需要状态目录写访问（配置读取会加锁，提交会保存稳定请求引用）。仍提示锁、重命名或清理失败时，核对所选环境、原 profile 和配置是否已加载；权限计划冲突或宿主仍拒绝时说明具体缺口，不反复请求单次扩权当作安装完成，不 chmod 放宽权限或另起执行器绕过。请求 JSON 和回答正文使用单独的明确工作区，目录授权不扩到整个个人目录。

DSH 使用插件回传原生权限与问题，不安装 Codex hooks。同步或 timed 问答模式由用户当前宿主 preset 决定，Remote Agent Session 不改默认模式。timed 问题可以在原轮继续工作后再答，临时重载插件或 bridge 会重新核对原问题和来源；明确断开、来源失效或过期的卡片不能复活。多选题按卡片提示回复完整选项名的 JSON 数组，例如 `["苹果","梨"]`；普通文字保留为自定义答案。多个问题使用引用对应卡片的方式回答，不能将普通 prompt 当作原生答案。

DSH 的断开、恢复、来源隔离和飞书卡片规则与上方相同；忙碌接续由原 DSH 执行器完成。Computer Use 等宿主未开放的授权仍在原应用处理，不承诺转发所有本地确认。当前插件使用 Unix socket，不以分发包存在推断 Windows 可用。

撤回前先断开对应飞书连接，在原会话运行 `remote-agent-session session plugin remove --agent deepseek-harness --profile desktop --permissions`，需要审阅时原样执行 apply_command，再重载 profile 核对；`already_removed`、`review_required=false` 表示无需重复应用。此路径保留插件，只撤本安装新增且没有其他安装共享的写根；预先已有的根不认领、不删除。仅当该 profile 不再需要插件时，另执行不带 --permissions 的 `remote-agent-session session plugin remove --agent deepseek-harness --profile desktop` 卸载。自定义 profile 使用真实名称；不因清理单个环境移除共享插件，失败先核对原应用实际状态。

## Computer Use 与本地确认

从飞书发起的工作即将使用 Computer Use、打开本地表单或调用 `request_permissions` 请求额外权限时，受信任的预告 hook 会先发「可能需要在 Codex 确认」。旧版用户可单独合并并信任新增的额外权限预告规则。若看到原生弹窗，请在原会话处理。已开通 Codex 手机远程连接时，可以在手机上打开原会话查看。也可以直接在飞书发「跳过这一步」或新的指令。

升级后先合并新版输出中的所有匹配组，在原客户端信任新增处理器，再重新连接。普通 CLI 的旧聊天若还未加载新增信任，可用新开的聊天核对。如果宿主不能触发该 hook，先用可以转发的普通问答征求是否继续，再调用可能等待确认的工具。用户离开电脑前也可以在同一个聊天中，对确实需要的应用做一次无副作用的读取，并在原生提示中选择仅允许本会话。每个应用和后续独立确认仍以 Codex 原生界面为准。

## 本地与飞书命令

| 入口 | 参数与结果 |
| --- | --- |
| `remote-agent-session session hooks` | 输出当前二进制与状态目录对应的JSON配置；合并而非覆盖已有hooks，原生信任由用户完成 |
| `remote-agent-session session attach [--agent codex或deepseek-harness] [--session 原生ID] [--name 名称]` | 在 DSH 内默认使用 DSH_SESSION_ID，否则使用 CODEX_THREAD_ID；普通终端必须指定准确宿主和原生ID。返回 runtime_kind/runtime_instance/native_session/session_name/generation/status/online |
| `remote-agent-session session plugin install/remove --agent deepseek-harness --profile 名称` | 显式管理指定profile的匹配插件；原生启用/信任仍需确认 |
| `remote-agent-session session plugin install/remove --agent deepseek-harness --profile 名称 --permissions [--session 原生ID]` | 生成状态目录授权/撤回计划；审阅后原样执行输出的 apply_command，保存后仍须原 profile 重载和实际验证；不安装/移除插件 |
| `remote-agent-session session plugin check --agent deepseek-harness [--session 原生ID]` | 只读检查本地原会话接口，不代表飞书已连接 |
| `remote-agent-session session status` | 查看当前绑定，ready且online才表示桥接在线 |
| `remote-agent-session session detach` | 断开转发，保留最近目标与后台 |
| 飞书 `/session new 新指令` | 有待答问题时，明确改做新任务 |
| 飞书 `/session status` | 查询，不改变连接 |
| 飞书 `/session detach` | 断开 |
| 飞书 `/session resume` | 恢复最近目标；本地后台在线、登录有效才能恢复 |
| 权限卡片添加 ✅／❌ | 本人仅允许或拒绝这一次请求，不复制编号；旧卡片或其他消息的表情无效 |
| 飞书问题卡片 | 直接回复你的答案，也可以发选项字母。有多个待答问题时回复对应卡片 |

`SESSION_CONNECTING` 不是成功；检查本地后可重试attach。`SESSION_CHANGED` 刷新绑定，不沿用旧generation。登录失效重新登录；离线消息明确未执行，恢复后由用户重新发送。正文超过262144字符时回本地查看完整回答，不声称截断部分为全部。

后台必须使用原session的 CODEX_HOME。普通 CLI 自动从 `codex app-server daemon version` 发现已运行的本地后台，并核对其确实持有目标线程；不会自动启停或升级该后台。自建共享执行器可通过 REMOTE_AGENT_SESSION_CODEX_SOCKET 显式指定用户拥有的 Unix WebSocket socket；Desktop 使用原生本地 IPC。不为接续创建第二执行者，不对正在运行的session额外resume。没有原执行者控制接口时忙碌消息明确拒绝，不将队列当interrupt。

## 自动 TL;DR

长回答在完整正文登记后生成独立摘要，额外消耗一次模型调用。Codex 使用临时只读隔离会话；DSH 直接调用原宿主当前模型服务，不提供工具、不创建或修改用户会话，无需安装或登录 Codex。摘要保留原结论、不确定性和决策点；限时失败不会撤回或阻塞正文。摘要不能代替原生任务审批。

正文及待摘要记录在同一服务端事务保存。摘要由独立有界 worker 领取，不阻塞新指令、打断或问题回答。未领取记录可在桥接重启后恢复；已经领取但结果不明的模型调用不重复，租约过期只补充摘要不可用提示。摘要回执丢失时沿用原 claim 和原文核对，不重发正文。

显式 attach 会从当前调用的宿主环境启动新一代本地连接程序，旧连接在消息边界交接，不停止原工作。成功确认还要求新程序实际核对原宿主并保存匹配当前完整目标的就绪回执；旧进程尚在线、旧心跳或服务端旧 ready 状态均不能代替。请在目标原宿主中发起连接，避免复用另一宿主的沙箱和路径。
