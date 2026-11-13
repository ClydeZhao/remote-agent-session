package app

import (
	"context"
	"errors"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"github.com/google/uuid"
)

type relaySummaryJob struct {
	MessageID  string `json:"message_id"`
	Generation int64  `json:"generation"`
	Text       string `json:"text"`
}

type relaySummaryCall func(context.Context, relay.Session, string) (string, error)

func summaryTarget(current *sessionView, target sessionView, generation int64) bool {
	return current != nil && current.Desired && current.Status == "ready" && current.Generation == generation && current.NativeSession == target.NativeSession && current.RuntimeKind == target.RuntimeKind && current.RuntimeInstance == target.RuntimeInstance
}

func summaryWait(ctx context.Context) bool {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func summaryTerminal(err error) bool {
	var f *failure
	if !errors.As(err, &f) {
		return false
	}
	if f.exit == exitAuth {
		return true
	}
	switch f.detail.Code {
	case "SESSION_CHANGED", "SESSION_DETACHED", "SESSION_OTHER_DEVICE", "SESSION_NOT_ATTACHED", "SESSION_LOGIN_REQUIRED", "SESSION_SUMMARY_CLAIM_INACTIVE", "SESSION_SUMMARY_CLAIM_CONFLICT", "SESSION_REQUEST_CONFLICT":
		return true
	default:
		return false
	}
}

func runRelaySummaries(ctx context.Context, r *remote, host relay.Session, target sessionView, current *atomic.Pointer[sessionView], summarize relaySummaryCall, signals ...<-chan struct{}) {
	var wake <-chan struct{}
	if len(signals) > 0 {
		wake = signals[0]
	}
	draining := true
	for ctx.Err() == nil {
		if !draining {
			select {
			case <-ctx.Done():
				return
			case <-wake:
				draining = true
			}
		}
		view := current.Load()
		if view == nil || !summaryTarget(view, target, view.Generation) {
			if !summaryWait(ctx) {
				return
			}
			continue
		}
		generation := view.Generation
		claimID := uuid.NewString()
		claimSucceeded := false
		var claimed struct {
			Job *relaySummaryJob `json:"job"`
		}
		for ctx.Err() == nil && summaryTarget(current.Load(), target, generation) {
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			err := r.relayCall(callCtx, "POST", "/summaries/claim", map[string]any{"generation": generation, "claim_id": claimID}, &claimed)
			cancel()
			if err == nil {
				claimSucceeded = true
				break
			}
			if summaryTerminal(err) || !summaryWait(ctx) {
				break
			}
		}
		if ctx.Err() != nil {
			return
		}
		if !claimSucceeded || claimed.Job == nil {
			if claimSucceeded {
				draining = false
				continue
			}
			if !summaryWait(ctx) {
				return
			}
			continue
		}
		job := claimed.Job
		if job.Generation != generation || job.MessageID == "" || job.Text == "" || utf8.RuneCountInString(job.Text) > 262144 || !summaryTarget(current.Load(), target, generation) {
			continue
		}
		sumCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		summary, err := summarize(sumCtx, host, job.Text)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err != nil || summary == "" {
			summary = "摘要暂时不可用，请查看上方完整正文。"
		}
		body := map[string]any{"generation": generation, "claim_id": claimID, "summary": summary}
		deliveryCtx, stopDelivery := context.WithTimeout(ctx, 60*time.Second)
		for deliveryCtx.Err() == nil && summaryTarget(current.Load(), target, generation) {
			callCtx, cancel := context.WithTimeout(deliveryCtx, 15*time.Second)
			var result map[string]bool
			err := r.relayCall(callCtx, "POST", "/messages/"+job.MessageID+"/summary", body, &result)
			cancel()
			if err == nil || summaryTerminal(err) || !summaryWait(deliveryCtx) {
				break
			}
		}
		stopDelivery()
	}
}
