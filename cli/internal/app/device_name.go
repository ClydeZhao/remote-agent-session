package app

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

func deviceName() (string, error) {
	return platformDeviceName(runtime.GOOS, os.Hostname, func(ctx context.Context, name string, args ...string) ([]byte, error) {
		return exec.CommandContext(ctx, name, args...).Output()
	})
}

func platformDeviceName(platform string, hostname func() (string, error), command func(context.Context, string, ...string) ([]byte, error)) (string, error) {
	if platform == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		data, err := command(ctx, "/usr/sbin/scutil", "--get", "ComputerName")
		if name := strings.TrimSpace(string(data)); err == nil && ctx.Err() == nil && name != "" {
			return name, nil
		}
	}
	return hostname()
}
