package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func TestForeignHandoffIsRetainedWithoutBlockingCurrentBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/session-relay" {
			t.Error("unexpected cloud request")
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"user_id":"current-user"}`)
	}))
	defer server.Close()
	store, e := localstate.Open(filepath.Join(t.TempDir(), "private"))
	if e != nil {
		t.Fatal(e)
	}
	records := []localstate.RelayHandoff{{Server: server.URL, UserID: "current-user", Runtime: "different-runtime", NativeSession: "foreign-store", ClientID: "foreign-message"}, {Server: server.URL, UserID: "other-user", NativeSession: "old-thread", ClientID: "old-message"}, {Server: "https://other.example.invalid", UserID: "current-user", NativeSession: "other-thread", ClientID: "other-message"}, {Server: server.URL, UserID: "current-user", Runtime: "other-local-runtime", RuntimeKind: "deepseek-harness", RuntimeInstance: "desktop", NativeSession: "dsh-session", ClientID: "dsh-message"}}
	if e = store.AddRelayHandoffs(records); e != nil {
		t.Fatal(e)
	}
	r := &remote{client: &api.Client{Client: server.Client()}, store: store, config: localstate.Config{Server: server.URL}, token: []byte("synthetic")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	open := func(context.Context, localstate.RelayHandoff) (relay.Session, error) {
		t.Fatal("foreign native session was accessed")
		return nil, nil
	}
	if err := finishRelayHandoffs(ctx, r, nil, open); err != nil {
		t.Fatal("foreign handoff blocks new binding", err)
	}
	after, e := store.RelayHandoffs()
	if e != nil || len(after) != 4 {
		t.Fatal("foreign receipt evidence was removed")
	}
}

func TestHandoffPreservesAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		io.WriteString(w, `{"code":"AUTH_REQUIRED","message":"Login required","retry_action":"user_action"}`)
	}))
	defer server.Close()
	store, e := localstate.Open(filepath.Join(t.TempDir(), "private"))
	if e != nil {
		t.Fatal(e)
	}
	r := &remote{client: &api.Client{Client: server.Client()}, store: store, config: localstate.Config{Server: server.URL}, token: []byte("revoked-synthetic")}
	if err := store.AddRelayHandoffs([]localstate.RelayHandoff{{Server: server.URL, UserID: "owner"}}); err != nil {
		t.Fatal(err)
	}
	err := finishRelayHandoffs(context.Background(), r, nil, nil)
	var f *failure
	if !errors.As(err, &f) || f.exit != exitAuth {
		t.Fatalf("auth failure lost: %v", err)
	}
}

func TestOversizedRelayAnswerReportsTerminalOutcome(t *testing.T) {
	var submitted struct {
		Outcome string `json:"outcome"`
		Text    string `json:"text"`
		Summary string `json:"summary"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/session-relay/messages/message/result" {
			t.Error("unexpected request", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
			t.Error(err)
		}
		io.WriteString(w, `{"accepted":true}`)
	}))
	defer server.Close()
	reader, send := io.Pipe()
	requests, writer := io.Pipe()
	rpc := relay.NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
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
		if request.Method != "thread/turns/list" {
			t.Error("unexpected Codex operation", request.Method)
			return
		}
		result := map[string]any{"data": []any{map[string]any{"id": "turn", "status": "completed", "completedAt": time.Now().Unix(), "itemsView": "full", "items": []any{map[string]any{"type": "userMessage", "clientId": "message"}, map[string]any{"type": "agentMessage", "phase": "final_answer", "text": strings.Repeat("中", 262145)}}}}, "nextCursor": nil}
		json.NewEncoder(send).Encode(map[string]any{"id": request.ID, "result": result})
		io.Copy(io.Discard, requests)
	}()
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}, token: []byte("synthetic")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	processRelayMessage(ctx, r, &relay.Codex{RPC: rpc}, sessionView{NativeSession: "thread"}, sessionMessage{ID: "message", Generation: 1, Kind: "chat", State: "dispatching", CreatedAt: time.Now()}, &relayMessageRecovery{})
	if submitted.Outcome != "output_too_large" || submitted.Text != "" || submitted.Summary != "" {
		t.Fatalf("oversized answer not settled safely: %+v", submitted)
	}
}

