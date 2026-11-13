//go:build darwin || linux

package app

import (
	"os/exec"
	"syscall"
)

func configureRelayProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }
