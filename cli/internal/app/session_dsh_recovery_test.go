package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

type dshRecoveryEvent struct {
	method string
	id     string
}

type dshRecoveryFixture struct {
	current       atomic.Pointer[sessionView]
	asyncClosed   atomic.Bool
	events        chan dshRecoveryEvent
	cancel        context.CancelFunc
	done          chan struct{}
	asyncID       string
	syncID        string
	r             *remote
	d             *relay.DSH
	answerReady   atomic.Bool
	mixAfterClaim atomic.Bool
	claimed       atomic.Bool
	nativeAnswers atomic.Int32
	settled       atomic.Bool
}

func startDSHRecoveryFixture(t *testing.T) *dshRecoveryFixture {
	t.Helper()
	f := &dshRecoveryFixture{events: make(chan dshRecoveryEvent, 128), done: make(chan struct{})}
	view := sessionView{UserID: "user", Generation: 4, NativeSession: "session-test", RuntimeKind: "deepseek-harness", RuntimeInstance: "profile", Desired: true, Online: true, Status: "ready"}
	f.current.Store(&view)
	requests := []relay.DSHInteraction{
		{ID: "continued", CallID: "continued", Kind: "question", TurnID: "1", OriginIDs: []string{"remote"}, Continued: true, Timed: true, Questions: []relay.DSHQuestion{{ID: "async", Question: "Continue?"}}},
		{ID: "blocking", Kind: "question", TurnID: "1", OriginIDs: []string{"remote"}, Questions: []relay.DSHQuestion{{ID: "sync", Question: "Choose?"}}},
	}
	f.asyncID = dshInteractionIDs(view, requests[0])[0]
	f.syncID = dshInteractionIDs(view, requests[1])[0]
	directory, err := os.MkdirTemp("", "bb-dsh-recovery-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	socket := filepath.Join(directory, "bridge.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	if err := os.Chmod(socket, 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		scanner := bufio.NewScanner(connection)
		for scanner.Scan() {
			var request struct {
				ID     int64                      `json:"id"`
				Method string                     `json:"method"`
				Params map[string]json.RawMessage `json:"params"`
			}
			if json.Unmarshal(scanner.Bytes(), &request) != nil {
				return
			}
			var result any
			switch request.Method {
			case "inspect":
				result = relay.DSHTarget{Instance: "instance", Owner: "owner", SessionID: view.NativeSession, Cwd: directory}
			case "bind_interactions":
				result = map[string]bool{"bound": true}
			case "unbind_interactions":
				result = map[string]bool{"unbound": true}
				f.events <- dshRecoveryEvent{method: "unbind"}
			case "interactions":
				result = map[string]any{"requests": requests[1:]}
			case "continued_interactions":
				f.events <- dshRecoveryEvent{method: "continued-scan"}
				result = map[string]any{"requests": requests[:1]}
				if f.settled.Load() {
					result = map[string]any{"requests": []relay.DSHInteraction{}}
				}
			case "answer_continued":
				var call, reply string
				var generation int64
				var answer struct {
					Answers []struct {
						ID     string
						Custom string
					}
				}
				json.Unmarshal(request.Params["call_id"], &call)
				json.Unmarshal(request.Params["request_id"], &reply)
				json.Unmarshal(request.Params["generation"], &generation)
				json.Unmarshal(request.Params["answer"], &answer)
				if call != "continued" || reply == "" || generation != 4 || len(answer.Answers) != 1 || answer.Answers[0].ID != "async" || answer.Answers[0].Custom != "continue-original" {
					t.Error("continued answer lost original identity")
				}
				f.nativeAnswers.Add(1)
				f.settled.Store(true)
				result = map[string]any{"accepted": true, "message_id": "native-original-answer"}
			default:
				t.Error("unexpected native operation", request.Method)
				return
			}
			if json.NewEncoder(connection).Encode(map[string]any{"id": request.ID, "result": result}) != nil {
				return
			}
		}
	}()
	d, err := relay.OpenDSH(context.Background(), socket, view.NativeSession, "instance")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	f.d = d
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := strings.TrimPrefix(request.URL.Path, "/v1/session-relay")
		switch {
		case request.Method == "GET" && path == "":
			json.NewEncoder(w).Encode(f.current.Load())
		case request.Method == "POST" && path == "/origin":
			scope := "remote"
			if f.mixAfterClaim.Load() && f.claimed.Load() {
				scope = "mixed"
				f.events <- dshRecoveryEvent{method: "mixed-final"}
			}
			json.NewEncoder(w).Encode(map[string]string{"scope": scope})
		case request.Method == "POST" && path == "/interactions":
			var body struct{ ID string }
			json.NewDecoder(request.Body).Decode(&body)
			f.events <- dshRecoveryEvent{method: "open", id: body.ID}
			state, answer := "pending", ""
			if body.ID == f.asyncID && f.answerReady.Load() {
				state, answer = "answered", "continue-original"
			}
			json.NewEncoder(w).Encode(map[string]string{"id": body.ID, "state": state, "answer": answer})
		case request.Method == "GET" && path == "/interactions/"+f.asyncID:
			json.NewEncoder(w).Encode(map[string]string{"id": f.asyncID, "state": "answered", "answer": "continue-original"})
		case request.Method == "POST" && path == "/async-answers":
			f.claimed.Store(true)
			json.NewEncoder(w).Encode(map[string]bool{"dispatch": true})
		case request.Method == "POST" && strings.HasSuffix(path, "/close"):
			id := strings.TrimSuffix(strings.TrimPrefix(path, "/interactions/"), "/close")
			var body struct{ State string }
			json.NewDecoder(request.Body).Decode(&body)
			if id == f.asyncID && body.State == "cancelled" {
				f.asyncClosed.Store(true)
			}
			f.events <- dshRecoveryEvent{method: "close", id: id}
			if body.State == "resolved" {
				f.events <- dshRecoveryEvent{method: "resolved", id: id}
			}
			json.NewEncoder(w).Encode(map[string]string{"id": id, "state": body.State})
		default:
			t.Error("unexpected API operation", request.Method, path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
	f.r = r
	ctx, cancel := context.WithCancel(context.Background())
	f.cancel = cancel
	go func() { defer close(f.done); runDSHInteractions(ctx, r, d, &f.current) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-f.done:
		case <-time.After(3 * time.Second):
			t.Error("interaction worker did not stop")
		}
	})
	f.waitFor(t, "open", f.asyncID, f.syncID)
	return f
}

func (f *dshRecoveryFixture) waitFor(t *testing.T, method string, ids ...string) {
	t.Helper()
	pending := map[string]bool{}
	for _, id := range ids {
		pending[id] = true
	}
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	for len(pending) > 0 {
		select {
		case event := <-f.events:
			if event.method == method {
				delete(pending, event.id)
			}
		case <-timer.C:
			t.Fatalf("missing %s events for %v", method, pending)
		}
	}
}

func TestDSHRecoveryDoesNotPreserveCardsForChangedOfflineTarget(t *testing.T) {
	for _, change := range []string{"generation", "session", "profile"} {
		t.Run(change, func(t *testing.T) {
			f := startDSHRecoveryFixture(t)
			view := *f.current.Load()
			view.Online = false
			if change == "generation" {
				view.Generation++
			}
			if change == "session" {
				view.NativeSession = "other-session"
			}
			if change == "profile" {
				view.RuntimeInstance = "other-profile"
			}
			f.current.Store(&view)
			f.waitFor(t, "close", f.asyncID, f.syncID)
		})
	}
}

func TestDSHWorkerExitObservesExplicitDetachBeforeNextPoll(t *testing.T) {
	f := startDSHRecoveryFixture(t)
	view := *f.current.Load()
	view.Desired = false
	f.current.Store(&view)
	f.cancel()
	f.waitFor(t, "close", f.asyncID, f.syncID)
}

func TestDSHTemporaryOfflineAndWorkerRestartPreserveOnlyTimedCards(t *testing.T) {
	f := startDSHRecoveryFixture(t)
	view := *f.current.Load()
	view.Online = false
	f.current.Store(&view)
	f.waitFor(t, "close", f.syncID)
	view.Online = true
	f.current.Store(&view)
	f.waitFor(t, "open", f.asyncID)
	f.cancel()
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		t.Fatal("interaction worker did not stop")
	}
	if f.asyncClosed.Load() {
		t.Fatal("temporary outage or worker restart cancelled the continued question")
	}
}