func TestEmptyRelayAnswerReportsTerminalOutcome(t *testing.T) {
	for _, answer := range []string{"no_final_message", "", " \n\t"} {
		t.Run(answer, func(t *testing.T) {
			var submitted struct {
				Outcome string `json:"outcome"`
				Text    string `json:"text"`
				Summary string `json:"summary"`
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/session-relay/messages/message/result" {
					t.Error("unexpected request", r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&submitted); err != nil {
					t.Error(err)
				}
				io.WriteString(w, `{"accepted":true}`)
			}))
			defer server.Close()
			reader, send := io.Pipe()
			requests, writer := io.Pipe()
			rpc := relay.NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
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
				if request.Method != "thread/turns/list" {
					t.Error("unexpected Codex operation", request.Method)
					return
				}
				items := []any{map[string]any{"type": "userMessage", "clientId": "message"}}
				if answer != "no_final_message" {
					items = append(items, map[string]any{"type": "agentMessage", "phase": "final_answer", "text": answer})
				}
				result := map[string]any{"data": []any{map[string]any{"id": "turn", "status": "completed", "completedAt": time.Now().Unix(), "itemsView": "full", "items": items}}, "nextCursor": nil}
				json.NewEncoder(send).Encode(map[string]any{"id": request.ID, "result": result})
				io.Copy(io.Discard, requests)
			}()
			r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}, token: []byte("synthetic")}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			processRelayMessage(ctx, r, &relay.Codex{RPC: rpc}, sessionView{NativeSession: "thread"}, sessionMessage{ID: "message", Generation: 1, Kind: "chat", State: "dispatching", CreatedAt: time.Now()}, &relayMessageRecovery{})
			if submitted.Outcome != "empty_output" || submitted.Text != "" || submitted.Summary != "" {
				t.Fatalf("empty answer not settled safely: %+v", submitted)
			}
		})
	}
}

func TestWaitRelayReadyRejectsFailedOrReplacedBinding(t *testing.T) {
	for _, status := range []string{"ready", "unavailable", "replaced"} {
		t.Run(status, func(t *testing.T) {
			expected := sessionView{Generation: 1, NativeSession: "thread", Desired: true, Online: true}
			current := expected
			current.Status = status
			if status == "replaced" {
				current.Generation++
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(current) }))
			defer server.Close()
			r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := waitRelayReady(ctx, r, expected)
			if (err == nil) != (status == "ready") {
				t.Fatalf("incorrect readiness for %s: %v", status, err)
			}
		})
	}
}

func testRelayIdentity(t *testing.T, r *remote, binary string) string {
	t.Helper()
	identity, err := relayIdentity(r, binary)
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestRelayBinaryChangeDoesNotReuseOldBridge(t *testing.T) {
	r := &remote{config: localstate.Config{Server: "https://synthetic.invalid"}, token: []byte("synthetic")}
	binary := filepath.Join(t.TempDir(), "remote-agent-session")
	if err := os.WriteFile(binary, []byte("first build"), 0700); err != nil {
		t.Fatal(err)
	}
	original := testRelayIdentity(t, r, binary)
	if err := os.WriteFile(binary, []byte("other build"), 0700); err != nil {
		t.Fatal(err)
	}
	updated := testRelayIdentity(t, r, binary)
	if original == updated || strings.Split(original, ":")[0] != strings.Split(updated, ":")[0] {
		t.Fatal("binary replacement must hand over work without revoking the login")
	}
}

func TestRelayRuntimeChangeDoesNotReuseOldBridge(t *testing.T) {
	r := &remote{config: localstate.Config{Server: "https://synthetic.invalid"}, token: []byte("synthetic")}
	t.Setenv("CODEX_HOME", "/synthetic/codex-one")
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "")
	original := testRelayIdentity(t, r, os.Args[0])
	t.Setenv("CODEX_HOME", "/synthetic/codex-two")
	if testRelayIdentity(t, r, os.Args[0]) == original {
		t.Fatal("different history store reused old bridge")
	}
	second := testRelayIdentity(t, r, os.Args[0])
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "/synthetic/private/executor.sock")
	if testRelayIdentity(t, r, os.Args[0]) == second {
		t.Fatal("different original executor reused old bridge")
	}
}

