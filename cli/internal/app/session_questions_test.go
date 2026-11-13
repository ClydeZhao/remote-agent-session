package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

type questionRuntime struct {
	question        relay.NativeQuestion
	answered        bool
	independentWork bool
	missing         bool
	unreadable      bool
	originFailed    bool
	allOriginFailed bool
	t               *testing.T
}

func (q *questionRuntime) PendingQuestions(context.Context, string) ([]relay.NativeQuestion, error) {
	if q.unreadable {
		return nil, errors.New("snapshot unavailable")
	}
	if q.missing {
		return nil, nil
	}
	return []relay.NativeQuestion{q.question}, nil
}
func (q *questionRuntime) OriginIDsForQuestion(context.Context, string, string) ([]string, error) {
	if q.originFailed {
		return nil, errors.New("origin unavailable")
	}
	return []string{"11111111-1111-4111-8111-111111111111"}, nil
}
func (q *questionRuntime) AllOriginIDsForQuestion(ctx context.Context, thread, turn string) ([]string, error) {
	if q.allOriginFailed {
		return nil, errors.New("full origin unavailable")
	}
	return q.OriginIDsForQuestion(ctx, thread, turn)
}
func (q *questionRuntime) AnswerQuestion(ctx context.Context, request relay.NativeQuestion, response relay.NativeQuestionResponse, authorize func() error) error {
	if err := authorize(); err != nil {
		return err
	}
	if !q.independentWork || request.StableID() != q.question.StableID() || response.Answers["color"].Answers[0] != "绿色" {
		q.t.Fatal("answer lost native identity or independent work did not continue")
	}
	q.answered = true
	return nil
}
func TestNativeAsyncQuestionContinuesWorkAndAnswersOriginalRequest(t *testing.T) {
	userAnswered := false
	closed := false
	dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Method == "POST" {
			json.NewDecoder(r.Body).Decode(&body)
		}
		if r.URL.Path == "/v1/session-relay/origin" {
			jsonReply(w, 200, map[string]any{"scope": "remote"})
			return
		}
		if r.URL.Path == "/v1/session-relay/async-answers" {
			jsonReply(w, 200, map[string]any{"dispatch": true})
			return
		}
		if strings.HasSuffix(r.URL.Path, "/close") {
			closed = true
			jsonReply(w, 200, map[string]any{"state": "resolved"})
			return
		}
		if r.URL.Path == "/v1/session-relay/interactions" {
			if body["async_question"] != true {
				t.Fatal("async lifecycle missing")
			}
			if userAnswered {
				jsonReply(w, 200, map[string]any{"id": body["id"], "state": "answered", "answer": "绿色"})
			} else {
				jsonReply(w, 200, map[string]any{"id": body["id"], "state": "pending"})
			}
			return
		}
		if strings.Contains(r.URL.Path, "/interactions/") {
			jsonReply(w, 200, map[string]any{"state": "answered", "answer": "绿色"})
			return
		}
		t.Fatalf("unexpected mutation: %s %s", r.Method, r.URL.Path)
	})
	remote, err := connect(dir, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer remote.close()
	if err = remote.auth(); err != nil {
		t.Fatal(err)
	}
	runtime := &questionRuntime{t: t, question: relay.NativeQuestion{RequestID: json.RawMessage(`7`), Owner: "owner", ThreadID: testID, TurnID: "turn", ItemID: "item", Questions: []relay.NativeQuestionItem{{ID: "color", Question: "颜色？"}}}}
	view := sessionView{NativeSession: testID, UserID: "user", Generation: 1, Desired: true, Status: "ready", Online: true}
	processNativeQuestions(context.Background(), remote, runtime, view)
	if runtime.answered || closed {
		t.Fatal("pending question unexpectedly consumed")
	}
	runtime.independentWork = true
	userAnswered = true
	processNativeQuestions(context.Background(), remote, runtime, view)
	if !runtime.answered || !closed {
		t.Fatal("native answer was not resolved")
	}
}

