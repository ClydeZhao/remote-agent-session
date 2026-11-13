//go:build !windows

package relay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopInterruptPinsOwnerAndExpectedTurn(t *testing.T) {
	for _, scenario := range []string{"active", "normalized_active", "completed", "unavailable", "wrong_turn", "revoked", "empty_receipt"} {
		t.Run(scenario, func(t *testing.T) {
			home, err := os.MkdirTemp("/tmp", "bb-ipc-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(home)
			t.Setenv("CODEX_HOME", home)
			dir := filepath.Join(home, "ipc")
			if err = os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("unix", filepath.Join(dir, "ipc.sock"))
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			calls := make(chan string, 4)
			go func() {
				conn, e := listener.Accept()
				if e != nil {
					return
				}
				defer conn.Close()
				for {
					header := make([]byte, 4)
					if _, e = io.ReadFull(conn, header); e != nil {
						return
					}
					body := make([]byte, binary.LittleEndian.Uint32(header))
					if _, e = io.ReadFull(conn, body); e != nil {
						return
					}
					var req struct {
						ID      string         `json:"requestId"`
						Method  string         `json:"method"`
						Version int            `json:"version"`
						Target  string         `json:"targetClientId"`
						Params  map[string]any `json:"params"`
					}
					if json.Unmarshal(body, &req) != nil {
						return
					}
					calls <- req.Method
					result := map[string]any{"clientId": "relay"}
					response := map[string]any{"type": "response", "requestId": req.ID, "resultType": "success", "handledByClientId": "owner", "result": result}
					if req.Method == "thread-owner-discovery" {
						if req.Params["conversationId"] != "thread" || req.Version != 1 {
							t.Error("wrong owner discovery")
						}
						if scenario == "unavailable" {
							response["resultType"] = "error"
						}
					}
					if req.Method == "thread-follower-interrupt-turn" {
						if req.Version != 4 || req.Target != "owner" || req.Params["expectedTurnId"] != "active-turn" || req.Params["mode"] != "system" {
							t.Error("interrupt lost scope")
						}
						id := "active-turn"
						if scenario == "wrong_turn" {
							id = "other-turn"
						}
						response["result"] = map[string]any{"ok": true, "interruptedTurnId": id}
						if scenario == "empty_receipt" {
							response["result"] = map[string]any{"ok": true, "interruptedTurnId": nil}
						}
					}
					data, _ := json.Marshal(response)
					frame := make([]byte, 4+len(data))
					binary.LittleEndian.PutUint32(frame, uint32(len(data)))
					copy(frame[4:], data)
					if _, e = conn.Write(frame); e != nil {
						return
					}
				}
			}()
			reader, send := io.Pipe()
			requests, writer := io.Pipe()
			rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
			defer rpc.Close()
			go func() {
				defer send.Close()
				defer requests.Close()
				decoder := json.NewDecoder(requests)
				for {
					var req struct {
						ID     int64  `json:"id"`
						Method string `json:"method"`
					}
					if decoder.Decode(&req) != nil {
						return
					}
					var result any
					switch req.Method {
					case "thread/turns/list":
						status := "inProgress"
						if scenario == "normalized_active" {
							status = "interrupted"
						}
						if scenario == "completed" {
							status = "completed"
						}
						result = map[string]any{"data": []Turn{{ID: "active-turn", Status: status}}}
					case "thread/read":
						result = map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/test", "status": map[string]string{"type": "notLoaded"}}}
					default:
						t.Error("unexpected executor mutation", req.Method)
						return
					}
					json.NewEncoder(send).Encode(map[string]any{"id": req.ID, "result": result})
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			err = (&Codex{RPC: rpc}).InterruptActive(ctx, "thread", func() error {
				if scenario == "revoked" {
					return ErrInterruptUnavailable
				}
				return nil
			})
			if (err != nil) != (scenario == "unavailable" || scenario == "wrong_turn" || scenario == "revoked" || scenario == "empty_receipt") {
				t.Fatal("unexpected result", err)
			}
			expected := 3
			if scenario == "completed" {
				expected = 0
			}
			if scenario == "unavailable" || scenario == "revoked" {
				expected = 2
			}
			if len(calls) != expected {
				t.Fatalf("expected %d control calls, got %d", expected, len(calls))
			}
		})
	}
}

func TestUnloadedNativeCLINeverFallsBackToQueue(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	reader, send := io.Pipe()
	requests, writer := io.Pipe()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	go func() {
		defer send.Close()
		defer requests.Close()
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		decoder := json.NewDecoder(requests)
		if decoder.Decode(&req) != nil {
			return
		}
		if req.Method != "thread/read" {
			t.Error("unexpected call", req.Method)
		}
		json.NewEncoder(send).Encode(map[string]any{"id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/test", "source": "cli", "originator": "codex-tui", "status": map[string]string{"type": "notLoaded"}}}})
		if decoder.Decode(&req) == nil {
			t.Error("queued after Desktop connection loss", req.Method)
		}
	}()
	c := &Codex{RPC: rpc}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if c.StartRemoteInput(ctx, "thread", "message", "text", func() error { return nil }) == nil {
		t.Fatal("connection failure accepted")
	}
}

func TestDesktopStartUsesOriginalOwnerAndClientMessageID(t *testing.T) {
	for _, originator := range []string{"", "codex-tui", "codex-desktop"} {
		t.Run(originator, func(t *testing.T) {
			testDesktopStartUsesOriginalOwnerAndClientMessageID(t, originator)
		})
	}
}

func testDesktopStartUsesOriginalOwnerAndClientMessageID(t *testing.T, originator string) {
	home, err := os.MkdirTemp("/tmp", "bb-start-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(home)
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "ipc")
	if err = os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", filepath.Join(dir, "ipc.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, e := listener.Accept()
		if e != nil {
			return
		}
		defer conn.Close()
		for step := 0; step < 3; step++ {
			header := make([]byte, 4)
			if _, e = io.ReadFull(conn, header); e != nil {
				t.Error(e)
				return
			}
			body := make([]byte, binary.LittleEndian.Uint32(header))
			if _, e = io.ReadFull(conn, body); e != nil {
				t.Error(e)
				return
			}
			var req struct {
				ID      string         `json:"requestId"`
				Method  string         `json:"method"`
				Version int            `json:"version"`
				Target  string         `json:"targetClientId"`
				Params  map[string]any `json:"params"`
			}
			if json.Unmarshal(body, &req) != nil {
				t.Error("invalid request")
				return
			}
			result := map[string]any{"clientId": "relay"}
			if step == 0 && req.Method != "initialize" {
				t.Error(req.Method)
			}
			if step == 1 && (req.Method != "thread-owner-discovery" || req.Params["conversationId"] != "thread") {
				t.Error("wrong discovery")
			}
			if step == 2 {
				if req.Method != "thread-follower-start-turn" || req.Version != 2 || req.Target != "owner" {
					t.Error("wrong executor")
				}
				start := req.Params["turnStart"].(map[string]any)
				request := start["request"].(map[string]any)
				if request["threadId"] != "thread" || request["clientUserMessageId"] != "source-message" || start["context"].(map[string]any)["inheritThreadSettings"] != true {
					t.Error("lost origin or permissions")
				}
				input := request["input"].([]any)[0].(map[string]any)
				if elements, ok := input["text_elements"].([]any); !ok || len(elements) != 0 {
					t.Error("missing Desktop text_elements array")
				}
				if input["text"] != "new input" {
					t.Error("input changed")
				}
				result = map[string]any{"result": map[string]any{"turn": map[string]any{"id": "new-turn"}}}
			}
			data, _ := json.Marshal(map[string]any{"type": "response", "requestId": req.ID, "resultType": "success", "handledByClientId": "owner", "result": result})
			frame := make([]byte, 4+len(data))
			binary.LittleEndian.PutUint32(frame, uint32(len(data)))
			copy(frame[4:], data)
			conn.Write(frame)
		}
	}()
	reader, send := io.Pipe()
	requests, writer := io.Pipe()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	go func() {
		defer send.Close()
		defer requests.Close()
		var req struct {
			ID     int64  `json:"id"`
			Method string `json:"method"`
		}
		decoder := json.NewDecoder(requests)
		if decoder.Decode(&req) != nil {
			return
		}
		if req.Method != "thread/read" {
			t.Error("unexpected method")
		}
		json.NewEncoder(send).Encode(map[string]any{"id": req.ID, "result": map[string]any{"thread": map[string]any{"id": "thread", "cwd": "/test", "originator": originator, "status": map[string]string{"type": "notLoaded"}}}})
		if decoder.Decode(&req) == nil {
			t.Error("unexpected sidecar mutation", req.Method)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	authorized := false
	if err := (&Codex{RPC: rpc}).StartRemoteInput(ctx, "thread", "source-message", "new input", func() error { authorized = true; return nil }); err != nil {
		t.Fatal(err)
	}
	if !authorized {
		t.Fatal("not authorized before start")
	}
	<-done
}