func TestRuntimeReplacementDoesNotRequestRevocation(t *testing.T) {
	store, err := localstate.Open(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	config := localstate.Config{Server: "https://synthetic.invalid"}
	if err = store.SaveConfig(config); err != nil {
		t.Fatal(err)
	}
	r := &remote{config: config, token: []byte("synthetic"), store: store}
	identity := testRelayIdentity(t, r, os.Args[0])
	if err = store.SaveRelayIntent(strings.Split(identity, ":")[0] + ":new-runtime"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	called := false
	watchRelayIntent(ctx, r, identity, func(revoke bool) {
		called = true
		if revoke {
			t.Error("runtime-only change revokes in-flight work")
		}
	})
	if !called {
		t.Fatal("runtime switch not observed")
	}
}

func TestSessionStatusDisplaysServerGuidanceWithoutMachineState(t *testing.T) {
	dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
		jsonReply(w, 200, map[string]any{"status": "unavailable", "device_name": "电脑", "project": "项目", "session_name": "会话", "guidance": "尚未连接，请在本地检查后重新连接。"})
	})
	var out, stderr bytes.Buffer
	code := Run([]string{"--state-dir", dir, "session", "status"}, &out, &stderr)
	if code != 0 || !strings.Contains(out.String(), "尚未连接，请在本地检查后重新连接。") || strings.Contains(out.String(), "unavailable") {
		t.Fatalf("machine status leaked: %d %q %q", code, out.String(), stderr.String())
	}
}

func TestSessionAPIErrorsKeepTrustedActionGuidance(t *testing.T) {
	for _, test := range []struct {
		code     string
		status   int
		expected string
		notify   bool
	}{
		{"SESSION_NOT_ATTACHED", 404, "把当前会话连到飞书", false},
		{"SESSION_OTHER_DEVICE", 403, "当前登录无法操作", false},
		{"SESSION_REQUEST_CONFLICT", 409, "原编号和原内容", false},
		{"SESSION_FUTURE_ERROR", 409, "先在原会话核对", false},
		{"CLI_TRANSPORT", 0, "不要重复提交任务", false},
	} {
		t.Run(test.code, func(t *testing.T) {
			dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
				if test.notify && (r.Method != "POST" || r.URL.Path != "/v1/session-relay/notify") {
					t.Errorf("wrong request %s %s", r.Method, r.URL.Path)
				}
				if test.status == 0 {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					conn.Close()
					return
				}
				jsonReply(w, test.status, api.ErrorBody{Code: test.code, Message: "UNTRUSTED_TEST_MESSAGE", RetryAction: api.SameRequest})
			})
			args := []string{"session", "status"}
			if test.notify {
				file := filepath.Join(t.TempDir(), "answer.txt")
				if err := os.WriteFile(file, []byte("answer"), 0600); err != nil {
					t.Fatal(err)
				}
				args = []string{"session", "notify", "--file", file, "--name", "test", "--session", testID, "--request", testID}
			}
			var out, stderr bytes.Buffer
			exit := Run(append([]string{"--state-dir", dir}, args...), &out, &stderr)
			if exit == 0 || !strings.Contains(stderr.String(), test.expected) || strings.Contains(stderr.String(), "UNTRUSTED_TEST_MESSAGE") {
				t.Fatalf("unsafe/missing guidance: %d %q", exit, stderr.String())
			}
			code, value := runJSON(t, dir, args...)
			var result struct {
				Error struct {
					Code        string `json:"code"`
					RetryAction string `json:"retry_action"`
				} `json:"error"`
			}
			if json.Unmarshal([]byte(value), &result) != nil || code == 0 || result.Error.Code != test.code || result.Error.RetryAction != "same_request" {
				t.Fatalf("lost stable error: %d %s", code, value)
			}
		})
	}
}

