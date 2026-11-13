package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync/atomic"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"github.com/google/uuid"
)

func dshOrigins(view sessionView) []string {
	ids := []string{}
	for _, message := range view.Messages {
		if message.Generation == view.Generation && (message.State == "pending" || message.State == "dispatching") {
			ids = append(ids, message.ID)
		}
	}
	return ids
}

func runDSHInteractions(ctx context.Context, r *remote, d *relay.DSH, current *atomic.Pointer[sessionView]) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	var bound sessionView
	var lastTarget sessionView
	registered := map[string]int64{}
	recoverable := map[string]bool{}
	preserved := func(view *sessionView) map[string]bool {
		if view != nil && (!view.Desired || view.RuntimeKind != "deepseek-harness" || view.UserID != lastTarget.UserID || view.Generation != lastTarget.Generation || view.NativeSession != lastTarget.NativeSession || view.RuntimeInstance != lastTarget.RuntimeInstance) {
			return nil
		}
		return recoverable
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if bound.NativeSession != "" {
			_ = d.UnbindInteractions(cleanup, bound.NativeSession, bound.Generation)
		}
		closeMissingDSHInteractions(cleanup, r, registered, preserved(current.Load()))
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		view := current.Load()
		if view == nil {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		if preserved(view) == nil {
			clear(recoverable)
			closeMissingDSHInteractions(callCtx, r, registered, nil)
		}
		if !view.Desired || !view.Online || view.Status != "ready" || view.RuntimeKind != "deepseek-harness" {
			if bound.NativeSession != "" {
				_ = d.UnbindInteractions(callCtx, bound.NativeSession, bound.Generation)
				bound = sessionView{}
			}
			closeMissingDSHInteractions(callCtx, r, registered, preserved(view))
			cancel()
			continue
		}
		var live sessionView
		if r.relayCall(callCtx, "GET", "", nil, &live) != nil || !live.Desired || !live.Online || live.Generation != view.Generation || live.NativeSession != view.NativeSession || live.RuntimeInstance != view.RuntimeInstance {
			cancel()
			continue
		}
		if d.BindInteractions(callCtx, view.NativeSession, view.Generation, dshOrigins(*view)) == nil {
			bound = *view
			lastTarget = *view
			requests, err := d.Interactions(callCtx, view.NativeSession, view.Generation)
			if err == nil {
				continued, continuedErr := d.ContinuedInteractions(callCtx, view.NativeSession, view.Generation)
				if continuedErr != nil {
					cancel()
					continue
				}
				requests = append(requests, continued...)
				present := map[string]bool{}
				for _, request := range requests {
					for _, id := range dshInteractionIDs(*view, request) {
						present[id] = true
						registered[id] = view.Generation
						recoverable[id] = request.Kind == "question" && request.Timed
					}
					processDSHInteraction(callCtx, r, d, *view, request)
				}
				closeMissingDSHInteractions(callCtx, r, registered, present)
				for id := range recoverable {
					if _, exists := registered[id]; !exists {
						delete(recoverable, id)
					}
				}
			}
		}
		cancel()
	}
}

func dshInteractionID(view sessionView, request relay.DSHInteraction, suffix string) string {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("dsh:%s:%d:%s:%s", view.UserID, view.Generation, request.ID, suffix))).String()
}

func dshInteractionIDs(view sessionView, request relay.DSHInteraction) []string {
	if request.Kind == "permission" {
		return []string{dshInteractionID(view, request, "permission")}
	}
	ids := []string{}
	for _, question := range request.Questions {
		ids = append(ids, dshInteractionID(view, request, question.ID))
	}
	return ids
}

func closeMissingDSHInteractions(ctx context.Context, r *remote, registered map[string]int64, present map[string]bool) {
	for id, generation := range registered {
		if present[id] {
			continue
		}
		var closed interactionView
		err := r.relayCall(ctx, "POST", "/interactions/"+id+"/close", map[string]any{"generation": generation, "state": "cancelled"}, &closed)
		var failure *failure
		obsolete := errors.As(err, &failure) && failure.detail.Code == "SESSION_CHANGED"
		if err == nil || obsolete {
			delete(registered, id)
		}
	}
}

