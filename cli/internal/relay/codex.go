package relay

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var ErrMixedOrigin = errors.New("本地输入介入了本轮，回复保留在本地。请先查看本地结果，再从飞书继续提问，不要重复执行任务")
var ErrHistoryIncomplete = errors.New("暂时无法确认这轮回复来自哪条消息，因此没有转发。请在本地查看原会话，不要重复提交任务")

type RPC struct {
	Events        chan json.RawMessage
	writer        io.Writer
	closer        func()
	mu            sync.Mutex
	writeMu       sync.Mutex
	pending       map[int64]chan envelope
	next          atomic.Int64
	done          chan struct{}
	once          sync.Once
	questionOwner string
}
type envelope struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("本地 Agent 请求失败（代码 %d）。请检查原会话和客户端是否正常，再核对连接状态", e.Code)
}
func NewRPC(reader io.Reader, writer io.Writer, closeFn func()) *RPC {
	r := &RPC{writer: writer, closer: closeFn, pending: map[int64]chan envelope{}, done: make(chan struct{}), Events: make(chan json.RawMessage, 512)}
	r.questionOwner = "daemon:" + uuid.NewString()
	go r.read(reader)
	return r
}
func (r *RPC) read(reader io.Reader) {
	defer r.Close()
	s := bufio.NewScanner(reader)
	s.Buffer(make([]byte, 65536), 16*1024*1024)
	for s.Scan() {
		var message struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id"`
		}
		if json.Unmarshal(s.Bytes(), &message) != nil {
			return
		}
		if message.Method != "" || len(message.ID) == 0 {

			select {
			case r.Events <- append(json.RawMessage(nil), s.Bytes()...):
			default:
				if len(message.ID) > 0 && string(message.ID) != "null" {
					return
				}
			}
			continue
		}
		var v envelope
		if json.Unmarshal(s.Bytes(), &v) != nil {
			return
		}
		r.mu.Lock()
		ch := r.pending[v.ID]
		r.mu.Unlock()
		if ch != nil {
			select {
			case ch <- v:
			default:
			}
		}
	}
}
func (r *RPC) Close() {
	r.once.Do(func() {
		close(r.done)
		if r.closer != nil {
			r.closer()
		}
	})
}
func (r *RPC) write(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, r.Close)
	defer stop()
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-r.done:
		return errors.New("与本地 Agent 的连接已关闭。请打开原客户端，检查后重新连接飞书")
	default:
	}
	n, err := r.writer.Write(data)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || n != len(data) {
		r.Close()
		return errors.New("暂时无法连接 Codex。请打开原 Codex 客户端，让本地 Agent 核对连接状态")
	}
	return nil
}

func (r *RPC) Call(ctx context.Context, method string, params any, dst any) error {
	id := r.next.Add(1)
	ch := make(chan envelope, 1)
	data, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.pending[id] = ch
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	if err = r.write(ctx, append(data, '\n')); err != nil {
		return err
	}
	decodeResponse := func(v envelope) error {
		if v.Error != nil {
			return v.Error
		}
		if dst != nil {
			return json.Unmarshal(v.Result, dst)
		}
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.done:
		select {
		case v := <-ch:
			return decodeResponse(v)
		default:
			return errors.New("与本地 Agent 的连接已关闭。请打开原客户端，检查后重新连接飞书")
		}
	case v := <-ch:
		return decodeResponse(v)
	}
}
func (r *RPC) Initialize(ctx context.Context) error {
	if err := r.Call(ctx, "initialize", map[string]any{"clientInfo": map[string]string{"name": "remote-agent-session_session_relay", "version": "0.1"}, "capabilities": map[string]bool{"experimentalApi": true}}, nil); err != nil {
		return err
	}
	return r.write(ctx, []byte("{\"method\":\"initialized\"}\n"))
}
func OpenCodex(ctx context.Context, binary string) (*Codex, error) {
	if binary == "" {
		binary = "codex"
	}
	if socket := os.Getenv("REMOTE_AGENT_SESSION_CODEX_SOCKET"); socket != "" {
		return openControlSocket(ctx, socket)
	}
	return openIsolatedCodex(ctx, binary)
}

func openIsolatedCodex(ctx context.Context, binary string) (*Codex, error) {
	if binary == "" {
		binary = "codex"
	}
	cmd := exec.Command(binary, "app-server", "--stdio")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		return nil, errors.New("无法启动 Codex 连接。请让本地 Agent 检查 Codex 是否已安装、可运行并已登录，再连接飞书")
	}
	r := NewRPC(out, in, func() { in.Close(); cmd.Process.Kill() })
	go func() { cmd.Wait(); r.Close() }()
	if err = r.Initialize(ctx); err != nil {
		r.Close()
		return nil, err
	}
	return &Codex{RPC: r}, nil
}

