package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync/atomic"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"github.com/google/uuid"
)

type nativeQuestions interface {
	PendingQuestions(context.Context, string) ([]relay.NativeQuestion, error)
	OriginIDsForQuestion(context.Context, string, string) ([]string, error)
	AllOriginIDsForQuestion(context.Context, string, string) ([]string, error)
	AnswerQuestion(context.Context, relay.NativeQuestion, relay.NativeQuestionResponse, func() error) error
}

func runNativeQuestions(ctx context.Context, r *remote, c nativeQuestions, current *atomic.Pointer[sessionView]) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			view := current.Load()
			if view == nil || !view.Desired || view.Status != "ready" {
				continue
			}
			callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			processNativeQuestions(callCtx, r, c, *view)
			cancel()
		}
	}
}

func processNativeQuestions(ctx context.Context, r *remote, c nativeQuestions, view sessionView) {
	questions, err := c.PendingQuestions(ctx, view.NativeSession)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	closeRequest := func(id, state string) {
		var result interactionView
		_ = r.relayCall(ctx, "POST", "/interactions/"+id+"/close", map[string]any{"generation": view.Generation, "state": state}, &result)
	}
	for _, q := range questions {
		if q.IsBlocking || q.ThreadID != view.NativeSession {
			continue
		}
		interactionIDs := map[string]string{}
		for _, question := range q.Questions {
			id := uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("%s:%d:%s:%s:%s:%s", view.UserID, view.Generation, q.StableID(), q.TurnID, q.ItemID, question.ID))).String()
			seen[id] = true
			interactionIDs[question.ID] = id
		}
		anchorIDs, err := c.OriginIDsForQuestion(ctx, q.ThreadID, q.TurnID)
		if err != nil || len(anchorIDs) == 0 {
			if errors.Is(err, relay.ErrMixedOrigin) {
				for _, id := range interactionIDs {
					closeRequest(id, "cancelled")
				}
			}
			continue
		}
		validateOrigin := func() error {
			allIDs, err := c.AllOriginIDsForQuestion(ctx, q.ThreadID, q.TurnID)
			if err != nil {
				return err
			}
			if len(allIDs) == 0 {
				return relay.ErrHistoryIncomplete
			}
			var scope struct {
				Scope string `json:"scope"`
			}
			err = r.relayCall(ctx, "POST", "/origin", map[string]any{"generation": view.Generation, "native_session": view.NativeSession, "message_ids": allIDs, "for_result": true}, &scope)
			if err != nil {
				return err
			}
			switch scope.Scope {
			case "remote":
				return nil
			case "local", "mixed", "inactive":
				return relay.ErrMixedOrigin
			default:
				return relay.ErrHistoryIncomplete
			}
		}
		if err := validateOrigin(); err != nil {
			if errors.Is(err, relay.ErrMixedOrigin) {
				for _, id := range interactionIDs {
					closeRequest(id, "cancelled")
				}
			}
			continue
		}
		sort.Strings(anchorIDs)
		responses := map[string]string{}
		allAnswered := true
		response := relay.NativeQuestionResponse{Answers: map[string]struct {
			Answers []string `json:"answers"`
		}{}}
		for _, question := range q.Questions {
			id := interactionIDs[question.ID]
			raw, _ := json.Marshal(map[string]any{"questions": []relay.NativeQuestionItem{question}})
			prompt, choices, err := questionPrompt(raw)
			if err != nil || question.ID == "" {
				closeRequest(id, "cancelled")
				allAnswered = false
				continue
			}
			var request interactionView
			body := map[string]any{"id": id, "generation": view.Generation, "native_session": view.NativeSession, "turn_id": q.TurnID, "message_ids": anchorIDs, "kind": "question", "prompt": prompt, "choices": choices, "async_question": true}
			if r.relayCall(ctx, "POST", "/interactions", body, &request) != nil || request.State != "answered" {
				allAnswered = false
				continue
			}
			responses[id] = request.Answer
			response.Answers[question.ID] = struct {
				Answers []string `json:"answers"`
			}{Answers: []string{request.Answer}}
		}
		if !allAnswered || len(responses) == 0 {
			continue
		}
		authorize := func() error {
			if err := validateOrigin(); err != nil {
				return err
			}
			currentIDs, err := c.OriginIDsForQuestion(ctx, q.ThreadID, q.TurnID)
			sort.Strings(currentIDs)
			if err != nil || !slices.Equal(anchorIDs, currentIDs) {
				return relay.ErrHistoryIncomplete
			}
			for id, answer := range responses {
				var current interactionView
				if err := r.relayCall(ctx, "GET", "/interactions/"+id, nil, &current); err != nil {
					return err
				}
				if current.State != "answered" || current.Answer != answer {
					return relay.ErrHistoryIncomplete
				}
			}
			return nil
		}
		if authorize() != nil {
			continue
		}
		q.ReplyID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("answer:%s:%d:%s", view.UserID, view.Generation, q.StableID()))).String()
		questionIDs := make([]string, 0, len(interactionIDs))
		for _, id := range interactionIDs {
			questionIDs = append(questionIDs, id)
		}
		sort.Strings(questionIDs)
		var claim struct {
			Dispatch bool `json:"dispatch"`
		}
		if r.relayCall(ctx, "POST", "/async-answers", map[string]any{"id": q.ReplyID, "generation": view.Generation, "interaction_ids": questionIDs}, &claim) != nil {
			continue
		}
		q.VerifyOnly = !claim.Dispatch
		if c.AnswerQuestion(ctx, q, response, authorize) == nil {
			for _, id := range interactionIDs {
				closeRequest(id, "resolved")
			}
		}
	}
	for _, id := range view.AsyncInteractions {
		if !seen[id] {
			closeRequest(id, "cancelled")
		}
	}
}
