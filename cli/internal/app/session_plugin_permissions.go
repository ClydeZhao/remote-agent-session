package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/distribution"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func sessionPluginPermissions(p output.Printer, stateDir, operation, profile, sessionID, approved string) int {
	if sessionID == "" {
		return localFailure(p, errors.New("请先通过官方插件管理器安装并启用匹配 Remote Agent Session 插件，再在确切 DSH 原会话生成权限计划；普通终端需用 --session 指定该原会话，不猜最近会话"))
	}
	directory, err := relay.DSHDirectory()
	if err != nil {
		return localFailure(p, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, _, err := relay.DiscoverDSH(ctx, directory, sessionID, "")
	if err != nil {
		return localFailure(p, errors.New("无法确认原 DSH 会话，请先安装、启用并重新加载匹配插件；权限计划不需要先打开 Remote Agent Session 状态目录"))
	}
	defer client.Close()
	policy, err := client.PermissionPolicy(ctx, sessionID)
	if err != nil {
		return localFailure(p, err)
	}
	executable, err := os.Executable()
	if err != nil {
		return localFailure(p, err)
	}
	environment := "standalone"
	installation, installErr := distribution.Discover(executable)
	if stateDir == "" && installErr == nil {
		stateDir = installation.StateDir
	}
	if stateDir == "" {
		stateDir, err = localstate.DefaultDir()
		if err != nil {
			return localFailure(p, err)
		}
	}
	input := distribution.DSHPermissionInput{Profile: profile, ProfileDirectory: policy.ProfileDirectory, Home: filepath.Dir(directory), StateDir: stateDir, Executable: executable, Environment: environment, Operation: operation, EffectiveRoots: policy.AdditionalWritableRoots}
	if policy.Profile != profile {
		return localFailure(p, errors.New("指定 profile 与原 DSH 会话不一致，未修改权限"))
	}
	plan, err := distribution.PlanDSHPermissions(input)
	if err != nil {
		return localFailure(p, err)
	}
	if plan.Status == "already_configured" || plan.Status == "already_removed" {
		message := "本安装的 DSH 状态目录授权及归属已配置，无需重复确认或应用。本次没有写入配置；实际状态锁、原子保存和临时清理是否可用仍需在原宿主核对，实际任务继续按原生权限流程审批。"
		if operation == "remove" {
			message = "本安装已没有需要撤回的 DSH 状态目录授权，无需重复确认或应用。用户原有范围和其他安装的共享授权保留；本次没有写入配置。"
		}
		return p.Result(map[string]any{"plan": plan, "status": plan.Status, "review_required": false, "configuration_applied": false, "runtime_access_verified": false}, message)
	}
	command := shellArg(executable) + " --state-dir " + shellArg(plan.StateDir) + " session plugin " + operation + " --agent deepseek-harness --profile " + shellArg(profile) + " --session " + shellArg(sessionID) + " --permissions --apply " + plan.ID
	if approved == "" {
		review, _ := json.MarshalIndent(plan, "", "  ")
		message := "请审阅本次权限计划：该精确状态目录的写访问由所选 DSH profile 的所有 workspace-write 会话和工具共享；当前未改配置，也未读取登录凭据。\n" + string(review) + "\n确认以上范围后运行：\n" + command
		return p.Result(map[string]any{"plan": plan, "apply_command": command, "review_required": true, "configuration_applied": false, "runtime_access_verified": false}, message)
	}
	if err := distribution.ApplyDSHPermissions(plan, approved); err != nil {
		return localFailure(p, err)
	}
	message := "已保存审阅过的 DSH 状态目录授权及归属记录，尚未确认原宿主已加载。请在原应用按其重载要求重新加载 profile 或重启应用，再从原会话验证状态锁、保存、原子替换与临时清理。实际任务仍按原生权限流程审批；本次未更改沙箱模式。"
	if operation == "remove" {
		message = "已撤回本安装拥有且未被其他安装共享的状态目录授权；用户原有目录和其他安装范围保留。共享插件保留，请重新加载原 DSH profile 核对撤回生效。"
	}
	return p.Result(map[string]any{"plan": plan, "configuration_applied": true, "runtime_access_verified": false, "status": "configuration_saved", "plugin_retained": true}, message)
}

func shellArg(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
