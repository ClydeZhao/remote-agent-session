package relay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func asyncQuestionFixture(t *testing.T) questionHistoryTurn {
	t.Helper()
	var turn questionHistoryTurn
	err := json.Unmarshal([]byte(`{"id":"turn","status":"inProgress","itemsView":"full","items":[{"id":"input","type":"userMessage","clientId":"remote"},{"id":"call_1","type":"agentMessage","delivery":"async","questions":[{"title":"Choose?","options":["A","B"]}]}]}`), &turn)
	if err != nil {
		t.Fatal(err)
	}
	return turn
}
func TestAsyncQuestionUsesNativeAgentMessageIdentity(t *testing.T) {
	turn := asyncQuestionFixture(t)
	got, err := pendingHistoryQuestions([]questionHistoryTurn{turn}, "thread", "owner")
	if err != nil || len(got) != 1 || got[0].Questions[0].ID != `["request_user_input_async","call_1",0]` {
		t.Fatalf("%v %v", got, err)
	}
	q := got[0]
	q.ReplyID = "reply"
	var response NativeQuestionResponse
	json.Unmarshal([]byte(`{"answers":{"[\"request_user_input_async\",\"call_1\",0]":{"answers":["B"]}}}`), &response)
	text, err := questionAnswerText(q, response)
	if err != nil {
		t.Fatal(err)
	}
	turn.Items = append(turn.Items, questionHistoryItem{Type: "userMessage", Content: []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{Type: "text", Text: text}}})
	got, err = pendingHistoryQuestions([]questionHistoryTurn{turn}, "thread", "owner")
	if err != nil || len(got) != 0 {
		t.Fatal("answered question remained pending")
	}
}
func TestAsyncAnswerSteersWithoutInterruptAndVerifyOnlyDoesNotResubmit(t *testing.T) {
	for _, status := range []string{"inProgress", "interrupted"} {
		t.Run(status, func(t *testing.T) { testAsyncAnswerSteers(t, status) })
	}
}
func testAsyncAnswerSteers(t *testing.T, status string) {
	client, server := net.Pipe()
	defer server.Close()
	c := &Codex{RPC: NewRPC(client, client, func() { client.Close() })}
	defer c.RPC.Close()
	turn := asyncQuestionFixture(t)
	turn.Status = status
	done := make(chan error, 1)
	steers := 0
	go func() {
		defer server.Close()
		decoder := json.NewDecoder(server)
		encoder := json.NewEncoder(server)
		for {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if decoder.Decode(&req) != nil {
				done <- nil
				return
			}
			var result any
			switch req.Method {
			case "thread/read":
				result = map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/test", "status": map[string]string{"type": "active"}}}
			case "thread/turns/list":
				result = map[string]any{"data": []any{turn}}
			case "turn/steer":
				steers++
				var params struct {
					Turn     string `json:"expectedTurnId"`
					ClientID string `json:"clientUserMessageId"`
					Input    []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"input"`
				}
				json.Unmarshal(req.Params, &params)
				if params.Turn != "turn" || params.ClientID != "reply" {
					done <- fmt.Errorf("lost identity")
					return
				}
				turn.Items = append(turn.Items, questionHistoryItem{Type: "userMessage", ClientID: params.ClientID, Content: params.Input})
				result = map[string]any{}
			default:
				done <- fmt.Errorf("unexpected mutation %s", req.Method)
				return
			}
			encoder.Encode(map[string]any{"id": req.ID, "result": result})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pending, err := c.PendingQuestions(ctx, "thread")
	if err != nil || len(pending) != 1 {
		t.Fatalf("%v %v", pending, err)
	}
	ids, err := c.OriginIDsForQuestion(ctx, "thread", "turn")
	if err != nil || len(ids) != 1 || ids[0] != "remote" {
		t.Fatalf("active question origin lost: %v %v", ids, err)
	}
	q := pending[0]
	q.ReplyID = "reply"
	var response NativeQuestionResponse
	json.Unmarshal([]byte(`{"answers":{"[\"request_user_input_async\",\"call_1\",0]":{"answers":["B"]}}}`), &response)
	q.VerifyOnly = true
	if err := c.AnswerQuestion(ctx, q, response, func() error { return nil }); err != ErrAnswerUnconfirmed {
		t.Fatalf("verify-only %v", err)
	}
	q.VerifyOnly = false
	changed := q
	changed.Owner = "other-owner"
	if err := c.AnswerQuestion(ctx, changed, response, func() error { return nil }); err == nil {
		t.Fatal("accepted replaced owner")
	}
	if err := c.AnswerQuestion(ctx, q, response, func() error { return fmt.Errorf("binding changed") }); err == nil {
		t.Fatal("ignored authorization failure")
	}
	if err := c.AnswerQuestion(ctx, q, response, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	q.VerifyOnly = true
	if err := c.AnswerQuestion(ctx, q, response, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	c.RPC.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if steers != 1 {
		t.Fatalf("steered %d times", steers)
	}
}

func TestAsyncQuestionSurvivesAnotherQuestionAnswerButNotNewTask(t *testing.T) {
	turn := asyncQuestionFixture(t)
	another := turn.Items[1]
	another.ID = "call_2"
	turn.Items = append(turn.Items, another)
	pending, err := pendingHistoryQuestions([]questionHistoryTurn{turn}, "thread", "owner")
	if err != nil || len(pending) != 2 {
		t.Fatal("missing questions")
	}
	q := pending[0]
	q.ReplyID = "reply"
	response := NativeQuestionResponse{Answers: map[string]struct {
		Answers []string `json:"answers"`
	}{q.Questions[0].ID: {Answers: []string{"B"}}}}
	text, err := questionAnswerText(q, response)
	if err != nil {
		t.Fatal(err)
	}
	next := questionHistoryTurn{ID: "answer-turn", Status: "inProgress", ItemsView: "full", Items: []questionHistoryItem{{Type: "userMessage", ClientID: "reply", Content: []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{{Type: "text", Text: text}}}}}
	pending, err = pendingHistoryQuestions([]questionHistoryTurn{next, turn}, "thread", "owner")
	if err != nil || len(pending) != 1 || pending[0].ItemID != "call_2" {
		t.Fatal("answer incorrectly expired another question")
	}
	next.Items[0].Content[0].Text = "New task"
	pending, err = pendingHistoryQuestions([]questionHistoryTurn{next, turn}, "thread", "owner")
	if err != nil || len(pending) != 0 {
		t.Fatal("new task retained old question")
	}
}

func TestAsyncDesktopAnswerUsesOwnerSteerWithoutInterrupt(t *testing.T) {
	for _, status := range []string{"inProgress", "interrupted"} {
		t.Run(status, func(t *testing.T) { testAsyncDesktopAnswerSteers(t, status) })
	}
}
func testAsyncDesktopAnswerSteers(t *testing.T, status string) {
	home, err := os.MkdirTemp("/tmp", "bb-async-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("CODEX_HOME", home)
	os.Mkdir(filepath.Join(home, "ipc"), 0700)
	listener, err := net.Listen("unix", filepath.Join(home, "ipc/ipc.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	var mu sync.Mutex
	turn := asyncQuestionFixture(t)
	turn.Status = status
	writes := 0
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				for {
					var size uint32
					if binary.Read(conn, binary.LittleEndian, &size) != nil {
						return
					}
					data := make([]byte, size)
					if _, err := io.ReadFull(conn, data); err != nil {
						return
					}
					var req struct {
						ID      string          `json:"requestId"`
						Method  string          `json:"method"`
						Target  string          `json:"targetClientId"`
						Version int             `json:"version"`
						Params  json.RawMessage `json:"params"`
					}
					json.Unmarshal(data, &req)
					result := map[string]any{"clientId": "relay"}
					switch req.Method {
					case "initialize", "thread-owner-discovery":
					case "thread-follower-steer-turn":
						if req.Target != "owner" || req.Version != 1 {
							t.Error("wrong owner or protocol")
						}
						var params struct {
							ClientID       string `json:"clientUserMessageId"`
							RestoreMessage *struct {
								ID      string    `json:"id"`
								Text    string    `json:"text"`
								Cwd     string    `json:"cwd"`
								Context *struct{} `json:"context"`
							} `json:"restoreMessage"`
							Input []struct {
								Type string `json:"type"`
								Text string `json:"text"`
							} `json:"input"`
						}
						json.Unmarshal(req.Params, &params)
						if params.RestoreMessage == nil || params.RestoreMessage.Context == nil || params.RestoreMessage.ID != params.ClientID || params.RestoreMessage.Cwd == "" || len(params.Input) != 1 || params.RestoreMessage.Text != params.Input[0].Text {
							t.Error("missing Desktop restoreMessage context")
						}
						mu.Lock()
						writes++
						turn.Items = append(turn.Items, questionHistoryItem{Type: "userMessage", ClientID: params.ClientID, Content: params.Input})
						mu.Unlock()
					default:
						t.Errorf("unexpected operation %s", req.Method)
					}
					response, _ := json.Marshal(map[string]any{"type": "response", "requestId": req.ID, "resultType": "success", "handledByClientId": "owner", "result": result})
					binary.Write(conn, binary.LittleEndian, uint32(len(response)))
					conn.Write(response)
				}
			}()
		}
	}()
	client, server := net.Pipe()
	defer server.Close()
	c := &Codex{RPC: NewRPC(client, client, func() { client.Close() })}
	defer c.RPC.Close()
	go func() {
		decoder := json.NewDecoder(server)
		encoder := json.NewEncoder(server)
		for {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if decoder.Decode(&req) != nil {
				return
			}
			var result any
			switch req.Method {
			case "thread/read":
				result = map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/test", "status": map[string]string{"type": "notLoaded"}}}
			case "thread/turns/list":
				mu.Lock()
				snapshot, _ := json.Marshal(turn)
				mu.Unlock()
				result = map[string]any{"data": []json.RawMessage{snapshot}}
			default:
				t.Errorf("sidecar mutation %s", req.Method)
				return
			}
			encoder.Encode(map[string]any{"id": req.ID, "result": result})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	pending, err := c.PendingQuestions(ctx, "thread")
	if err != nil || len(pending) != 1 {
		t.Fatalf("%v %v", pending, err)
	}
	q := pending[0]
	q.ReplyID = "reply"
	response := NativeQuestionResponse{Answers: map[string]struct {
		Answers []string `json:"answers"`
	}{q.Questions[0].ID: {Answers: []string{"B"}}}}
	if err := c.AnswerQuestion(ctx, q, response, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	count := writes
	mu.Unlock()
	if count != 1 {
		t.Fatalf("writes %d", count)
	}
}

func TestAsyncInterruptedQuestionNeedsCompletionEvidence(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprint(completed), func(t *testing.T) {
			turn := asyncQuestionFixture(t)
			stamp := "null"
			if completed {
				stamp = "123"
			}
			if err := json.Unmarshal([]byte(`{"status":"interrupted","completedAt":`+stamp+`}`), &turn); err != nil {
				t.Fatal(err)
			}
			questions, err := pendingHistoryQuestions([]questionHistoryTurn{turn}, "thread", "owner")
			expected := 1
			if completed {
				expected = 0
			}
			if err != nil || len(questions) != expected {
				t.Fatalf("completion=%v questions=%v error=%v", completed, questions, err)
			}
		})
	}
}

func TestAsyncQuestionAnchorsIdleAnswerInputAndChecksLaterReplies(t *testing.T) {
	turn := asyncQuestionFixture(t)
	turn.Items[0].ClientID = "reply"
	turn.Items[0].Content = []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}{
		{Type: "text", Text: `<send_user_message_question_reply>[{"questionItemId":"previous","answer":"A"}]</send_user_message_question_reply>`},
	}
	later := turn.Items[0]
	later.ClientID = "later-reply"
	turn.Items = append(turn.Items, later)
	client, server := net.Pipe()
	defer server.Close()
	c := &Codex{RPC: NewRPC(client, client, func() { client.Close() })}
	defer c.RPC.Close()
	go func() {
		decoder := json.NewDecoder(server)
		encoder := json.NewEncoder(server)
		for {
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
			}
			if decoder.Decode(&req) != nil {
				return
			}
			if req.Method != "thread/turns/list" {
				t.Error("unexpected mutation", req.Method)
				return
			}
			encoder.Encode(map[string]any{"id": req.ID, "result": map[string]any{"data": []questionHistoryTurn{turn}}})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	anchor, err := c.OriginIDsForQuestion(ctx, "thread", "turn")
	if err != nil || len(anchor) != 1 || anchor[0] != "reply" {
		t.Fatalf("answer-triggered question lost stable origin: %v %v", anchor, err)
	}
	all, err := c.AllOriginIDsForQuestion(ctx, "thread", "turn")
	if err != nil || len(all) != 2 || all[0] != "reply" || all[1] != "later-reply" {
		t.Fatalf("full authorization inputs missing: %v %v", all, err)
	}
}
