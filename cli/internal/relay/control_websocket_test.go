//go:build !windows

package relay

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func TestControlSocketWebSocketAndPrivateSymlink(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "bb-control-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "server.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, e := websocket.Accept(w, r, nil)
		if e != nil {
			return
		}
		defer conn.CloseNow()
		for {
			_, data, e := conn.Read(r.Context())
			if e != nil {
				return
			}
			var req struct {
				ID     int64  `json:"id"`
				Method string `json:"method"`
			}
			if json.Unmarshal(data, &req) != nil {
				t.Error("invalid RPC frame")
				return
			}
			if req.ID == 0 {
				continue
			}
			result := map[string]any{}
			if req.Method == "thread/read" {
				result["thread"] = map[string]any{"id": "thread", "cwd": "/test", "status": map[string]string{"type": "idle"}}
			}
			out, _ := json.Marshal(map[string]any{"id": req.ID, "result": result})
			if conn.Write(r.Context(), websocket.MessageText, out) != nil {
				return
			}
		}
	})}
	go server.Serve(listener)
	defer server.Close()
	link := filepath.Join(dir, "alias.sock")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", link)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c, err := OpenCodex(ctx, "must-not-launch-another-executor")
	if err != nil {
		t.Fatal(err)
	}
	defer c.RPC.Close()
	thread, err := c.Inspect(ctx, "thread")
	if err != nil || thread.Status.Type != "idle" {
		t.Fatal("WebSocket RPC failed", err)
	}
	if err = os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err = OpenCodex(ctx, ""); err == nil {
		t.Fatal("accepted writable control directory")
	}
	os.Chmod(dir, 0700)
}

func TestLiveOwnedControlSocket(t *testing.T) {
	socket := os.Getenv("REMOTE_AGENT_SESSION_TEST_CONTROL_SOCKET")
	threadID := os.Getenv("REMOTE_AGENT_SESSION_TEST_OWNED_THREAD")
	if socket == "" || threadID == "" {
		t.Skip("explicit isolated executor required")
	}
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", socket)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := OpenCodex(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.RPC.Close()
	thread, err := c.Inspect(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if thread.Status.Type == "notLoaded" {
		t.Fatal("not connected to original executor")
	}
	var loaded struct {
		Data []string `json:"data"`
	}
	if err = c.RPC.Call(ctx, "thread/loaded/list", map[string]any{}, &loaded); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range loaded.Data {
		if id == threadID {
			found = true
		}
	}
	if !found {
		t.Fatal("target not owned")
	}
	if thread.Status.Type == "idle" {
		if err = c.InterruptActive(ctx, threadID, func() error { t.Fatal("idle executor should not interrupt"); return nil }); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLiveAdapterInterruptAndContinue(t *testing.T) {
	socket := os.Getenv("REMOTE_AGENT_SESSION_TEST_CONTROL_SOCKET")
	if socket == "" || os.Getenv("REMOTE_AGENT_SESSION_TEST_CONTROL_MUTATIONS") != "1" {
		t.Skip("explicit isolated executor and mutation opt-in required")
	}
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", socket)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := OpenCodex(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	defer c.RPC.Close()
	cwd := t.TempDir()
	var created struct {
		Thread Thread `json:"thread"`
	}
	if err = c.RPC.Call(ctx, "thread/start", map[string]any{"cwd": cwd, "approvalPolicy": "never", "sandbox": "read-only", "baseInstructions": "Isolated relay verification. Do not modify files, send messages, or create other agents. Follow the test prompt."}, &created); err != nil {
		t.Fatal(err)
	}
	thread := created.Thread.ID
	if thread == "" {
		t.Fatal("missing new thread")
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 15*time.Second)
		if err := c.InterruptActive(cleanup, thread, func() error { return nil }); err != nil {
			t.Errorf("cleanup interrupt: %v", err)
		}
		stop()
		unsubscribe, unsubscribeCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer unsubscribeCancel()
		if err := c.RPC.Call(unsubscribe, "thread/unsubscribe", map[string]any{"threadId": thread}, nil); err != nil {
			t.Errorf("cleanup unsubscribe: %v", err)
		}
	}()
	first, second := uuid.NewString(), uuid.NewString()
	since := time.Now()
	authorizeCount := 0
	authorize := func() error { authorizeCount++; return nil }
	if err = c.StartRemoteInput(ctx, thread, first, "Use the terminal to run sleep 30 once, then reply WAIT_FINISHED. Do nothing else.", authorize); err != nil {
		t.Fatal(err)
	}
	working := false
	for !working {
		select {
		case <-ctx.Done():
			t.Fatal("did not observe work", ctx.Err())
		case raw := <-c.RPC.Events:
			var event struct {
				Method string `json:"method"`
				Params struct {
					Thread string `json:"threadId"`
					Item   Item   `json:"item"`
				} `json:"params"`
			}
			if json.Unmarshal(raw, &event) != nil {
				t.Fatal("invalid event")
			}
			working = event.Params.Thread == thread && event.Method == "item/started" && (event.Params.Item.Type == "commandExecution" || event.Params.Item.Type == "dynamicToolCall")
		}
	}
	if err = c.InterruptActive(ctx, thread, authorize); err != nil {
		t.Fatal(err)
	}
	awaitOutcome := func(id, status string) Outcome {
		t.Helper()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			outcome, e := c.Outcome(ctx, thread, id, since)
			if e != nil {
				t.Fatal(e)
			}
			if outcome.Complete {
				if outcome.Status != status {
					t.Fatalf("want %s, got %s", status, outcome.Status)
				}
				return outcome
			}
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-ticker.C:
			}
		}
	}
	awaitOutcome(first, "interrupted")
	if err = c.StartRemoteInput(ctx, thread, second, "Reply exactly ADAPTER_RESUMED. Do not call tools.", authorize); err != nil {
		t.Fatal(err)
	}
	outcome := awaitOutcome(second, "completed")
	if strings.TrimSpace(outcome.Text) != "ADAPTER_RESUMED" {
		t.Fatalf("unexpected continuation %q", outcome.Text)
	}
	if authorizeCount != 3 {
		t.Fatalf("authorization callbacks: %d", authorizeCount)
	}
	t.Log("adapter confirmed active interruption and same-session continuation with source IDs")
}
