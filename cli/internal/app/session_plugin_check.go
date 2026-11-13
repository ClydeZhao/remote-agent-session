package app

import (
	"context"
	"os"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/distribution"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func checkDSHPlugin(p output.Printer, args []string) int {
	f := flags("session plugin check")
	agent := f.String("agent", "", "agent runtime")
	session := f.String("session", os.Getenv("DSH_SESSION_ID"), "current native session")
	if f.Parse(args) != nil || f.NArg() != 0 || *agent != "deepseek-harness" {
		return usage(p)
	}
	checks := []diagnostic{}
	add := func(name, status, message string) {
		now := time.Now().UTC()
		var checkedAt *time.Time
		if status != "not_checked" {
			checkedAt = &now
		}
		checks = append(checks, diagnostic{Name: name, Status: status, Message: message, CheckedAt: checkedAt})
		if !p.JSON {
			p.Status(p.Err, status, message)
		}
	}
	finish := func(available bool) int {
		message := "原会话的本地接续接口尚未确认，请按检查项处理。"
		exit := exitIncomplete
		if available {
			message = "原会话的本地接续接口可用；本地状态访问、飞书登录和连接尚未检查。请先用 plugin install --permissions 审阅精确目录授权，在原宿主验证状态读写后执行 remote-agent-session session attach --agent deepseek-harness；本检查不代表权限已生效。"
			exit = 0
		}
		if code := p.Result(map[string]any{"agent": *agent, "native_session": *session, "local_runtime_available": available, "feishu_checked": false, "checks": checks}, message); code != 0 {
			return code
		}
		return exit
	}
	add("local_state_access", "not_checked", "插件启用不授予 Remote Agent Session CLI 的本地状态访问。本检查不写状态，也未验证状态锁、原子替换和临时文件清理；先用 session plugin install --agent deepseek-harness --profile 实际profile --permissions 审阅精确目录范围，未应用时仍须原生单次审批，不要切换全局完全访问。")
	executable, err := os.Executable()
	if err == nil {
		_, discoverErr := distribution.PackagedDSHPlugin(executable)
		if discoverErr == nil {
			add("plugin_release_record", "pass", "当前安装完整且包含匹配插件的发行记录；下载摘要在安装插件时校验")
		} else {
			add("plugin_release_record", "not_checked", "当前 CLI 没有可用的插件发行记录；需要安装插件时请使用包含插件的完整 Remote Agent Session 发行版")
		}
	}
	if *session == "" {
		add("original_session", "not_checked", "请在目标 DSH 原会话执行，或用 --session 指定该会话 ID；不会猜测最近会话")
		return finish(false)
	}
	directory, err := relay.DSHDirectory()
	if err != nil {
		add("plugin_runtime", "failed", "无法定位当前 DSH 的插件目录，请核对原应用的 DSH_HOME")
		return finish(false)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, _, err := relay.DiscoverDSH(ctx, directory, *session, "")
	if err != nil {
		add("plugin_runtime", "failed", "无法确认唯一的原会话执行器。请在原应用 Plugins 中核对 Remote Agent Session 插件已启用，并打开目标会话；自定义 profile 需使用其 DSH_HOME")
		return finish(false)
	}
	defer client.Close()
	add("original_session", "pass", "已核对当前插件实例持有该原会话")
	if err := client.CheckInteractions(ctx); err != nil {
		add("native_interactions", "failed", "原宿主缺少所需的权限、问题或异步续答接口；请安装匹配插件并在 Plugins 中重新加载")
		return finish(false)
	}
	add("native_interactions", "pass", "已核对插件的权限、同步问题和异步续答接口；尚未发起实际授权或问答")
	if _, err := client.Queued(ctx, *session, "capability-check"); err != nil {
		add("native_queue", "failed", "无法读取原会话输入队列，请在原应用确认会话仍可用")
		return finish(false)
	}
	if err := client.CheckHistory(ctx, *session); err != nil {
		add("native_history", "failed", "无法取得可核对的完整原会话历史，请检查原应用；未发起新轮次")
		return finish(false)
	}
	add("native_history", "pass", "原会话队列和历史可读取；未创建输入、修改绑定或启动替代执行器")
	return finish(true)
}

type diagnostic struct {
	Name      string        `json:"name"`
	Status    string        `json:"status"`
	Message   string        `json:"message"`
	CheckedAt *time.Time    `json:"checked_at"`
	Error     *output.Error `json:"error,omitempty"`
}
