package relay

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestLiveNativeCLIOwner(t *testing.T) {
	threadID := os.Getenv("REMOTE_AGENT_SESSION_TEST_NATIVE_CLI_THREAD")
	socket := os.Getenv("REMOTE_AGENT_SESSION_TEST_NATIVE_CLI_SOCKET")
	if threadID == "" || socket == "" {
		t.Skip("explicit native CLI test thread and socket required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, err := openControlSocket(ctx, socket)
	if err != nil {
		t.Fatal(err)
	}
	defer client.RPC.Close()
	thread, err := client.Inspect(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != threadID || thread.Originator != "codex-tui" || thread.Status.Type == "notLoaded" {
		t.Fatalf("original CLI owner not confirmed: source=%s originator=%s status=%s", thread.Source, thread.Originator, thread.Status.Type)
	}
	t.Logf("original CLI thread confirmed; status=%s", thread.Status.Type)
}

func TestLiveNativeCLIUsesDefaultOwner(t *testing.T) {
	id := os.Getenv("REMOTE_AGENT_SESSION_TEST_NATIVE_CLI_THREAD")
	if id == "" {
		t.Skip("explicit native CLI test thread required")
	}
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := OpenCodexForThread(ctx, "", id)
	if err != nil {
		t.Fatal(err)
	}
	defer client.RPC.Close()
	thread, err := client.Inspect(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if thread.ID != id || thread.Originator != "codex-tui" || thread.Status.Type == "notLoaded" {
		t.Fatalf("default connection is not original CLI executor: status=%s originator=%s", thread.Status.Type, thread.Originator)
	}
}
