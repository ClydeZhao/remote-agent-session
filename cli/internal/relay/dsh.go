package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"
)

type DSHTarget struct {
	Instance  string `json:"instance"`
	Owner     string `json:"owner"`
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
	Status    string `json:"status"`
	TurnID    string `json:"turn_id"`
}

type DSH struct {
	rpc    *RPC
	target DSHTarget
}

func OpenDSH(ctx context.Context, socketPath, sessionID, instance string) (*DSH, error) {
	if err := validateControlSocket(socketPath); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	if err != nil {
		return nil, ErrInterruptUnavailable
	}
	rpc := NewRPC(conn, conn, func() { conn.Close() })
	var target DSHTarget
	if err = rpc.Call(ctx, "inspect", map[string]string{"session_id": sessionID}, &target); err != nil {
		rpc.Close()
		return nil, err
	}
	if target.SessionID != sessionID || target.Instance == "" || target.Instance != instance || target.Owner == "" || target.Cwd == "" {
		rpc.Close()
		return nil, ErrInterruptUnavailable
	}
	return &DSH{rpc: rpc, target: target}, nil
}

func (d *DSH) params(sessionID string) (map[string]any, error) {
	if sessionID != d.target.SessionID {
		return nil, ErrInterruptUnavailable
	}
	return map[string]any{"instance": d.target.Instance, "owner": d.target.Owner, "session_id": sessionID, "cwd": d.target.Cwd}, nil
}

func (d *DSH) inspect(ctx context.Context, sessionID string) (DSHTarget, error) {
	if _, err := d.params(sessionID); err != nil {
		return DSHTarget{}, err
	}
	var current DSHTarget
	if err := d.rpc.Call(ctx, "inspect", map[string]string{"session_id": sessionID}, &current); err != nil {
		return current, err
	}
	if current.SessionID != sessionID || current.Instance != d.target.Instance || current.Owner != d.target.Owner || current.Cwd != d.target.Cwd {
		return DSHTarget{}, ErrInterruptUnavailable
	}
	return current, nil
}

func (d *DSH) InspectRelayTarget(ctx context.Context, sessionID string) (Thread, error) {
	target, err := d.inspect(ctx, sessionID)
	thread := Thread{ID: target.SessionID, Cwd: target.Cwd, Name: "DSH 会话"}
	thread.Status.Type = target.Status
	return thread, err
}

func (d *DSH) history(ctx context.Context, sessionID string) (DSHHistory, error) {
	params, err := d.params(sessionID)
	if err != nil {
		return DSHHistory{}, err
	}
	var history DSHHistory
	err = d.rpc.Call(ctx, "history", params, &history)
	return history, err
}

func (d *DSH) CheckHistory(ctx context.Context, sessionID string) error {
	history, err := d.history(ctx, sessionID)
	if err != nil {
		return err
	}
	_, err = history.Outcome(sessionID, "capability-check")
	return err
}

func (d *DSH) Queued(ctx context.Context, sessionID, id string) (bool, error) {
	params, err := d.params(sessionID)
	if err != nil {
		return false, err
	}
	params["request_id"] = id
	var result struct{ Queued bool }
	err = d.rpc.Call(ctx, "queued", params, &result)
	return result.Queued, err
}

func (d *DSH) InterruptActive(ctx context.Context, sessionID string, authorize func() error) error {
	current, err := d.inspect(ctx, sessionID)
	if err != nil {
		return err
	}
	if current.Status == "idle" {
		return nil
	}
	if current.TurnID == "" {
		return ErrInterruptUnavailable
	}
	params, err := d.params(sessionID)
	if err != nil {
		return err
	}
	params["turn_id"] = current.TurnID
	if err = authorize(); err != nil {
		return err
	}
	var result struct{ Interrupted bool }
	if err = d.rpc.Call(ctx, "interrupt", params, &result); err != nil || !result.Interrupted {
		return ErrInterruptUnavailable
	}
	return nil
}

func (d *DSH) StartRemoteInput(ctx context.Context, sessionID, id, text string, authorize func() error) error {
	params, err := d.params(sessionID)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
	}
	if _, err = d.inspect(ctx, sessionID); err != nil {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
	}
	if err = authorize(); err != nil {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
	}
	params["request_id"], params["text"] = id, text
	var result struct{ Accepted bool }
	if err = d.rpc.Call(ctx, "submit", params, &result); err != nil {
		return err
	}
	if !result.Accepted {
		return errors.New("无法确认 DeepSeek Harness 是否接收输入，请核对原会话，不要重发")
	}
	return nil
}

func (d *DSH) Outcome(ctx context.Context, sessionID, id string, since time.Time) (Outcome, error) {
	history, err := d.history(ctx, sessionID)
	if err != nil {
		return Outcome{}, err
	}
	return history.Outcome(sessionID, id)
}

func (d *DSH) CancelQueued(ctx context.Context, sessionID, id string) error {
	params, err := d.params(sessionID)
	if err != nil {
		return err
	}
	params["request_id"] = id
	var result struct{ Cancelled bool }
	if err = d.rpc.Call(ctx, "cancel_queued", params, &result); err != nil {
		return err
	}
	if !result.Cancelled {
		return ErrInterruptUnavailable
	}
	return nil
}

func (d *DSH) Close()                { d.rpc.Close() }
func (d *DSH) Done() <-chan struct{} { return d.rpc.Done() }

var _ Session = (*DSH)(nil)