func TestRelayDispatchAndHistoryFailures(t *testing.T) {
	for _, scenario := range []struct {
		name, initial, expected string
	}{
		{"start rejected after interrupt", "pending", "not_submitted"},
		{"binding check unavailable after interrupt", "pending", "not_submitted"},
		{"start receipt lost but history confirms execution", "pending", "completed"},
		{"history unavailable", "dispatching", "unknown"},
		{"healthy long turn", "dispatching", "working"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			view := sessionView{NativeSession: "thread", Desired: true, Online: true, Status: "ready", Generation: 1}
			result := make(chan string, 2)
			bindingReads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.URL.Path {
				case "/v1/session-relay":
					bindingReads++
					if scenario.name == "binding check unavailable after interrupt" && bindingReads == 2 {
						jsonReply(w, 503, map[string]string{"code": "UNAVAILABLE"})
						return
					}
					json.NewEncoder(w).Encode(view)
				case "/v1/session-relay/messages/message/dispatch":
					io.WriteString(w, `{"dispatch":true}`)
				case "/v1/session-relay/messages/message/working":
					result <- "working"
					io.WriteString(w, `{"accepted":true}`)
				case "/v1/session-relay/messages/message/result":
					var body struct{ Outcome string }
					if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					result <- body.Outcome
					io.WriteString(w, `{"accepted":true}`)
				default:
					t.Error("unexpected server operation", req.URL.Path)
				}
			}))
			defer server.Close()
			r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
			incoming, sender := io.Pipe()
			requests, outgoing := io.Pipe()
			rpc := relay.NewRPC(incoming, outgoing, func() { incoming.Close(); outgoing.Close() })
			defer rpc.Close()
			sequence := make(chan []string, 1)
			go func() {
				defer sender.Close()
				defer requests.Close()
				decode, encode := json.NewDecoder(requests), json.NewEncoder(sender)
				var calls []string
				defer func() { sequence <- calls }()
				for {
					var call struct {
						ID     int64
						Method string
					}
					if decode.Decode(&call) != nil {
						return
					}
					calls = append(calls, call.Method)
					reply := map[string]any{"id": call.ID, "result": map[string]any{}}
					switch call.Method {
					case "thread/read":
						reply["result"] = map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/synthetic", "status": map[string]string{"type": "active"}}}
					case "thread/turns/list":
						if len(calls) > 2 && scenario.expected == "completed" {
							reply["result"] = map[string]any{"data": []any{map[string]any{
								"id": "new", "status": "completed", "itemsView": "full", "completedAt": time.Now().Unix(),
								"items": []any{
									map[string]string{"type": "userMessage", "clientId": "message"},
									map[string]string{"type": "agentMessage", "phase": "final_answer", "text": "完成"},
								},
							}}}
						} else if scenario.expected == "working" {
							reply["result"] = map[string]any{"data": []any{map[string]any{
								"id": "long", "status": "inProgress", "itemsView": "full",
								"items": []any{map[string]string{"type": "userMessage", "clientId": "message"}},
							}}}
						} else if scenario.initial == "pending" {
							reply["result"] = map[string]any{"data": []any{map[string]string{"id": "old", "status": "inProgress"}}}
						} else {
							reply["error"] = map[string]any{"code": -32602}
						}
					case "turn/interrupt":
					case "turn/start":
						if scenario.name == "binding check unavailable after interrupt" {
							t.Error("attempted submission after failed binding check")
						}
						code := -32602
						if scenario.expected == "completed" {
							code = -32603
						}
						reply["error"] = map[string]any{"code": code}
					default:
						t.Error("unexpected Codex operation", call.Method)
					}
					if encode.Encode(reply) != nil {
						return
					}
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			dispatched := time.Now().Add(-time.Minute)
			message := sessionMessage{
				ID: "message", Generation: 1, State: scenario.initial, CreatedAt: dispatched, DispatchedAt: &dispatched,
			}
			recovery := &relayMessageRecovery{}
			if scenario.expected == "unknown" {
				processRelayMessage(ctx, r, &relay.Codex{RPC: rpc}, view, message, recovery)
				if len(result) != 0 || recovery.observationFailedSince.IsZero() {
					t.Fatal("transient history failure was not given time to recover")
				}
				recovery.observationFailedSince = dispatched
			}
			if scenario.expected == "working" {
				recovery.observationFailedSince = dispatched
			}
			processRelayMessage(ctx, r, &relay.Codex{RPC: rpc}, view, message, recovery)
			rpc.Close()
			calls := <-sequence
			if scenario.expected == "completed" && strings.Count(strings.Join(calls, " "), "turn/start") != 1 {
				t.Fatalf("ambiguous submission should be reconciled without replay: %v", calls)
			}
			select {
			case got := <-result:
				if got != scenario.expected {
					t.Fatalf("got %q, want %q after %v", got, scenario.expected, calls)
				}
			default:
				t.Fatalf("no terminal result after %v", calls)
			}
		})
	}
}