type Codex struct {
	RPC *RPC
}
type Thread struct {
	Source     string `json:"source"`
	Originator string `json:"originator"`
	ID         string `json:"id"`
	Name       string `json:"name"`
	Cwd        string `json:"cwd"`
	Status     struct {
		Type string `json:"type"`
	} `json:"status"`
}
type Item struct {
	ClientUserMessageID string `json:"clientUserMessageId"`
	Status              string `json:"status"`
	Type                string `json:"type"`
	ClientID            string `json:"clientId"`
	Text                string `json:"text"`
	Phase               string `json:"phase"`
}
type Turn struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	ItemsView   string `json:"itemsView"`
	Items       []Item `json:"items"`
	CompletedAt *int64 `json:"completedAt"`
	StartedAt   int64  `json:"startedAt"`
}
type Outcome struct {
	UnverifiedText string
	OriginIDs      []string
	Found          bool
	Complete       bool
	TurnID         string
	Text           string
	Status         string
	Mixed          bool
}

func (c *Codex) Inspect(ctx context.Context, id string) (Thread, error) {
	var r struct {
		Thread Thread `json:"thread"`
	}
	err := c.RPC.Call(ctx, "thread/read", map[string]any{"threadId": id, "includeTurns": false}, &r)
	if err == nil && (r.Thread.ID != id || r.Thread.Cwd == "") {
		err = errors.New("无法确认目标会话。请在原 Codex 会话内重新发起连接，不要猜测会话编号")
	}
	return r.Thread, err
}
func (c *Codex) Enqueue(ctx context.Context, thread, id, text string) (string, error) {
	var r struct {
		Submission struct {
			ID       string `json:"id"`
			ClientID string `json:"clientUserMessageId"`
		} `json:"queuedSubmission"`
	}
	err := c.RPC.Call(ctx, "thread/queue/add", map[string]any{"threadId": thread, "clientUserMessageId": id, "input": []map[string]any{{"type": "text", "text": text}}}, &r)
	if err == nil && (r.Submission.ID == "" || r.Submission.ClientID != id) {
		err = errors.New("Codex 未确认是否接收这条消息。请先在本地核对原会话，不要直接重发")
	}
	return r.Submission.ID, err
}
func (c *Codex) Queued(ctx context.Context, thread, id string) (bool, error) {
	cursor := ""
	for page := 0; page < 20; page++ {
		var r struct {
			Data []struct {
				ClientID string `json:"clientUserMessageId"`
			} `json:"data"`
			Next *string `json:"nextCursor"`
		}
		args := map[string]any{"threadId": thread, "limit": 100}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if err := c.RPC.Call(ctx, "thread/queue/list", args, &r); err != nil {
			return false, err
		}
		for _, item := range r.Data {
			if item.ClientID == id {
				return true, nil
			}
		}
		if r.Next == nil || *r.Next == "" {
			return false, nil
		}
		cursor = *r.Next
	}
	return false, ErrHistoryIncomplete
}
func (item Item) sourceID() (string, bool) {
	if item.Type == "userMessage" {
		return item.ClientID, true
	}
	if item.Type == "steeringUserMessage" && item.Status == "accepted" {
		return item.ClientUserMessageID, true
	}
	return "", false
}

func classify(t Turn, id string) (Outcome, error) {
	o := Outcome{TurnID: t.ID, Status: t.Status, Complete: t.CompletedAt != nil && (t.Status == "completed" || t.Status == "failed" || t.Status == "interrupted")}
	if t.ItemsView != "full" {
		return o, ErrHistoryIncomplete
	}
	var text []string
	for _, item := range t.Items {
		if source, ok := item.sourceID(); ok {
			o.OriginIDs = append(o.OriginIDs, source)
			if source == id {
				o.Found = true
			} else {
				o.Mixed = true
			}
		}
		if item.Type == "agentMessage" && (item.Phase == "" || item.Phase == "final_answer") {
			text = append(text, item.Text)
		}
	}
	if !o.Found {
		return Outcome{}, nil
	}
	if o.Complete && t.Status == "completed" {
		o.UnverifiedText = strings.Join(text, "\n\n")

	}
	if o.Mixed {
		return o, ErrMixedOrigin
	}
	o.Text = o.UnverifiedText
	o.UnverifiedText = ""
	return o, nil
}
func (c *Codex) CheckHistory(ctx context.Context, thread string) error {
	var history struct {
		Data []Turn `json:"data"`
	}
	unavailable := errors.New("无法读取原 Codex 的完整会话历史。请在电脑端检查原会话和客户端版本，再重新连接飞书")
	if err := c.RPC.Call(ctx, "thread/turns/list", map[string]any{"threadId": thread, "itemsView": "full", "limit": 1, "sortDirection": "desc"}, &history); err != nil || history.Data == nil {
		return unavailable
	}
	for _, turn := range history.Data {
		if turn.ItemsView != "full" {
			return unavailable
		}
	}
	return nil
}