func TestDSHWorkerExitAfterOfflineStillObservesDetach(t *testing.T) {
	f := startDSHRecoveryFixture(t)
	view := *f.current.Load()
	view.Online = false
	f.current.Store(&view)
	f.waitFor(t, "close", f.syncID)
	view.Desired = false
	f.current.Store(&view)
	f.cancel()
	f.waitFor(t, "close", f.asyncID)
}

func TestDSHWorkerExitWithdrawsLeaseWithStaleReadySnapshot(t *testing.T) {
	f := startDSHRecoveryFixture(t)
	f.cancel()
	f.waitFor(t, "unbind", "")
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		t.Fatal("interaction worker did not stop")
	}
	if !f.current.Load().Desired || !f.current.Load().Online || f.current.Load().Status != "ready" {
		t.Fatal("test did not retain the stale ready snapshot")
	}
}

func TestDSHSecondWorkerRestoresPendingTimedQuestionAndAnswersOriginalOnce(t *testing.T) {
	f := startDSHRecoveryFixture(t)
	f.cancel()
	select {
	case <-f.done:
	case <-time.After(3 * time.Second):
		t.Fatal("first worker did not stop")
	}
	if f.asyncClosed.Load() {
		t.Fatal("first worker cancelled the recoverable timed card")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); runDSHInteractions(ctx, f.r, f.d, &f.current) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("second worker did not stop")
		}
	})
	f.answerReady.Store(true)
	f.waitFor(t, "resolved", f.asyncID)
	f.waitFor(t, "continued-scan", "")
	cancel()
	<-done
	if f.nativeAnswers.Load() != 1 || !f.settled.Load() || f.asyncClosed.Load() {
		t.Fatal("restored question was cancelled, duplicated or not settled", f.nativeAnswers.Load())
	}
}

func TestDSHFinalAnswerAuthorizationRejectsOriginChangedAfterDispatchClaim(t *testing.T) {
	f := startDSHRecoveryFixture(t)
	f.mixAfterClaim.Store(true)
	f.answerReady.Store(true)
	f.waitFor(t, "mixed-final", "")
	f.cancel()
	<-f.done
	if !f.claimed.Load() || f.nativeAnswers.Load() != 0 || f.settled.Load() {
		t.Fatal("mixed origin crossed the final native authorization boundary")
	}
}
