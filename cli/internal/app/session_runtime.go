package app

import (
	"context"
	"errors"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"os"
)

func currentRuntime() string {
	if os.Getenv("DSH_SESSION_ID") != "" {
		return "deepseek-harness"
	}
	return "codex"
}

func currentNativeSession(runtime string) string {
	if runtime == "deepseek-harness" {
		return os.Getenv("DSH_SESSION_ID")
	}
	return os.Getenv("CODEX_THREAD_ID")
}

func relaySessionName(explicit, native, runtime string) string {
	if explicit != "" {
		return explicit
	}
	if native != "" {
		return native
	}
	switch runtime {
	case "codex":
		return "Codex 会话"
	case "deepseek-harness":
		return "DSH 会话"
	default:
		return "Agent 会话"
	}
}

func openRelayRuntime(ctx context.Context, store *localstate.Store, runtime, instance, session string) (relay.Session, string, error) {
	switch runtime {
	case "", "codex":
		client, err := relay.OpenCodexForThread(ctx, "", session)
		return client, instance, err
	case "deepseek-harness":
		directory, err := dshRelayDirectory(store, instance)
		if err != nil {
			return nil, "", err
		}
		client, actual, err := relay.DiscoverDSH(ctx, directory, session, instance)
		if err != nil {
			return nil, "", err
		}
		if err = store.SaveRelayEndpoint(actual, directory); err != nil {
			client.Close()
			return nil, "", err
		}
		return client, actual, nil
	default:
		return nil, "", errors.New("当前 Remote Agent Session 尚不能连接这个 Agent，请在原应用继续操作")
	}
}

func dshRelayDirectory(store *localstate.Store, instance string) (string, error) {
	if instance != "" {
		directory, err := store.RelayEndpoint(instance)
		if err != nil {
			return "", err
		}
		if directory != "" {
			actual, err := relay.DSHRuntimeIdentity(directory)
			if err != nil || actual != instance {
				return "", relay.ErrInterruptUnavailable
			}
			return directory, nil
		}
	}
	directory, err := relay.DSHDirectory()
	if err != nil {
		return "", err
	}
	if instance != "" {
		actual, err := relay.DSHRuntimeIdentity(directory)
		if err != nil || actual != instance {
			return "", relay.ErrInterruptUnavailable
		}
	}
	return directory, nil
}
