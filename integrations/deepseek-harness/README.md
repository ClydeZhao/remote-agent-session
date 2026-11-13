# DeepSeek Harness 本地接续插件

插件在原宿主进程中查找已加载、允许人类交互的原 Agent，向 Remote Agent Session 提供私有 Unix socket。仅控制现有实例，不调用 create 或 resume；原宿主拥有会话、输入队列和事件日志。

rc.1 按原生 `agents.roots()` 判断运行时根 Agent；rc.2 按原生 Session 的 `origin` 判断普通会话，拒绝 `subagent` 与未知来源，历史 `parentSession` 本身不代表子代理。两条路径都先核对注册表持有确切 live 实例，不创建或恢复第二执行器。支持版本只声明已适配的 rc.1 与 rc.2，不开放未知后续版本。

`inspect` 返回原生会话、目录、实例和 owner。后续 history、queued、submit、interrupt、cancel_queued 都必须携带相同目标身份。相同会话 ID 的执行器被替换后，旧 owner 不再有效。submit 使用稳定请求 ID，重试先核对原生队列和持久输入记录；同 ID 不同内容拒绝，撤回后的旧请求不能再次执行。

history 保留全部序号，但只返回来源、轮次边界和回答文本；不转发工具参数、模型推理或模型回放状态。客户端必须检查快照完整性并核对本轮全部输入来源，不能直接把最后一条文本视为可发送结果。

默认端点目录位于宿主 DSH_HOME 下的 remote-agent-session 子目录，可通过插件配置 directory 指定绝对路径。目录必须归当前用户所有且为 0700，不接受符号链接。每次加载使用新实例名、socket 和描述文件，卸载只清理自己的文件。CLI 发现和绑定必须检查文件归属、协议版本及实际实例，不能只相信遗留描述文件。

原生交互通过 bind_interactions 指定当前 generation、获准的输入 ID 和不超过 30 秒的租约。监听器只在确切 live Agent 的 scope 中安装，纯本地请求交回原生链路。权限决定返回宿主的单次字符串 outcome；待决请求在取消、过期、本地介入、断开或卸载后不能再允许。答案接口重新检查 owner、generation、轮次及全部输入来源。旧 generation 的断开不能撤销新绑定；问题答案必须对应原问题 ID 和原选项。

CLI 通过插件自动发现原执行器、路由消息及回传同步和 continued 问答。连接预检必须同时确认 protocol、interactions 和 continued，缺项时要求更新插件。原生问答模式由宿主 preset 拥有：legacy 是同步模式，timed 模式的模型参数是 timeout（秒），不是 timeoutMs；插件不替用户改变默认 preset。

continued 回答先保存发送意图，再调用宿主 userQuestions.answer；只有原生答案消息与持久回执匹配，历史才映射为对应远程请求。结果不明只核对，不再次发送。多题各自登记卡片，原问题来源保持固定，期间已关联答案纳入提交前的全部来源复核。timed 问题在完整目标与代次不变的临时断线后重新核对来源，明确 detach、换绑、登录失效或过期时取消；同步阻塞请求不继承旧授权。插件要求支持 Unix socket 的宿主及 package.json 声明的兼容版本，不覆盖 Windows；单次历史响应限 8 MiB，超限明确失败，不能截断冒充完整历史。发行资源、安装、运行加载与最终验收分别判断，不以包存在或单一路径成功代替完整放行。

在本目录运行 `npm test` 检查原执行器隔离、稳定请求重试、撤回、交互失效和输出最小化。这些边界替身测试不能证明真实桌面或飞书接续。设置 `REMOTE_AGENT_SESSION_TEST_DSH_EXECUTABLE` 为所选 DSH 可执行文件、`REMOTE_AGENT_SESSION_TEST_DSH_SESSION_MODULE` 为该版本的 session 模块后，原生测试额外使用实际 AgentRegistry、消息校验和问答服务，验证普通会话识别、子代理排除及 owner 替换；不能只用保留旧接口的替身证明新版兼容。

完整本机发行构建把匹配插件放在 `share/remote-agent-session/deepseek-harness.tgz`，CLI 内嵌其 SHA-256 并在安装前验证。普通产品升级不自动修改宿主 profile；必须显式运行插件安装命令，再在原应用启用。资源释放注册在 Cordis effect 的 disposer，关闭插件只移除自身 socket 和描述文件。