func processDSHInteraction(ctx context.Context, r *remote, d *relay.DSH, view sessionView, request relay.DSHInteraction) {
	validate := func() error {
		ids := request.OriginIDs
		if request.Continued && len(request.AllOriginIDs) > 0 {
			ids = request.AllOriginIDs
		}
		var origin struct {
			Scope string `json:"scope"`
		}
		if err := r.relayCall(ctx, "POST", "/origin", map[string]any{"generation": view.Generation, "native_session": view.NativeSession, "message_ids": ids, "for_result": request.Continued}, &origin); err != nil {
			return err
		}
		if origin.Scope != "remote" {
			return relay.ErrMixedOrigin
		}
		return nil
	}
	if validate() != nil {
		return
	}
	if request.Continued {
		if d.BindInteractions(ctx, view.NativeSession, view.Generation, append(append(dshOrigins(view), request.OriginIDs...), request.AllOriginIDs...)) != nil {
			return
		}
	}
	responses := map[string]string{}
	register := func(suffix, kind, prompt string, choices map[string]string) (string, bool) {
		id := dshInteractionID(view, request, suffix)
		var result interactionView
		body := map[string]any{"id": id, "generation": view.Generation, "native_session": view.NativeSession, "turn_id": request.TurnID, "message_ids": request.OriginIDs, "kind": kind, "prompt": prompt, "async_question": request.Continued}
		if choices != nil {
			body["choices"] = choices
		}
		if r.relayCall(ctx, "POST", "/interactions", body, &result) != nil || result.ID != id || result.State != "answered" {
			return "", false
		}
		responses[id] = result.Answer
		return result.Answer, true
	}
	var answer any
	if request.Kind == "permission" {
		var call struct {
			Arguments json.RawMessage `json:"arguments"`
		}
		if json.Unmarshal(request.ToolCall, &call) != nil || len(call.Arguments) == 0 {
			return
		}
		var raw string
		if json.Unmarshal(call.Arguments, &raw) == nil {
			call.Arguments = json.RawMessage(raw)
		}
		target, err := d.InspectRelayTarget(ctx, view.NativeSession)
		if err != nil {
			return
		}
		prompt, err := permissionPrompt(relayHookInput{Tool: request.ToolName, Cwd: target.Cwd, Input: call.Arguments})
		if err != nil {
			return
		}
		value, ok := register("permission", "permission", prompt, nil)
		if !ok || (value != "allow" && value != "deny") {
			return
		}
		answer = value
	} else if request.Kind == "question" {
		if (request.Timed && !request.Continued) || len(request.Questions) == 0 {
			return
		}
		answers := []map[string]any{}
		complete := true
		for _, question := range request.Questions {
			prompt, choices, err := dshQuestionPrompt(question)
			if err != nil {
				return
			}
			value, ok := register(question.ID, "question", prompt, choices)
			if !ok {
				complete = false
				continue
			}
			answers = append(answers, dshQuestionAnswer(question, value))
		}
		if !complete {
			return
		}
		answer = map[string]any{"answers": answers}
	} else {
		return
	}
	authorize := func() error {
		if err := validate(); err != nil {
			return err
		}
		for id, value := range responses {
			var current interactionView
			if r.relayCall(ctx, "GET", "/interactions/"+id, nil, &current) != nil || current.State != "answered" || current.Answer != value {
				return relay.ErrAnswerUnconfirmed
			}
		}
		return nil
	}
	if request.Continued {
		replyID := uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("dsh-answer:%s:%d:%s:%s", view.UserID, view.Generation, view.NativeSession, request.CallID))).String()
		ids := make([]string, 0, len(responses))
		for id := range responses {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var claim struct {
			Dispatch bool `json:"dispatch"`
		}
		if authorize() != nil || r.relayCall(ctx, "POST", "/async-answers", map[string]any{"id": replyID, "generation": view.Generation, "interaction_ids": ids}, &claim) != nil {
			return
		}
		if d.BindInteractions(ctx, view.NativeSession, view.Generation, append(dshOrigins(view), replyID)) != nil {
			return
		}
		if _, err := d.AnswerContinued(ctx, view.NativeSession, view.Generation, request.CallID, replyID, answer, !claim.Dispatch, authorize); err != nil {
			return
		}
	} else if err := d.AnswerInteraction(ctx, view.NativeSession, view.Generation, request.ID, answer, authorize); err != nil {
		return
	}
	for id := range responses {
		var closed interactionView
		_ = r.relayCall(ctx, "POST", "/interactions/"+id+"/close", map[string]any{"generation": view.Generation, "state": "resolved"}, &closed)
	}
}
