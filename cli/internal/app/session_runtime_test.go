package app

import (
	"context"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeDetectionUsesNativeSessionEnvironment(t *testing.T) {
	t.Setenv("DSH_SESSION_ID", "")
	t.Setenv("CODEX_THREAD_ID", "codex-thread")
	if currentRuntime() != "codex" || currentNativeSession("codex") != "codex-thread" {
		t.Fatal("Codex discovery changed")
	}
	t.Setenv("DSH_SESSION_ID", "session-dsh")
	if currentRuntime() != "deepseek-harness" || currentNativeSession("deepseek-harness") != "session-dsh" {
		t.Fatal("DSH did not use native identity")
	}
}

func TestRelaySessionNamePreservesExplicitAndNativeNamesAndRuntimeFallback(t *testing.T) {
	for _, scenario := range []struct {
		explicit string
		native   string
		runtime  string
		expected string
	}{
		{"用户命名", "原生标题", "deepseek-harness", "用户命名"},
		{"", "原生标题", "deepseek-harness", "原生标题"},
		{"", "", "deepseek-harness", "DSH 会话"},
		{"", "", "codex", "Codex 会话"},
		{"", "", "workbuddy", "Agent 会话"},
		{"", "", "", "Agent 会话"},
	} {
		if name := relaySessionName(scenario.explicit, scenario.native, scenario.runtime); name != scenario.expected {
			t.Fatalf("unexpected name %q for %+v", name, scenario)
		}
	}
}

func TestUnsupportedRuntimeNeverStartsCodex(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	client, _, err := openRelayRuntime(context.Background(), nil, "workbuddy", "profile", "session")
	if client != nil || err == nil {
		t.Fatal("unsupported runtime accepted")
	}
}

func TestDSHLeaseIncludesOnlyCurrentPendingAndRunningMessages(t *testing.T) {
	view := sessionView{Generation: 2, Messages: []sessionMessage{
		{ID: "old", Generation: 1, State: "dispatching"},
		{ID: "pending", Generation: 2, State: "pending"},
		{ID: "running", Generation: 2, State: "dispatching"},
		{ID: "done", Generation: 2, State: "completed"},
	}}
	ids := dshOrigins(view)
	if len(ids) != 2 || ids[0] != "pending" || ids[1] != "running" {
		t.Fatal(ids)
	}
}

func TestDSHReconnectUsesPersistedProfileAfterEnvironmentChange(t *testing.T) {
	state := filepath.Join(t.TempDir(), "private")
	store, err := localstate.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	original := t.TempDir()
	instance, err := relay.DSHRuntimeIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRelayEndpoint(instance, original); err != nil {
		t.Fatal(err)
	}
	otherHome := t.TempDir()
	if err := os.Mkdir(filepath.Join(otherHome, "remote-agent-session"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_HOME", otherHome)
	reopened, err := localstate.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := dshRelayDirectory(reopened, instance)
	if err != nil || got != original {
		t.Fatalf("original profile lost: %q %v", got, err)
	}
	fresh, err := dshRelayDirectory(reopened, "")
	if err != nil || fresh != filepath.Join(otherHome, "remote-agent-session") {
		t.Fatalf("new attach ignored selected profile: %q %v", fresh, err)
	}
	unknown := "dsh-unknown"
	if _, err := dshRelayDirectory(reopened, unknown); err == nil {
		t.Fatal("unknown profile redirected to current environment")
	}
	if err := store.SaveRelayEndpoint(instance, otherHome); err == nil {
		t.Fatal("existing identity overwritten")
	}
}

func TestDSHReconnectRejectsMismatchedStoredEndpoint(t *testing.T) {
	store, err := localstate.Open(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	original := t.TempDir()
	instance, err := relay.DSHRuntimeIdentity(original)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRelayEndpoint(instance, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DSH_HOME", filepath.Dir(original))
	if _, err := dshRelayDirectory(store, instance); err == nil {
		t.Fatal("mismatched stored identity accepted")
	}
}