`summarize` RPC 复核确切 owner 后，按原 Agent 当前模型路由调用原生 `llm.prepareCall` 与 `stream`，不创建会话、不追加原会话历史，也不提供工具或回退到 Codex。输入限 262144 字符，25 秒限时，512 输出 token，拒绝工具响应、缺终态和超长输出。完整正文先登记到服务端，再生成 TL;DR；辅助调用失败仅报告摘要不可用。

## 状态目录授权

先通过 `remote-agent-session session plugin install --agent deepseek-harness --profile desktop` 安装，并在原应用启用或重新加载匹配插件。新增只读 `permission_policy` RPC 固定原会话和 owner，返回实际 mode、profile、profile_directory、workspace_root 与物理 additional_writable_roots；它不授予权限，也不读取 Remote Agent Session 登录凭据。

原会话运行同一安装命令加 `--permissions` 时，仅生成目录授权计划，不重复调用插件管理器；默认文本和 `--json` 都提供完整计划与 apply_command。用户审阅后原样执行该命令，CLI 复核计划和当前配置再保存。同范围及 binary/规范状态目录/environment 归属均不变时，返回 already_configured、review_required=false，不提供 apply_command；无自身归属可撤时返回 already_removed，同样不要求 review。configuration_changes 只描述写根/patch，ownership_changes 描述归属新增；预先已有写根首次登记归属仍须 review，但不会因此认领用户根的删除权。无变更结果仍 runtime_access_verified=false，不代替实际访问验证。原会话必须是 workspace-write，无法解释的动态写根、覆盖冲突、profile 不符或路径别名不静默放行，不自动修改宿主模式。用户流程见[接入指引](../../cli/internal/buildinfo/references/session.md#deepseek-harness-接续)。

配置使用官方 profile 的 cordis.patch.yml 中 additionalWritableRoots，写入精确物理状态目录。授权由该 profile 全部 workspace-write 会话及工具共享，可修改该目录的 credential/config；默认读取能力与新增目录写权限均不构成程序级隔离。请求 JSON/回答正文保持独立工作区，正常状态仍由 CLI localstate 模块拥有。

CLI distribution 管理 profile 中私有 `.remote-agent-session-permissions.json` 归属与 `.remote-agent-session-permissions-pending.json` 恢复记录，通过宿主 package.json.lock 协调配置写入。归属固定可执行文件与规范状态目录；用户预先已有根不认领，多个安装共享根保留引用。`configuration_saved` 和 `runtime_access_verified=false` 仅表示配置已保存，必须按宿主要求重载 profile，再从原会话验证锁、原子保存与临时清理；不能以 plan/apply 成功代替真实体验。

`session plugin remove --agent deepseek-harness --profile desktop --permissions` 同样先计划再应用，只撤本安装新增且无人共享的目录授权，插件保留。只有不再需要共享插件时，才另用不带 --permissions 的 remove 调用官方管理器卸载；配置撤回也须重载核对实际范围。

Go 客户端通过 `relay.OpenDSH` 连接已核对的 socket、实例与会话；所有写入固定 owner。对已获准的隔离原会话，可设置 REMOTE_AGENT_SESSION_TEST_DSH_SOCKET、REMOTE_AGENT_SESSION_TEST_DSH_INSTANCE、REMOTE_AGENT_SESSION_TEST_DSH_SESSION，在 cli 目录运行 `go test ./internal/relay -run '^TestLiveDSHOriginalSessionHistory$' -count=1 -v`。可选 REMOTE_AGENT_SESSION_TEST_DSH_REQUEST 指定该会话中已完成的测试输入 ID，核对来源和完整结果。该测试只读，不创建输入、会话或执行器，也不证明飞书收发。

`relay.DiscoverDSH` 在私有端点目录逐一进行只读 owner 检查，只接受唯一可确认的目标。runtime_instance 使用端点目录规范路径的摘要，与每次插件加载随机产生的 instance 和每个 live Agent 的 owner 分开；自定义 profile 应使用独立端点目录。CLI 在核对原执行器后，将实例与目录映射保存到所选 Remote Agent Session 环境的私有本地状态；重启和撤回按原实例读取，不能因当前 DSH_HOME 改变而选择其他 profile。多个 owner、不能排除的失效端点或不安全描述文件不会自动选择第一个。描述文件须为当前用户的私有普通文件，不接受符号链接、硬链接或越出端点目录的 socket。自动发现的只读验证仅需指定 REMOTE_AGENT_SESSION_TEST_DSH_SESSION，运行 `go test ./internal/relay -run '^TestLiveDSHDiscovery$' -count=1 -v`。
