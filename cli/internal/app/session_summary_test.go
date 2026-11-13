package app

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

type summaryTestHost struct {
	relay.Session
	interrupted atomic.Bool
	submitted   atomic.Bool
}

func (h *summaryTestHost) Outcome(_ context.Context, _ string, id string, _ time.Time) (relay.Outcome, error) {
	if id == "first" {
		return relay.Outcome{Found: true, Complete: true, Status: "completed", Text: strings.Repeat("中", 201)}, nil
	}
	return relay.Outcome{Found: true, Status: "inProgress"}, nil
}
func (h *summaryTestHost) InterruptActive(_ context.Context, _ string, authorize func() error) error {
	if err := authorize(); err != nil {
		return err
	}
	h.interrupted.Store(true)
	return nil
}
func (h *summaryTestHost) StartRemoteInput(_ context.Context, _ string, _ string, _ string, authorize func() error) error {
	if err := authorize(); err != nil {
		return err
	}
	h.submitted.Store(true)
	return nil
}

func TestSlowDurableSummaryDoesNotBlockNextInputInterruptOrAnswer(t *testing.T) {
	view := sessionView{Desired: true, Online: true, Status: "ready", Generation: 1, NativeSession: "original", RuntimeKind: "deepseek-harness", RuntimeInstance: "instance"}
	var bodySaved, claimed atomic.Bool
	summaryPosted := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/session-relay":
			jsonReply(w, 200, view)
		case "/v1/session-relay/messages/first/result":
			var body struct {
				SummaryPending bool `json:"summary_pending"`
				Summary        string
				Text           string
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || !body.SummaryPending || body.Summary != "" || body.Text != strings.Repeat("中", 201) {
				t.Error("body and pending summary were not registered together")
			}
			bodySaved.Store(true)
			io.WriteString(w, `{"accepted":true}`)
		case "/v1/session-relay/summaries/claim":
			if !bodySaved.Load() {
				t.Error("claim preceded body")
			}
			if claimed.Swap(true) {
				io.WriteString(w, `{"job":null}`)
			} else {
				jsonReply(w, 200, map[string]any{"job": relaySummaryJob{MessageID: "first", Generation: 1, Text: strings.Repeat("中", 201)}})
			}
		case "/v1/session-relay/messages/next/dispatch":
			io.WriteString(w, `{"dispatch":true}`)
		case "/v1/session-relay/messages/next/working", "/v1/session-relay/interactions/question":
			io.WriteString(w, `{"accepted":true}`)
		case "/v1/session-relay/messages/first/summary":
			summaryPosted <- struct{}{}
			io.WriteString(w, `{"accepted":true}`)
		default:
			t.Error("unexpected operation", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
	host := &summaryTestHost{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	processRelayMessage(ctx, r, host, view, sessionMessage{ID: "first", Kind: "chat", State: "dispatching", Generation: 1}, &relayMessageRecovery{})
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var current atomic.Pointer[sessionView]
	current.Store(&view)
	go func() {
		defer close(done)
		runRelaySummaries(ctx, r, host, view, &current, func(ctx context.Context, _ relay.Session, _ string) (string, error) {
			close(entered)
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-release:
				return "摘要", nil
			}
		})
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("summary worker did not start")
	}
	dispatched := make(chan struct{})
	go func() {
		defer close(dispatched)
		processRelayMessage(ctx, r, host, view, sessionMessage{ID: "next", Kind: "chat", State: "pending", Generation: 1}, &relayMessageRecovery{})
	}()
	select {
	case <-dispatched:
	case <-time.After(time.Second):
		t.Fatal("slow summary blocked the next input")
	}
	if !host.interrupted.Load() || !host.submitted.Load() {
		t.Fatal("next input did not interrupt and submit")
	}
	answerCtx, stop := context.WithTimeout(ctx, time.Second)
	defer stop()
	var answer map[string]bool
	if err := r.relayCall(answerCtx, "POST", "/interactions/question", map[string]any{"answer": "yes"}, &answer); err != nil {
		t.Fatal("summary blocked question answer", err)
	}
	select {
	case <-summaryPosted:
		t.Fatal("summary completed before release")
	default:
	}
	close(release)
	select {
	case <-summaryPosted:
	case <-time.After(time.Second):
		t.Fatal("summary was not delivered")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}
}

func TestSummaryClaimAndDeliveryRetryKeepIdentityAndDoNotRegenerate(t *testing.T) {
	view := sessionView{Desired: true, Online: true, Status: "ready", Generation: 2, NativeSession: "original", RuntimeKind: "codex"}
	var claimCalls, deliveries, modelCalls atomic.Int32
	var claimID, summary string
	delivered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/claim") && deliveries.Load() >= 2 {
			io.WriteString(w, `{"job":null}`)
			return
		}
		var body struct {
			ClaimID string `json:"claim_id"`
			Summary string
		}
		json.NewDecoder(r.Body).Decode(&body)
		if claimID == "" {
			claimID = body.ClaimID
		} else if claimID != body.ClaimID {
			t.Error("retry replaced original claim")
		}
		if strings.HasSuffix(r.URL.Path, "/claim") {
			if claimCalls.Add(1) == 1 {
				connection, _, _ := w.(http.Hijacker).Hijack()
				connection.Close()
				return
			}
			jsonReply(w, 200, map[string]any{"job": relaySummaryJob{MessageID: "original-message", Generation: 2, Text: "material"}})
			return
		}
		if !strings.HasSuffix(r.URL.Path, "/summary") {
			t.Error("summary worker attempted body delivery")
		}
		if summary == "" {
			summary = body.Summary
		} else if summary != body.Summary {
			t.Error("summary retry changed content")
		}
		if deliveries.Add(1) == 1 {
			connection, _, _ := w.(http.Hijacker).Hijack()
			connection.Close()
			return
		}
		io.WriteString(w, `{"accepted":true}`)
		delivered <- struct{}{}
	}))
	defer server.Close()
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var current atomic.Pointer[sessionView]
	current.Store(&view)
	done := make(chan struct{})
	go func() {
		defer close(done)
		runRelaySummaries(ctx, r, nil, view, &current, func(context.Context, relay.Session, string) (string, error) {
			modelCalls.Add(1)
			return "stable summary", nil
		})
	}()
	select {
	case <-delivered:
	case <-ctx.Done():
		t.Fatal("fixed-identity recovery did not finish")
	}
	cancel()
	<-done
	if claimCalls.Load() != 2 || deliveries.Load() != 2 || modelCalls.Load() != 1 {
		t.Fatalf("unexpected claim/model/delivery counts %d/%d/%d", claimCalls.Load(), modelCalls.Load(), deliveries.Load())
	}
}
