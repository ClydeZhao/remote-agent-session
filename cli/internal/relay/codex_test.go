package relay

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func TestCompletedTurnRequiresExclusiveClientOrigin(t *testing.T) {
	base := Turn{ID: "turn", Status: "completed", CompletedAt: completedTime(), ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "feishu-id"}, {Type: "agentMessage", Text: "answer", Phase: "final_answer"}}}
	o, e := classify(base, "feishu-id")
	if e != nil || o.Text != "answer" || !o.Complete {
		t.Fatalf("valid remote turn: %+v %v", o, e)
	}
	base.Items = append(base.Items, Item{Type: "userMessage", ClientID: "local-id"})
	o, e = classify(base, "feishu-id")
	if !errors.Is(e, ErrMixedOrigin) || o.Text != "" {
		t.Fatalf("mixed input leaked: %+v %v", o, e)
	}
	base.ItemsView = "summary"
	if _, e = classify(base, "feishu-id"); !errors.Is(e, ErrHistoryIncomplete) {
		t.Fatal("summary history accepted")
	}
}
func TestLocalAndInProgressTurnsDoNotProduceForwardableText(t *testing.T) {
	for _, status := range []string{"inProgress", "completed", "failed"} {
		o, e := classify(Turn{Status: status, ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "local"}, {Type: "agentMessage", Text: "local private answer"}}}, "remote")
		if e != nil || o.Found || o.Text != "" {
			t.Fatalf("local turn forwarded: %+v %v", o, e)
		}
	}
	o, e := classify(Turn{Status: "inProgress", ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "remote"}, {Type: "agentMessage", Text: "partial"}}}, "remote")
	if e != nil || !o.Found || o.Complete || o.Text != "" {
		t.Fatalf("partial answer forwarded: %+v %v", o, e)
	}
}
func TestHistorySearchFollowsPages(t *testing.T) {
	reader, send := io.Pipe()
	requests, writer := io.Pipe()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	calls := make(chan string, 3)
	go func() {
		defer send.Close()
		defer requests.Close()
		dec := json.NewDecoder(requests)
		for page := 0; page < 2; page++ {
			var req struct {
				ID     int64          `json:"id"`
				Method string         `json:"method"`
				Params map[string]any `json:"params"`
			}
			if dec.Decode(&req) != nil {
				return
			}
			calls <- req.Method
			if req.Method != "thread/turns/list" || req.Params["itemsView"] != "full" {
				return
			}
			result := map[string]any{"data": []Turn{{ID: "local-turn", Status: "completed", CompletedAt: completedTime(), ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "local"}}}}, "nextCursor": "older"}
			if page == 1 {
				if req.Params["cursor"] != "older" {
					return
				}
				result = map[string]any{"data": []Turn{{ID: "remote-turn", Status: "completed", CompletedAt: completedTime(), ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "remote"}, {Type: "agentMessage", Text: "original answer"}}}}, "nextCursor": nil}
			}
			json.NewEncoder(send).Encode(map[string]any{"id": req.ID, "result": result})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	o, e := (&Codex{RPC: rpc}).Outcome(ctx, "thread", "remote", time.Now())
	if e != nil || o.Text != "original answer" {
		t.Fatalf("pagination failed %+v %v", o, e)
	}
}
func TestSummaryThreshold(t *testing.T) {
	if NeedsSummary("已完成，无需回复。") {
		t.Fatal("short text summarized")
	}
	text := ""
	for i := 0; i < 201; i++ {
		text += "中"
	}
	if !NeedsSummary(text) {
		t.Fatal("long CJK omitted")
	}
	text = ""
	for i := 0; i < 201; i++ {
		text += "word "
	}
	if !NeedsSummary(text) {
		t.Fatal("long English omitted")
	}
}
func TestLiveCodexObserver(t *testing.T) {
	id := os.Getenv("REMOTE_AGENT_SESSION_TEST_CODEX_THREAD")
	if id == "" {
		t.Skip("explicit local Codex test thread required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, e := OpenCodex(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	defer c.RPC.Close()
	thread, e := c.Inspect(ctx, id)
	if e != nil || thread.ID != id {
		t.Fatalf("inspect: %+v %v", thread, e)
	}
	var loaded struct {
		Data []string `json:"data"`
	}
	if e = c.RPC.Call(ctx, "thread/loaded/list", map[string]any{}, &loaded); e != nil || len(loaded.Data) != 0 {
		t.Fatalf("observer unexpectedly owns sessions: %+v %v", loaded, e)
	}
}
func TestLiveSummary(t *testing.T) {
	if os.Getenv("REMOTE_AGENT_SESSION_TEST_SUMMARY") != "1" {
		t.Skip("explicit model summary test required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	summary, e := summarizeCodex(ctx, "任务已经完成，输入检查正常。没有需要用户决定的问题，用户无需回复。")
	if e != nil || summary == "" {
		t.Fatalf("summary failed: %v", e)
	}
}

func TestUnrecognizedStatusIsNotTerminal(t *testing.T) {
	for _, status := range []string{"", "unknown", "in_progress", "inProgress"} {
		o, e := classify(Turn{Status: status, ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "remote"}}}, "remote")
		if e != nil || o.Complete {
			t.Fatalf("status %q treated as complete: %+v %v", status, o, e)
		}
	}
}

func completedTime() *int64 { n := time.Now().Unix(); return &n }
func TestObserverInterruptionWithoutEndTimestampIsStillRunning(t *testing.T) {
	o, e := classify(Turn{Status: "interrupted", ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "remote"}}}, "remote")
	if e != nil || o.Complete || !o.Found {
		t.Fatalf("persisted active turn misclassified: %+v %v", o, e)
	}
}

