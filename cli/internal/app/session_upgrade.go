package app

import (
	"github.com/google/uuid"
	"os"
	"os/exec"
	"strings"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/distribution"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/platformfs"
)

func relayCommand(binary, dir string, activations ...string) *exec.Cmd {
	cmd := exec.Command(binary, "--state-dir", dir, "session", "_serve")
	activation := ""
	if len(activations) > 0 {
		activation = activations[0]
	}
	cmd.Env = append(os.Environ(), "REMOTE_AGENT_SESSION_RELAY_ACTIVATION="+activation)
	configureRelayProcess(cmd)
	return cmd
}

func relayUpgradeCheck(r *remote, dir, binary, identity string, executable os.FileInfo) func() bool {
	_, managedErr := distribution.Discover(binary)
	managed := managedErr == nil
	started := false
	return func() bool {
		if started {
			return true
		}
		current, err := os.Lstat(binary)
		if err != nil || !current.Mode().IsRegular() || !platformfs.Safe(binary, current, false) {
			return false
		}
		if os.SameFile(executable, current) && executable.Size() == current.Size() && executable.ModTime().Equal(current.ModTime()) {
			return false
		}
		replacement, info, err := readRelayIdentity(r, binary)
		if err != nil || !os.SameFile(current, info) {
			return false
		}
		if replacement == relayBaseIdentity(identity) {
			executable = info
			return false
		}
		if managed {
			install, err := distribution.Discover(binary)
			if err != nil || install.Pending() || !strings.HasSuffix(replacement, ":"+install.BinaryHash) {
				return false
			}
		}
		activation := uuid.NewString()
		nextIdentity := replacement + ":" + activation
		cmd := relayCommand(binary, dir, activation)
		if err := cmd.Start(); err != nil {
			return false
		}
		replaced, err := r.store.ReplaceRelayIntent(identity, nextIdentity)
		if err != nil || !replaced {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return false
		}
		_ = cmd.Process.Release()
		started = true
		return true
	}
}

func relayBaseIdentity(identity string) string {
	parts := strings.Split(identity, ":")
	if len(parts) > 3 {
		return strings.Join(parts[:3], ":")
	}
	return identity
}
