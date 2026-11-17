package relay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

type controlWebSocket struct {
	conn    *websocket.Conn
	ctx     context.Context
	pending []byte
}

func openControlSocket(ctx context.Context, path string) (*Codex, error) {
	resolved, release, err := prepareControlSocket(path)
	if err != nil {
		return nil, err
	}
	defer release()
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", resolved)
	}}
	client := &http.Client{Transport: transport}
	conn, _, err := websocket.Dial(ctx, "ws://localhost/", &websocket.DialOptions{HTTPClient: client})
	if err != nil {
		transport.CloseIdleConnections()
		return nil, ErrInterruptUnavailable
	}
	conn.SetReadLimit(16 * 1024 * 1024)
	lifetime, cancel := context.WithCancel(context.Background())
	stream := &controlWebSocket{conn: conn, ctx: lifetime}
	rpc := NewRPC(stream, stream, func() { cancel(); conn.CloseNow(); transport.CloseIdleConnections() })
	if err = rpc.Initialize(ctx); err != nil {
		rpc.Close()
		return nil, err
	}
	return &Codex{RPC: rpc}, nil
}

func (s *controlWebSocket) Read(dst []byte) (int, error) {
	if len(s.pending) == 0 {
		kind, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return 0, io.EOF
		}
		if kind != websocket.MessageText {
			return 0, errors.New("Codex control socket 返回非文本消息")
		}
		s.pending = append(data, '\n')
	}
	n := copy(dst, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *controlWebSocket) Write(data []byte) (int, error) {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, websocket.MessageText, bytes.TrimSpace(data)); err != nil {
		return 0, err
	}
	return len(data), nil
}
