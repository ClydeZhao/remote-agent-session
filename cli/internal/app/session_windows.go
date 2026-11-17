package app

import (
	"errors"
	"os/exec"
	"strings"
	"syscall"
)

func configureRelayProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200 | 0x00000008}
}

func formatRelayHookCommand(binary, directory string) (string, error) {
	for _, value := range []string{binary, directory} {
		if strings.ContainsAny(value, "%!\"\r\n\x00") {
			return "", errors.New("Windows hook 路径不能包含百分号、感叹号、双引号或控制字符")
		}
	}
	quote := func(value string) string {
		trailing := len(value) - len(strings.TrimRight(value, `\`))
		return `"` + value + strings.Repeat(`\`, trailing) + `"`
	}
	return quote(binary) + " --state-dir " + quote(directory) + " session hook", nil
}
