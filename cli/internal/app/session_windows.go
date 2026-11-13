package app

import (
	"os/exec"
	"syscall"
)

func configureRelayProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200 | 0x00000008}
}
