package relay

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

type replyThenCloseWriter struct {
	send *io.PipeWriter
	done <-chan struct{}
}

func (w replyThenCloseWriter) Write(data []byte) (int, error) {
	var request struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(data, &request); err != nil {
		return 0, err
	}
	if err := json.NewEncoder(w.send).Encode(map[string]any{"id": request.ID, "result": map[string]bool{"accepted": true}}); err != nil {
		return 0, err
	}
	w.send.Close()
	<-w.done
	return len(data), nil
}

func TestRPCPreservesCompleteResponseBeforePeerEOF(t *testing.T) {
	for i := 0; i < 64; i++ {
		reader, send := io.Pipe()
		rpc := NewRPC(reader, io.Discard, func() { reader.Close() })
		rpc.writer = replyThenCloseWriter{send: send, done: rpc.Done()}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var result struct{ Accepted bool }
		err := rpc.Call(ctx, "submit", nil, &result)
		cancel()
		rpc.Close()
		if err != nil || !result.Accepted {
			t.Fatalf("complete response lost at EOF: accepted=%t error=%v", result.Accepted, err)
		}
	}
}

func TestRPCPeerEOFWithoutResponseRemainsUnconfirmed(t *testing.T) {
	reader, send := io.Pipe()
	requests, writer := io.Pipe()
	rpc := NewRPC(reader, writer, func() { reader.Close(); writer.Close() })
	defer rpc.Close()
	go func() {
		defer requests.Close()
		var request map[string]any
		json.NewDecoder(requests).Decode(&request)
		send.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result struct{ Accepted bool }
	if err := rpc.Call(ctx, "submit", nil, &result); err == nil || result.Accepted {
		t.Fatal("missing receipt became accepted")
	}
}