func TestCancellationTargetsOnlyOwnedQueueItem(t *testing.T) {
	reader, send := io.Pipe()
	requests, writer := io.Pipe()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	done := make(chan string, 1)
	go func() {
		defer send.Close()
		defer requests.Close()
		dec := json.NewDecoder(requests)
		var list struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		if dec.Decode(&list) != nil {
			return
		}
		json.NewEncoder(send).Encode(map[string]any{"id": list.ID, "result": map[string]any{"data": []map[string]string{{"id": "foreign-queue", "clientUserMessageId": "local"}, {"id": "owned-queue", "clientUserMessageId": "remote"}}, "nextCursor": nil}})
		var remove struct {
			ID     int64             `json:"id"`
			Method string            `json:"method"`
			Params map[string]string `json:"params"`
		}
		if dec.Decode(&remove) != nil {
			return
		}
		done <- remove.Method + ":" + remove.Params["queuedSubmissionId"]
		json.NewEncoder(send).Encode(map[string]any{"id": remove.ID, "result": map[string]bool{"deleted": true}})
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := (&Codex{RPC: rpc}).CancelQueued(ctx, "thread", "remote"); err != nil {
		t.Fatal(err)
	}
	if target := <-done; target != "thread/queue/delete:owned-queue" {
		t.Fatal(target)
	}
}

func TestInternalAgentActivityIsNotForwarded(t *testing.T) {
	turn := Turn{ID: "turn", Status: "completed", CompletedAt: completedTime(), ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "remote"}, {Type: "collabAgentToolCall", Text: "Interacted with /root/review_changes"}, {Type: "subAgentActivity", Text: "internal activity"}, {Type: "agentMessage", Phase: "final_answer", Text: "用户回答"}}}
	result, err := classify(turn, "remote")
	if err != nil || result.Text != "用户回答" {
		t.Fatalf("internal activity leaked: %+v %v", result, err)
	}
}

func TestAttachRequiresTrustedSynchronousHooksWithEnoughTime(t *testing.T) {
	for _, scenario := range []string{"valid", "untrusted", "short_timeout", "async", "wrong_state", "missing_attention", "untrusted_attention"} {
		t.Run(scenario, func(t *testing.T) {
			reader, send := io.Pipe()
			requests, writer := io.Pipe()
			rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
			defer rpc.Close()
			go func() {
				defer send.Close()
				defer requests.Close()
				var request struct {
					ID     int64  `json:"id"`
					Method string `json:"method"`
				}
				if json.NewDecoder(requests).Decode(&request) != nil {
					return
				}
				if request.Method != "hooks/list" {
					t.Error("unexpected capability read")
				}
				trust, command, timeout, async := "trusted", "expected-command", 310, false
				switch scenario {
				case "untrusted":
					trust = "untrusted"
				case "short_timeout":
					timeout = 5
				case "async":
					async = true
				case "wrong_state":
					command = "other-command"
				}
				hooks := []map[string]any{}
				for _, event := range []string{"permissionRequest", "preToolUse"} {
					matcher := ".*"
					if event == "preToolUse" {
						matcher = QuestionHookMatcher
					}
					hooks = append(hooks, map[string]any{"eventName": event, "command": command, "enabled": true, "trustStatus": trust, "async": async, "matcher": matcher, "timeoutSec": timeout})
				}
				if scenario != "missing_attention" {
					attentionTrust := trust
					if scenario == "untrusted_attention" {
						attentionTrust = "untrusted"
					}
					hooks = append(hooks, map[string]any{"eventName": "preToolUse", "command": command, "enabled": true, "trustStatus": attentionTrust, "async": async, "matcher": AttentionHookMatcher, "timeoutSec": timeout})
				}
				json.NewEncoder(send).Encode(map[string]any{"id": request.ID, "result": map[string]any{"data": []any{map[string]any{"cwd": "/project", "errors": []any{}, "hooks": hooks}}}})
				io.Copy(io.Discard, requests)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err := (&Codex{RPC: rpc}).RequireInteractionHooks(ctx, "/project", "expected-command")
			if (err == nil) != (scenario == "valid") {
				t.Fatalf("unexpected readiness for %s: %v", scenario, err)
			}
		})
	}
}

func TestEmptyCompletedTurnRetainsTerminalEvidence(t *testing.T) {
	turn := Turn{ID: "turn", Status: "completed", CompletedAt: completedTime(), ItemsView: "full", Items: []Item{{Type: "userMessage", ClientID: "remote"}, {Type: "agentMessage", Phase: "commentary", Text: "working"}}}
	outcome, err := classify(turn, "remote")
	if err != nil || !outcome.Found || !outcome.Complete || outcome.Text != "" {
		t.Fatalf("terminal evidence lost: %+v %v", outcome, err)
	}
	turn.CompletedAt = nil
	outcome, err = classify(turn, "remote")
	if err != nil || outcome.Complete {
		t.Fatalf("unfinished turn settled: %+v %v", outcome, err)
	}
}

func TestRPCBlockedWriteHonorsDeadline(t *testing.T) {
	reader, peer := io.Pipe()
	input, writer := io.Pipe()
	defer peer.Close()
	defer input.Close()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- rpc.Call(ctx, "blocked", nil, nil) }()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wrong deadline result: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		rpc.Close()
		<-result
		t.Fatal("RPC write ignored its deadline")
	}
}