func (c *Codex) Outcome(ctx context.Context, thread, id string, since time.Time) (Outcome, error) {
	cursor := ""
	for page := 0; page < 20; page++ {
		var r struct {
			Data []Turn  `json:"data"`
			Next *string `json:"nextCursor"`
		}
		args := map[string]any{"threadId": thread, "itemsView": "full", "limit": 50, "sortDirection": "desc"}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if err := c.RPC.Call(ctx, "thread/turns/list", args, &r); err != nil {
			return Outcome{}, err
		}
		for _, t := range r.Data {
			o, err := classify(t, id)
			if o.Found || err != nil {
				return o, err
			}
			if t.StartedAt > 0 && t.StartedAt < since.Add(-time.Minute).Unix() {
				return Outcome{}, nil
			}
		}
		if r.Next == nil || *r.Next == "" {
			return Outcome{}, nil
		}
		cursor = *r.Next
	}
	return Outcome{}, ErrHistoryIncomplete
}

func (c *Codex) CancelQueued(ctx context.Context, thread, id string) error {
	cursor := ""
	for page := 0; page < 20; page++ {
		var r struct {
			Data []struct {
				ID       string `json:"id"`
				ClientID string `json:"clientUserMessageId"`
			} `json:"data"`
			Next *string `json:"nextCursor"`
		}
		args := map[string]any{"threadId": thread, "limit": 100}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if err := c.RPC.Call(ctx, "thread/queue/list", args, &r); err != nil {
			return err
		}
		for _, item := range r.Data {
			if item.ClientID == id {
				return c.RPC.Call(ctx, "thread/queue/delete", map[string]any{"threadId": thread, "queuedSubmissionId": item.ID}, nil)
			}
		}
		if r.Next == nil || *r.Next == "" {
			return nil
		}
		cursor = *r.Next
	}
	return ErrHistoryIncomplete
}

func (r *RPC) Done() <-chan struct{} { return r.done }

func (c *Codex) OriginIDs(ctx context.Context, thread, turn string) ([]string, error) {
	cursor := ""
	for page := 0; page < 20; page++ {
		var response struct {
			Data []Turn  `json:"data"`
			Next *string `json:"nextCursor"`
		}
		args := map[string]any{"threadId": thread, "itemsView": "full", "limit": 50, "sortDirection": "desc"}
		if cursor != "" {
			args["cursor"] = cursor
		}
		if err := c.RPC.Call(ctx, "thread/turns/list", args, &response); err != nil {
			return nil, err
		}
		for _, item := range response.Data {
			if item.ID != turn {
				continue
			}
			if item.ItemsView != "full" || item.CompletedAt != nil {
				return nil, ErrHistoryIncomplete
			}
			ids := []string{}
			for _, message := range item.Items {
				if source, ok := message.sourceID(); ok {
					ids = append(ids, source)
				}
			}
			return ids, nil
		}
		if response.Next == nil || *response.Next == "" {
			return nil, ErrHistoryIncomplete
		}
		cursor = *response.Next
	}
	return nil, ErrHistoryIncomplete
}

const QuestionHookMatcher = "(^|[.])(request_user_input|request_user_input_async)$"
const FilePermissionHookMatcher = "(^|[.])request_permissions$"
const AttentionHookMatcher = "^mcp__(cua_repl(__|[.])js|data(__|[.])form_elicitation_v2_request)$"

func (c *Codex) RequireInteractionHooks(ctx context.Context, cwd, command string) error {
	var response struct {
		Data []struct {
			Cwd    string            `json:"cwd"`
			Errors []json.RawMessage `json:"errors"`
			Hooks  []struct {
				Event   string `json:"eventName"`
				Command string `json:"command"`
				Enabled bool   `json:"enabled"`
				Trust   string `json:"trustStatus"`
				Async   bool   `json:"async"`
				Matcher string `json:"matcher"`
				Timeout int    `json:"timeoutSec"`
			} `json:"hooks"`
		} `json:"data"`
	}
	if err := c.RPC.Call(ctx, "hooks/list", map[string]any{"cwds": []string{cwd}}, &response); err != nil {
		return errors.New("无法检查飞书权限和问答配置。请让本地 Agent 核对 Codex 版本是否支持 hooks/list；修复前继续在本地操作")
	}
	permission, question, attention := false, false, false
	for _, entry := range response.Data {
		if entry.Cwd != cwd || len(entry.Errors) != 0 {
			continue
		}
		for _, hook := range entry.Hooks {
			if hook.Command != command || !hook.Enabled || hook.Async || hook.Timeout < 310 || (hook.Trust != "trusted" && hook.Trust != "managed") {
				continue
			}
			if hook.Event == "permissionRequest" && hook.Matcher == ".*" {
				permission = true
			}
			if hook.Event == "preToolUse" && hook.Matcher == QuestionHookMatcher {
				question = true
			}
			if hook.Event == "preToolUse" && hook.Matcher == AttentionHookMatcher {
				attention = true
			}
		}
	}
	if !permission || !question {
		return errors.New("尚未配置飞书权限和问答交互。请让 Agent 用 remote-agent-session session hooks 生成配置，再由你在 Codex /hooks 中审阅并信任，完成后重新连接；无需开启 Full Access")
	}
	if !attention {
		return errors.New("尚未配置或信任本地确认预告。请让 Agent 合并新版 remote-agent-session session hooks 输出的预告处理器，在 Codex 审阅信任后重新连接")
	}
	return nil
}
