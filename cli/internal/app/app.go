package app

import (
	"flag"
	"io"
	"os"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/buildinfo"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
)

const help = `Remote Agent Session
用法：remote-agent-session [--json] [--state-dir 目录] 命令
  config set --server URL --allow-loopback-http
  config show
  login [finish] | logout
  session hooks
  session plugin install|remove|check --agent deepseek-harness
  session attach|status|detach
  start|stop|restart|status|doctor
  skills read [session|setup]
  version | help
全局选项放在命令前。保留原生任务审批，仅接续确切原会话。
`

func flags(name string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(io.Discard)
	return f
}

func Run(args []string, stdout, stderr io.Writer) int {
	p := output.Printer{Out: stdout, Err: stderr}
	f := flags("remote-agent-session")
	f.BoolVar(&p.JSON, "json", false, "structured output")
	timeout := f.Duration("timeout", 15*time.Second, "HTTP request timeout")
	stateDir := f.String("state-dir", "", "private local state directory")
	err := f.Parse(args)
	if err == flag.ErrHelp {
		return p.Result(map[string]string{"help": help}, help)
	}
	if err != nil {
		return usage(p)
	}
	args = f.Args()
	if len(args) == 0 || (len(args) == 1 && args[0] == "help") {
		return p.Result(map[string]string{"help": help}, help)
	}
	if len(args) == 1 && args[0] == "version" {
		return p.Result(buildinfo.Info(), "Remote Agent Session "+buildinfo.Version+"（API "+buildinfo.APIVersion+"；构建 "+buildinfo.Revision+"）")
	}
	if len(args) == 2 && args[0] == "skills" && args[1] == "export" {
		return p.Result(map[string]any{"files": buildinfo.SkillFiles()}, "Use --json skills export to read the skill bundle.")
	}
	if (len(args) == 2 || len(args) == 3) && args[0] == "skills" && args[1] == "read" {
		topic := ""
		if len(args) == 3 {
			topic = args[2]
		}
		data, err := buildinfo.ReadSkill(topic)
		if err != nil {
			return usage(p)
		}
		return p.Result(map[string]string{"version": buildinfo.Version, "content": string(data)}, string(data))
	}
	if len(args) == 2 && args[0] == "session" && args[1] == "hook" {
		return relayHookDiagnosed(*stateDir, os.Stdin, stdout, stderr)
	}
	if len(args) >= 2 && args[0] == "session" && args[1] == "hooks" {
		return relayHooks(p, *stateDir, args[2:])
	}
	if len(args) >= 2 && args[0] == "session" && args[1] == "plugin" {
		return sessionPlugin(p, *stateDir, args[2:])
	}
	if args[0] == "start" || args[0] == "stop" || args[0] == "restart" || args[0] == "status" || args[0] == "doctor" || args[0] == "service-install" || args[0] == "service-remove" || args[0] == "service-forget" {
		return lifecycle(p, *stateDir, args)
	}
	if args[0] == "login" || args[0] == "logout" || args[0] == "session" {
		r, e := connect(*stateDir, *timeout)
		if e != nil {
			return report(p, e)
		}
		defer r.close()
		if args[0] == "login" {
			return login(p, r, args[1:])
		}
		if e = r.auth(); e != nil {
			return report(p, e)
		}
		if args[0] == "logout" {
			return logout(p, r, args[1:])
		}
		return sessionCommand(p, r, *stateDir, args[1:])
	}

	if len(args) < 2 || args[0] != "config" {
		return usage(p)
	}
	var cfg localstate.Config
	switch args[1] {
	case "set":
		c := flags("config set")
		c.StringVar(&cfg.Server, "server", "", "API base URL")
		c.BoolVar(&cfg.AllowLoopbackHTTP, "allow-loopback-http", false, "allow isolated loopback HTTP")
		if c.Parse(args[2:]) != nil || c.NArg() != 0 || cfg.Server == "" {
			return usage(p)
		}
		server, e := localstate.ValidateServer(cfg.Server, cfg.AllowLoopbackHTTP)
		if e != nil {
			return p.Failure(output.Error{Code: "CLI_INVALID_SERVER", Category: "usage", Message: e.Error(), RetryAction: "user_action"}, output.Usage)
		}
		cfg.Server = server
	case "show":
		if len(args) != 2 {
			return usage(p)
		}
	default:
		return usage(p)
	}
	if *stateDir == "" {
		*stateDir, err = localstate.DefaultDir()
		if err != nil {
			return localFailure(p, err)
		}
	}
	store, err := localstate.Open(*stateDir)
	if err != nil {
		return localFailure(p, err)
	}
	if args[1] == "set" {
		err = store.SaveConfig(cfg)
	} else {
		cfg, err = store.Config()
	}
	if err != nil {
		return localFailure(p, err)
	}
	human := "服务地址：" + cfg.Server
	if cfg.AllowLoopbackHTTP {
		human += "\n本机开发 HTTP 已启用。"
	}
	return p.Result(cfg, human)
}

func boolText(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}
func usage(p output.Printer) int {
	return p.Failure(output.Error{Code: "CLI_USAGE", Category: "usage", Message: "命令或参数不正确。运行 remote-agent-session help 查看用法；全局选项放在命令前。", RetryAction: "user_action"}, output.Usage)
}
func localFailure(p output.Printer, err error) int {
	return p.Failure(output.Error{Code: "CLI_LOCAL_STATE", Category: "local_state", Message: err.Error(), RetryAction: "user_action"}, output.LocalFailure)
}
