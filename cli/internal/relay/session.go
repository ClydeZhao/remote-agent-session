package relay

import (
	"context"
	"time"
)

type Session interface {
	InspectRelayTarget(context.Context, string) (Thread, error)
	CheckHistory(context.Context, string) error
	Queued(context.Context, string, string) (bool, error)
	InterruptActive(context.Context, string, func() error) error
	StartRemoteInput(context.Context, string, string, string, func() error) error
	Outcome(context.Context, string, string, time.Time) (Outcome, error)
	CancelQueued(context.Context, string, string) error
	Close()
	Done() <-chan struct{}
}

func (c *Codex) Close() {
	c.RPC.Close()
}

func (c *Codex) Done() <-chan struct{} {
	return c.RPC.Done()
}

var _ Session = (*Codex)(nil)