func TestRelayHandoffsKeepUnrequestedAndContinuePastErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/v1/session-relay":
			jsonReply(w, 200, map[string]string{"user_id": "owner"})
		case "/v1/session-relay/messages/active/cancellation":
			jsonReply(w, 200, map[string]bool{"requested": false, "settled": false})
		case "/v1/session-relay/messages/failed/cancellation":
			jsonReply(w, 200, map[string]bool{"requested": true, "settled": false})
		case "/v1/session-relay/messages/settled/cancellation":
			jsonReply(w, 200, map[string]bool{"requested": false, "settled": true})
		default:
			t.Error("unexpected operation", req.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	store, err := localstate.Open(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	var items []localstate.RelayHandoff
	for _, id := range []string{"active", "failed", "settled"} {
		items = append(items, localstate.RelayHandoff{
			Server: server.URL, UserID: "owner", Runtime: relayRuntimeIdentity(), NativeSession: id, ClientID: id,
		})
	}
	if err := store.AddRelayHandoffs(items); err != nil {
		t.Fatal(err)
	}
	r := &remote{client: &api.Client{Client: server.Client()}, store: store, config: localstate.Config{Server: server.URL}}
	var opened []string
	open := func(_ context.Context, item localstate.RelayHandoff) (relay.Session, error) {
		opened = append(opened, item.NativeSession)
		return nil, errors.New("original executor unavailable")
	}
	if err := finishRelayHandoffs(context.Background(), r, nil, open); err != nil {
		t.Fatal(err)
	}
	pending, err := store.RelayHandoffs()
	if err != nil || len(pending) != 2 || pending[0] != items[0] || pending[1] != items[1] || strings.Join(opened, ",") != "failed" {
		t.Fatalf("pending %v, original executors inspected %v, error %v", pending, opened, err)
	}
}

func TestRetiringRelayRetainsUnrequestedInputWithoutTouchingNativeQueue(t *testing.T) {
	store, err := localstate.Open(filepath.Join(t.TempDir(), "private"))
	if err != nil {
		t.Fatal(err)
	}
	r := &remote{store: store, config: localstate.Config{Server: "https://synthetic.invalid"}}
	view := sessionView{UserID: "owner", NativeSession: "thread", Messages: []sessionMessage{{ID: "active", State: "dispatching"}}}
	if !retireRelay(r, nil, view) {
		t.Fatal("unconfirmed input was not recorded for next login")
	}
	items, err := store.RelayHandoffs()
	if err != nil || len(items) != 1 || items[0].ClientID != "active" {
		t.Fatalf("wrong retained records: %v, error %v", items, err)
	}
}

func TestUnavailableRuntimeFallbackGuidanceRefersToOriginalAgent(t *testing.T) {
	for _, runtime := range []string{"codex", "deepseek-harness"} {
		t.Run(runtime, func(t *testing.T) {
			view := sessionView{RuntimeKind: runtime, NativeSession: "original-session", Generation: 4, Desired: true, Online: true, Status: "unavailable"}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				jsonReply(w, 200, view)
			}))
			defer server.Close()
			r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
			_, err := waitRelayReady(context.Background(), r, view)
			if err == nil || !strings.Contains(err.Error(), "原 Agent") || strings.Contains(err.Error(), "Codex") || !strings.Contains(err.Error(), "重新连接") {
				t.Fatal("unavailable fallback names the wrong host", err)
			}
		})
	}
	message := relayErrorMessage("SESSION_NOT_ATTACHED")
	if !strings.Contains(message, "本地 Agent") || strings.Contains(message, "Codex") {
		t.Fatal("unattached fallback names a specific host", message)
	}
}