func TestNativeQuestionMissingAndUnreadableAreDifferent(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(fmt.Sprint(unreadable), func(t *testing.T) {
			closed := false
			dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/known/close") {
					t.Fatal("unexpected endpoint", r.URL.Path)
				}
				closed = true
				jsonReply(w, 200, map[string]any{"state": "cancelled"})
			})
			remote, err := connect(dir, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer remote.close()
			if err = remote.auth(); err != nil {
				t.Fatal(err)
			}
			runtime := &questionRuntime{t: t, missing: true, unreadable: unreadable}
			processNativeQuestions(context.Background(), remote, runtime, sessionView{NativeSession: testID, AsyncInteractions: []string{"known"}, Generation: 1})
			if closed == unreadable {
				t.Fatal("unknown snapshot cancelled request or complete snapshot retained absent request")
			}
		})
	}
}

func TestNativeQuestionOriginFailuresPreservePendingAnswer(t *testing.T) {
	for _, failure := range []string{"origin", "all_origin", "server", "authentication", "unknown_scope", "confirmed_local", "confirmed_mixed", "confirmed_inactive"} {
		t.Run(failure, func(t *testing.T) {
			failed := false
			cancelled := false
			registrations := 0
			var id string
			dir, _ := testSetup(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/session-relay/origin" {
					if failed && (failure == "server" || failure == "authentication") {
						status := 503
						if failure == "authentication" {
							status = 401
						}
						jsonReply(w, status, map[string]any{"code": "UNAVAILABLE", "message": "unavailable"})
						return
					}
					scope := "remote"
					if failed && strings.HasPrefix(failure, "confirmed_") {
						scope = strings.TrimPrefix(failure, "confirmed_")
					}
					if failed && failure == "unknown_scope" {
						scope = "unknown"
					}
					jsonReply(w, 200, map[string]any{"scope": scope})
					return
				}
				if strings.HasSuffix(r.URL.Path, "/close") {
					cancelled = true
					jsonReply(w, 200, map[string]any{"state": "cancelled"})
					return
				}
				if r.URL.Path == "/v1/session-relay/interactions" {
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					id = body["id"].(string)
					registrations++
					jsonReply(w, 200, map[string]any{"id": id, "state": "pending"})
					return
				}
				t.Errorf("unexpected request: %s", r.URL.Path)
			})
			remote, err := connect(dir, 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			defer remote.close()
			if err = remote.auth(); err != nil {
				t.Fatal(err)
			}
			runtime := &questionRuntime{t: t, question: relay.NativeQuestion{RequestID: json.RawMessage(`7`), Owner: "owner", ThreadID: testID, TurnID: "turn", ItemID: "item", Questions: []relay.NativeQuestionItem{{ID: "color", Question: "颜色？"}}}}
			view := sessionView{NativeSession: testID, UserID: "user", Generation: 1, Desired: true, Status: "ready", Online: true}
			processNativeQuestions(context.Background(), remote, runtime, view)
			if id == "" {
				t.Fatal("question was not registered")
			}
			view.AsyncInteractions = []string{id}
			failed = true
			runtime.originFailed = failure == "origin"
			runtime.allOriginFailed = failure == "all_origin"
			processNativeQuestions(context.Background(), remote, runtime, view)
			if strings.HasPrefix(failure, "confirmed_") {
				if !cancelled || runtime.answered || registrations != 1 {
					t.Fatal("confirmed invalid origin was not cancelled")
				}
				return
			}
			if cancelled || runtime.answered || registrations != 1 {
				t.Fatal("temporary source failure cancelled or consumed existing question")
			}
			failed = false
			runtime.originFailed = false
			runtime.allOriginFailed = false
			processNativeQuestions(context.Background(), remote, runtime, view)
			if cancelled || registrations != 2 {
				t.Fatal("question did not recover after temporary failure")
			}
		})
	}
}