func TestInitializeNotificationWriteHonorsDeadline(t *testing.T) {
	reader, peer := io.Pipe()
	input, writer := io.Pipe()
	defer peer.Close()
	defer input.Close()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	go func() {
		var request struct {
			ID int64 `json:"id"`
		}
		if json.NewDecoder(input).Decode(&request) == nil {
			json.NewEncoder(peer).Encode(map[string]any{"id": request.ID, "result": map[string]any{}})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- rpc.Initialize(ctx) }()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wrong notification deadline: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		rpc.Close()
		<-result
		t.Fatal("initialized notification ignored its deadline")
	}
}

type enteredWriter struct {
	io.Writer
	entered chan struct{}
}

func (w enteredWriter) Write(data []byte) (int, error) {
	select {
	case w.entered <- struct{}{}:
	default:
	}
	return w.Writer.Write(data)
}

func TestRPCWaitingWriterCanCancelBlockedConnection(t *testing.T) {
	reader, peer := io.Pipe()
	input, writer := io.Pipe()
	defer peer.Close()
	defer input.Close()
	entered := make(chan struct{}, 1)
	rpc := NewRPC(reader, enteredWriter{writer, entered}, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	first := make(chan error, 1)
	go func() { first <- rpc.Call(context.Background(), "blocked", nil, nil) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	second := make(chan error, 1)
	go func() { second <- rpc.Call(ctx, "waiting", nil, nil) }()
	select {
	case err := <-second:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("wrong queued writer deadline: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		rpc.Close()
		<-second
		<-first
		t.Fatal("waiting writer ignored its deadline")
	}
	if err := <-first; err == nil {
		t.Fatal("blocked connection falsely succeeded")
	}
}

func TestSuccessfulRPCCallStopsCancellationWatcher(t *testing.T) {
	reader, peer := io.Pipe()
	input, writer := io.Pipe()
	defer peer.Close()
	defer input.Close()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	go func() {
		decoder := json.NewDecoder(input)
		for {
			var request struct {
				ID int64 `json:"id"`
			}
			if decoder.Decode(&request) != nil {
				return
			}
			if json.NewEncoder(peer).Encode(map[string]any{"id": request.ID, "result": map[string]any{}}) != nil {
				return
			}
		}
	}()
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err := rpc.Call(ctx, "ready", nil, nil)
		cancel()
		if err != nil {
			t.Fatal("completed call cancellation closed reusable connection", err)
		}
	}
	select {
	case <-rpc.Done():
		t.Fatal("successful call cancellation closed RPC")
	default:
	}
}

func TestRPCPreservesServerRequestsWithoutConfusingClientReplies(t *testing.T) {
	for _, id := range []string{`"native-request"`, `1`, `0`} {
		t.Run(id, func(t *testing.T) {
			reader, peer := io.Pipe()
			requests, writer := io.Pipe()
			rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
			defer rpc.Close()
			go func() {
				defer peer.Close()
				defer requests.Close()
				var request map[string]json.RawMessage
				if json.NewDecoder(requests).Decode(&request) != nil {
					return
				}
				raw := `{"id":` + id + `,"method":"item/tool/requestUserInput","params":{"threadId":"thread"}}` + "\n"
				if _, err := io.WriteString(peer, raw); err != nil {
					return
				}
				json.NewEncoder(peer).Encode(map[string]any{"id": request["id"], "result": map[string]bool{"ok": true}})
				io.Copy(io.Discard, requests)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var result struct {
				OK bool `json:"ok"`
			}
			if err := rpc.Call(ctx, "probe", nil, &result); err != nil || !result.OK {
				t.Fatalf("server request corrupted reply: %v", err)
			}
			select {
			case raw := <-rpc.Events:
				var request map[string]json.RawMessage
				if json.Unmarshal(raw, &request) != nil || string(request["id"]) != id {
					t.Fatal("lost original server request ID")
				}
			case <-ctx.Done():
				t.Fatal("server request was dropped")
			}
		})
	}
}
