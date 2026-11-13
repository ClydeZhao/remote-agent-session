package app

import (
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"os"
	"os/exec"
	"path/filepath"
)

func lifecycle(p output.Printer, stateDir string, args []string) int {
	root := os.Getenv("REMOTE_AGENT_SESSION_HOME")
	if root == "" {
		exe, err := os.Executable()
		if err != nil {
			return localFailure(p, err)
		}
		root = filepath.Dir(filepath.Dir(exe))
	}
	script := filepath.Join(root, "tools", "lifecycle.py")
	if _, err := os.Stat(script); err != nil {
		return localFailure(p, err)
	}
	python := filepath.Join(root, ".venv", "bin", "python")
	commandArgs := append([]string{script}, args...)
	if p.JSON {
		commandArgs = append(commandArgs, "--json")
	}
	cmd := exec.Command(python, commandArgs...)
	if stateDir == "" {
		var err error
		stateDir, err = localstate.DefaultDir()
		if err != nil {
			return localFailure(p, err)
		}
	}
	clientRoot, err := filepath.Abs(stateDir)
	if err != nil {
		return localFailure(p, err)
	}
	cmd.Env = append(os.Environ(), "REMOTE_AGENT_SESSION_CLIENT_STATE_DIR="+clientRoot)
	cmd.Stdout = p.Out
	cmd.Stderr = p.Err
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			return e.ExitCode()
		}
		return localFailure(p, err)
	}
	return 0
}
