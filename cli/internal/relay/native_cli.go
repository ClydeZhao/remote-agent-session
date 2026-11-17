package relay

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"
)

func OpenCodexForThread(ctx context.Context, binary, id string) (*Codex, error) {
	observer, err := OpenCodex(ctx, binary)
	if err != nil {
		return nil, err
	}
	if id == "" {
		return observer, nil
	}
	thread, err := observer.Inspect(ctx, id)
	if err != nil || thread.ID != id {
		observer.RPC.Close()
		if err != nil {
			return nil, err
		}
		return nil, ErrInterruptUnavailable
	}
	if os.Getenv("REMOTE_AGENT_SESSION_CODEX_SOCKET") != "" {
		if !loadedThreadStatus(thread.Status.Type) {
			observer.RPC.Close()
			return nil, errors.New("指定的 Codex 后台尚未加载当前会话。请退出原终端会话后，用 codex --remote 接入该后台并恢复同一会话，再连接飞书")
		}
		return observer, nil
	}
	fallback := func() (*Codex, error) {
		if thread.Status.Type != "notLoaded" && thread.Status.Type != "" {
			return observer, nil
		}
		desktop, err := openDesktop(ctx)
		if err == nil {
			defer desktop.conn.Close()
			var owner string
			owner, err = desktop.call("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": id}, "", nil)
			if err == nil && owner != "" {
				return observer, nil
			}
		}
		observer.RPC.Close()
		return nil, ErrInterruptUnavailable
	}
	if binary == "" {
		binary = "codex"
	}
	versionCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	data, err := exec.CommandContext(versionCtx, binary, "app-server", "daemon", "version").Output()
	cancel()
	var daemon struct {
		Status string `json:"status"`
		Socket string `json:"socketPath"`
	}
	if err != nil || json.Unmarshal(data, &daemon) != nil || daemon.Status != "running" || daemon.Socket == "" {
		return fallback()
	}
	ownerCtx, ownerCancel := context.WithTimeout(ctx, 3*time.Second)
	defer ownerCancel()
	owner, err := openControlSocket(ownerCtx, daemon.Socket)
	if err != nil {
		return fallback()
	}
	actual, err := owner.Inspect(ownerCtx, id)
	if err != nil || actual.ID != id || actual.Cwd != thread.Cwd || actual.Status.Type == "notLoaded" || actual.Status.Type == "" {
		owner.RPC.Close()
		return fallback()
	}
	observer.RPC.Close()
	return owner, nil
}

func (c *Codex) InspectRelayTarget(ctx context.Context, id string) (Thread, error) {
	thread, err := c.Inspect(ctx, id)
	if err == nil && (thread.ID != id || (thread.Status.Type != "notLoaded" && !loadedThreadStatus(thread.Status.Type))) {
		err = errors.New("无法确认原会话的当前状态。请在原客户端打开该会话并完成启动提示，再重新连接")
	}
	return thread, err
}

func loadedThreadStatus(status string) bool {
	return status == "idle" || status == "active" || status == "systemError"
}
