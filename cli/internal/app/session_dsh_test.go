package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func TestDSHHandoffPreservesRuntimeIdentityAtNativeBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/session-relay/messages/message/cancellation" {
			t.Error("unexpected request", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"requested": true})
	}))
	defer server.Close()
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
	item := localstate.RelayHandoff{RuntimeKind: "deepseek-harness", RuntimeInstance: "profile", NativeSession: "session-dsh", ClientID: "message"}
	called := false
	result := reconcileRelayHandoff(context.Background(), r, item, func(ctx context.Context, target localstate.RelayHandoff) (relay.Session, error) {
		called = true
		if target != item {
			t.Error("lost runtime identity", target)
		}
		return nil, errors.New("original host offline")
	})
	if !called || result {
		t.Fatal("unconfirmed withdrawal was lost")
	}
}

func TestDSHCancelledQuestionClosesOnlyMissingCards(t *testing.T) {
	var ids []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/v1/session-relay/interactions/old/close" {
			t.Error("unexpected close", r.URL.Path)
		}
		var body struct {
			Generation int64
			State      string
		}
		json.NewDecoder(r.Body).Decode(&body)
		if body.Generation != 4 || body.State != "cancelled" {
			t.Error("invalid cancellation", body)
		}
		ids = append(ids, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"id": "old", "state": "cancelled"})
	}))
	defer server.Close()
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
	registered := map[string]int64{"old": 4, "current": 4}
	closeMissingDSHInteractions(context.Background(), r, registered, map[string]bool{"current": true})
	if len(ids) != 1 || len(registered) != 1 || registered["current"] != 4 {
		t.Fatal("wrong cancellation scope", registered)
	}
}

func TestDSHCardCleanupRetainsUnknownFailuresButDropsObsoleteGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/session-relay/interactions/old/close" {
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]string{"code": "SESSION_CHANGED", "message": "changed", "retry_action": "user_action"})
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"code": "UNAVAILABLE", "message": "unavailable", "retry_action": "same_request"})
		}
	}))
	defer server.Close()
	r := &remote{client: &api.Client{Client: server.Client()}, config: localstate.Config{Server: server.URL}}
	registered := map[string]int64{"old": 3, "unknown": 4}
	closeMissingDSHInteractions(context.Background(), r, registered, nil)
	if len(registered) != 1 || registered["unknown"] != 4 {
		t.Fatal("cleanup discarded unresolved state or retained obsolete generation", registered)
	}
}
