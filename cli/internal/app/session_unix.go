//go:build darwin || linux

package app

import (
	"os/exec"
	"strings"
	"syscall"
)

func configureRelayProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func formatRelayHookCommand(binary, directory string) (string, error) {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	return quote(binary) + " --state-dir " + quote(directory) + " session hook", nil
}