func TestFreshControllerReadinessRejectsStaleOnlineStatus(t *testing.T) {
	view := sessionView{Generation: 8, NativeSession: "original", RuntimeKind: "codex", Desired: true, Online: true, Status: "ready"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { jsonReply(w, 200, view) }))
	defer server.Close()
	store, err := localstate.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	release, err := store.RelayLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	store.SaveRelayOwner("old-controller")
	store.SaveRelayIntent("old-controller")
	if err := store.SaveRelayReadiness(localstate.RelayReadiness{Controller: "old-controller", Generation: 8, NativeSession: "original", RuntimeKind: "codex"}); err != nil {
		t.Fatal(err)
	}
	r := &remote{client: &api.Client{Client: server.Client()}, store: store, config: localstate.Config{Server: server.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	if _, err := waitRelayReady(ctx, r, view, "new-controller"); err == nil {
		t.Fatal("stale online server and terminating owner confirmed successor")
	}
	cancel()
	store.SaveRelayIntent("new-controller")
	store.SaveRelayOwner("new-controller")
	if err := store.SaveRelayReadiness(localstate.RelayReadiness{Controller: "old-controller", Generation: 8, NativeSession: "original", RuntimeKind: "codex"}); err == nil {
		t.Fatal("retiring owner overwrote successor witness")
	}
	if err := store.SaveRelayReadiness(localstate.RelayReadiness{Controller: "new-controller", Generation: 8, NativeSession: "original", RuntimeKind: "codex"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := waitRelayReady(ctx, r, view, "new-controller"); err != nil {
		t.Fatal("verified successor was not accepted", err)
	}
}

func TestRelayLaunchContextSeparatesDSHAndCodexWithoutExtraSocket(t *testing.T) {
	r := &remote{config: localstate.Config{Server: "http://127.0.0.1:18481"}, token: []byte("synthetic")}
	t.Setenv("CODEX_HOME", "/synthetic/codex")
	t.Setenv("REMOTE_AGENT_SESSION_CODEX_SOCKET", "")
	t.Setenv("DSH_SESSION_ID", "native-dsh")
	t.Setenv("DSH_HOME", "/synthetic/dsh")
	dsh := testRelayIdentity(t, r, os.Args[0])
	t.Setenv("DSH_SESSION_ID", "")
	codex := testRelayIdentity(t, r, os.Args[0])
	if dsh == codex || strings.Split(dsh, ":")[0] != strings.Split(codex, ":")[0] {
		t.Fatal("native launch context was reused or incorrectly revoked login")
	}
	t.Setenv("DSH_SESSION_ID", "native-dsh")
	t.Setenv("DSH_HOME", "/synthetic/other-profile")
	if testRelayIdentity(t, r, os.Args[0]) == dsh {
		t.Fatal("different DSH host context reused executor permissions")
	}
}
