package relay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type DSHDescriptor struct {
	Protocol int    `json:"protocol"`
	Runtime  string `json:"runtime"`
	Instance string `json:"instance"`
	Socket   string `json:"socket"`
}

func DSHDirectory() (string, error) {
	home := os.Getenv("DSH_HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
		home = filepath.Join(home, ".dsh")
	}
	if !filepath.IsAbs(home) {
		return "", ErrInterruptUnavailable
	}
	return filepath.Join(home, "remote-agent-session"), nil
}

func DSHRuntimeIdentity(directory string) (string, error) {
	path, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return "", ErrInterruptUnavailable
	}
	if !filepath.IsAbs(path) {
		return "", ErrInterruptUnavailable
	}
	digest := sha256.Sum256([]byte(path))
	return "dsh-" + hex.EncodeToString(digest[:]), nil
}

func DiscoverDSH(ctx context.Context, directory, sessionID, expectedRuntime string) (*DSH, string, error) {
	if !filepath.IsAbs(directory) || validatePrivateDiscoveryPath(directory, true) != nil {
		return nil, "", ErrInterruptUnavailable
	}
	runtime, err := DSHRuntimeIdentity(directory)
	if err != nil || (expectedRuntime != "" && expectedRuntime != runtime) {
		return nil, "", ErrInterruptUnavailable
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) > 256 {
		return nil, "", ErrInterruptUnavailable
	}
	var found *DSH
	uncertain := false
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if ctx.Err() != nil {
			if found != nil {
				found.Close()
			}
			return nil, "", ctx.Err()
		}
		path := filepath.Join(directory, entry.Name())
		if validatePrivateDiscoveryPath(path, false) != nil {
			uncertain = true
			continue
		}
		data, err := os.ReadFile(path)
		var descriptor DSHDescriptor
		if err != nil || len(data) > 4096 || json.Unmarshal(data, &descriptor) != nil || descriptor.Protocol != 1 || descriptor.Runtime != "deepseek-harness" || descriptor.Instance == "" || filepath.Dir(descriptor.Socket) != directory {
			uncertain = true
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		client, err := OpenDSH(callCtx, descriptor.Socket, sessionID, descriptor.Instance)
		cancel()
		if err != nil {
			var rpcErr *rpcError
			if errors.As(err, &rpcErr) && rpcErr.Message == "owner_unavailable" {
				continue
			}
			if _, statErr := os.Lstat(descriptor.Socket); errors.Is(statErr, os.ErrNotExist) {
				continue
			}
			uncertain = true
			continue
		}
		if found != nil {
			found.Close()
			client.Close()
			return nil, "", errors.New("多个 DeepSeek Harness 实例持有该会话，请在原应用确认目标后重试")
		}
		found = client
	}
	if found == nil || uncertain {
		if found != nil {
			found.Close()
		}
		return nil, "", ErrInterruptUnavailable
	}
	return found, runtime, nil
}
