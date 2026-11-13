package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/distribution"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
)

func sessionPlugin(p output.Printer, stateDir string, args []string) int {
	if len(args) > 0 && args[0] == "check" {
		return checkDSHPlugin(p, args[1:])
	}
	if len(args) == 0 || (args[0] != "install" && args[0] != "remove") {
		return usage(p)
	}
	f := flags("session plugin " + args[0])
	agent := f.String("agent", "", "agent runtime")
	profile := f.String("profile", "", "DSH profile")
	dsh := f.String("dsh", "", "absolute path to DSH CLI")
	permissions := f.Bool("permissions", false, "review the exact profile state-directory grant")
	apply := f.String("apply", "", "apply the reviewed permission plan id")
	session := f.String("session", os.Getenv("DSH_SESSION_ID"), "current native session")
	if f.Parse(args[1:]) != nil || f.NArg() != 0 || *agent != "deepseek-harness" || !regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`).MatchString(*profile) {
		return usage(p)
	}
	if runtime.GOOS == "windows" {
		return localFailure(p, errors.New("当前接续插件需要 Unix socket，请在受支持的 macOS 或 Linux 宿主安装"))
	}
	if *permissions {
		return sessionPluginPermissions(p, stateDir, args[0], *profile, *session, *apply)
	}
	if *apply != "" {
		return usage(p)
	}
	command, err := dshPluginCommand(*dsh)
	if err != nil {
		return localFailure(p, err)
	}
	operation, target := "remove", "remote-agent-session-deepseek-harness"
	if args[0] == "install" {
		executable, err := os.Executable()
		if err != nil {
			return localFailure(p, err)
		}
		target, err = distribution.PrepareDSHPlugin(executable)
		if err != nil {
			return localFailure(p, err)
		}
		operation = "add"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, command, "plugin", "--profile", *profile, operation, target)
	if err := cmd.Run(); err != nil {
		return localFailure(p, errors.New("DSH 插件管理器未确认完成。请在原应用检查指定 profile 的插件状态；不要直接假定安装或移除成功"))
	}
	message := "DSH 已完成插件安装。请在原应用的 Plugins 页面核对并启用 Remote Agent Session 插件；更新已加载的插件后需关闭再启用。随后在原会话运行相同 install 命令并加 --permissions，审阅该 profile 共享的精确状态目录授权；插件启用不等于状态访问已验证，不需要切换全局完全访问。"
	if operation == "remove" {
		message = "DSH 已完成插件移除。请在原应用确认插件已停用；原会话历史保留。"
	}
	return p.Result(map[string]string{"agent": *agent, "profile": *profile, "operation": operation, "status": "host_command_completed"}, message)
}

func dshPluginCommand(explicit string) (string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return "", errors.New("--dsh 需要 DSH CLI 的绝对路径")
		}
		return explicit, nil
	}
	if path, err := exec.LookPath("dsh"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "darwin" {
		path := "/Applications/DeepSeek Harness.app/Contents/Resources/runtime/cli/bin/dsh"
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			return path, nil
		}
	}
	return "", errors.New("找不到 DSH CLI，请安装 DeepSeek Harness，或用 --dsh 指定其 CLI 绝对路径")
}
