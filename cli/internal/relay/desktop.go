package relay

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/google/uuid"
)

var ErrInterruptUnavailable = errors.New("无法确认原会话可以接收这条新消息。请先在本地查看上一轮是否仍在运行；使用支持打断的客户端连接后再继续")

var ErrRemoteInputNotSubmitted = errors.New("remote input was not submitted")

type desktopIPC struct {
	conn   net.Conn
	client string
}

func openDesktop(ctx context.Context) (*desktopIPC, error) {
	if runtime.GOOS == "windows" {
		return nil, ErrInterruptUnavailable
	}
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(userHome, ".codex")
	}
	path := filepath.Join(home, "ipc", "ipc.sock")
	if err := validateControlSocket(path); err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", path)
	if err != nil {
		return nil, ErrInterruptUnavailable
	}
	deadline := time.Now().Add(40 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	conn.SetDeadline(deadline)
	d := &desktopIPC{conn: conn}
	var result struct {
		ClientID string `json:"clientId"`
	}
	_, err = d.call("initialize", 0, map[string]any{"clientType": "remote-agent-session-relay"}, "", &result)
	if err != nil || result.ClientID == "" {
		conn.Close()
		return nil, ErrInterruptUnavailable
	}
	d.client = result.ClientID
	return d, nil
}

func (d *desktopIPC) call(method string, version int, params any, target string, dst any) (string, error) {
	id := uuid.NewString()
	data, err := json.Marshal(map[string]any{"type": "request", "requestId": id, "sourceClientId": d.client, "method": method, "version": version, "params": params, "targetClientId": target, "timeoutMs": 35000})
	if err != nil {
		return "", err
	}
	frame := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(frame, uint32(len(data)))
	copy(frame[4:], data)
	for len(frame) > 0 {
		n, e := d.conn.Write(frame)
		if e != nil {
			return "", e
		}
		frame = frame[n:]
	}
	for {
		header := make([]byte, 4)
		if _, err = io.ReadFull(d.conn, header); err != nil {
			return "", err
		}
		size := binary.LittleEndian.Uint32(header)
		if size == 0 || size > 16*1024*1024 {
			return "", ErrInterruptUnavailable
		}
		body := make([]byte, size)
		if _, err = io.ReadFull(d.conn, body); err != nil {
			return "", err
		}
		var response struct {
			Type       string          `json:"type"`
			ID         string          `json:"requestId"`
			ResultType string          `json:"resultType"`
			Owner      string          `json:"handledByClientId"`
			Result     json.RawMessage `json:"result"`
		}
		if json.Unmarshal(body, &response) != nil {
			return "", ErrInterruptUnavailable
		}
		if response.ID != id || response.Type != "response" {
			continue
		}
		if response.ResultType != "success" {
			return "", ErrInterruptUnavailable
		}
		if dst != nil {
			err = json.Unmarshal(response.Result, dst)
		}
		return response.Owner, err
	}
}

func (c *Codex) InterruptActive(ctx context.Context, thread string, authorize func() error) error {
	current, err := c.InspectRelayTarget(ctx, thread)
	if err != nil {
		return err
	}
	if current.Status.Type == "idle" {
		return nil
	}
	var history struct {
		Data []Turn `json:"data"`
	}
	if err := c.RPC.Call(ctx, "thread/turns/list", map[string]any{"threadId": thread, "limit": 1, "itemsView": "notLoaded"}, &history); err != nil {
		return err
	}
	if len(history.Data) == 0 || (history.Data[0].Status != "inProgress" && history.Data[0].Status != "interrupted") || history.Data[0].CompletedAt != nil {
		return nil
	}
	turn := history.Data[0].ID
	if turn == "" {
		return ErrHistoryIncomplete
	}
	if current.Status.Type != "notLoaded" {
		if err := authorize(); err != nil {
			return err
		}
		return c.RPC.Call(ctx, "turn/interrupt", map[string]any{"threadId": thread, "turnId": turn}, nil)
	}
	d, err := openDesktop(ctx)
	if err != nil {
		return err
	}
	defer d.conn.Close()
	owner, err := d.call("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": thread}, "", nil)
	if err != nil || owner == "" {
		return ErrInterruptUnavailable
	}
	var result struct {
		OK          bool    `json:"ok"`
		Interrupted *string `json:"interruptedTurnId"`
	}
	if err := authorize(); err != nil {
		return err
	}
	_, err = d.call("thread-follower-interrupt-turn", 4, map[string]any{"conversationId": thread, "mode": "system", "expectedTurnId": turn}, owner, &result)
	if err != nil || !result.OK {
		return ErrInterruptUnavailable
	}
	if result.Interrupted == nil || *result.Interrupted != turn {
		return ErrInterruptUnavailable
	}
	return nil
}

func (c *Codex) StartRemoteInput(ctx context.Context, thread, id, text string, authorize func() error) error {
	current, err := c.InspectRelayTarget(ctx, thread)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
	}
	params := map[string]any{"threadId": thread, "clientUserMessageId": id, "input": []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}}
	if current.Status.Type != "notLoaded" {
		if err := authorize(); err != nil {
			return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
		}
		err := c.RPC.Call(ctx, "turn/start", params, nil)
		var rejection *rpcError
		if errors.As(err, &rejection) && (rejection.Code == -32601 || rejection.Code == -32602) {
			return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
		}
		return err
	}
	d, err := openDesktop(ctx)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, ErrInterruptUnavailable)
	}
	defer d.conn.Close()
	owner, err := d.call("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": thread}, "", nil)
	if err != nil || owner == "" {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, ErrInterruptUnavailable)
	}
	if err := authorize(); err != nil {
		return fmt.Errorf("%w: %w", ErrRemoteInputNotSubmitted, err)
	}
	_, err = d.call("thread-follower-start-turn", 2, map[string]any{"conversationId": thread, "turnStart": map[string]any{"request": params, "context": map[string]any{"inheritThreadSettings": true}}}, owner, nil)
	return err
}
