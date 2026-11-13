package relay

import (
	"encoding/json"
	"errors"
	"testing"
)

func dshHistory(events ...DSHEvent) DSHHistory {
	for i := range events {
		events[i].Seq = i
	}
	return DSHHistory{SessionID: "session-test", NextSeq: len(events), Events: events}
}

func dshEvent(kind, data string) DSHEvent {
	return DSHEvent{Type: kind, Data: json.RawMessage(data)}
}

func TestDSHOutcomeRequiresCompleteUnmixedTurn(t *testing.T) {
	base := []DSHEvent{
		dshEvent("turn/start", `{"turn":1}`),
		dshEvent("user/message", `{"source":{"kind":"user","rpcId":"remote-input"}}`),
		dshEvent("user/message", `{"source":{"kind":"time-context"}}`),
		dshEvent("assistant/message", `{"turn":1,"message":{"role":"assistant","content":[{"type":"reasoning","text":"private reasoning"},{"type":"text","text":"final answer"}]}}`),
	}
	for _, test := range []struct {
		name     string
		extra    []DSHEvent
		complete bool
		mixed    bool
		text     string
	}{
		{name: "running"},
		{name: "completed", extra: []DSHEvent{dshEvent("turn/end", `{"turn":1,"reason":{"kind":"completed"}}`)}, complete: true, text: "final answer"},
		{name: "local intervention", extra: []DSHEvent{dshEvent("user/message", `{"source":{"kind":"user"}}`), dshEvent("turn/end", `{"turn":1,"reason":{"kind":"completed"}}`)}, complete: true, mixed: true},
		{name: "unknown plugin input", extra: []DSHEvent{dshEvent("user/message", `{"source":{"kind":"another-plugin","rpcId":"remote-input"}}`), dshEvent("turn/end", `{"turn":1,"reason":{"kind":"completed"}}`)}, complete: true, mixed: true},
		{name: "failed", extra: []DSHEvent{dshEvent("turn/end", `{"turn":1,"reason":{"kind":"error"}}`)}, complete: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			events := append(append([]DSHEvent{}, base...), test.extra...)
			outcome, err := dshHistory(events...).Outcome("session-test", "remote-input")
			if !outcome.Found || outcome.Complete != test.complete || outcome.Mixed != test.mixed || outcome.Text != test.text || errors.Is(err, ErrMixedOrigin) != test.mixed {
				t.Fatalf("outcome=%+v error=%v", outcome, err)
			}
		})
	}
}

func TestDSHOutcomeRejectsGapsAndWrongTurn(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*DSHHistory)
	}{
		{"wrong session", func(h *DSHHistory) { h.SessionID = "another-session" }},
		{"missing tail", func(h *DSHHistory) { h.NextSeq++ }},
		{"sequence gap", func(h *DSHHistory) { h.Events[1].Seq++ }},
		{"wrong completion", func(h *DSHHistory) { h.Events[2].Data = json.RawMessage(`{"turn":2,"reason":{"kind":"completed"}}`) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := dshHistory(dshEvent("turn/start", `{"turn":1}`), dshEvent("user/message", `{"source":{"kind":"user","rpcId":"remote-input"}}`), dshEvent("turn/end", `{"turn":1,"reason":{"kind":"completed"}}`))
			test.edit(&h)
			outcome, err := h.Outcome("session-test", "remote-input")
			if !errors.Is(err, ErrHistoryIncomplete) || outcome.Text != "" {
				t.Fatalf("accepted incomplete history: %+v %v", outcome, err)
			}
		})
	}
}
