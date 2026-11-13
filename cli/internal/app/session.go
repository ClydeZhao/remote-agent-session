package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"github.com/google/uuid"
)

type sessionMessage struct {
	DispatchedAt *time.Time `json:"dispatched_at"`
	ID           string     `json:"id"`
	Generation   int64      `json:"generation"`
	Kind         string     `json:"kind"`
	Content      string     `json:"content"`
	State        string     `json:"state"`
	CreatedAt    time.Time  `json:"created_at"`
}
type sessionCancellation struct {
	RuntimeKind     string `json:"runtime_kind"`
	RuntimeInstance string `json:"runtime_instance"`
	ID              string `json:"id"`
	NativeSession   string `json:"native_session"`
}

type sessionView struct {
	RuntimeKind       string                `json:"runtime_kind"`
	RuntimeInstance   string                `json:"runtime_instance"`
	AsyncInteractions []string              `json:"async_interactions"`
	Guidance          string                `json:"guidance"`
	UserID            string                `json:"user_id"`
	DeliveryWarning   bool                  `json:"delivery_warning"`
	Cancellations     []sessionCancellation `json:"cancellations"`
	NativeSession     string                `json:"native_session"`
	DeviceName        string                `json:"device_name"`
	Project           string                `json:"project"`
	SessionName       string                `json:"session_name"`
	Generation        int64                 `json:"generation"`
	Desired           bool                  `json:"desired"`
	Status            string                `json:"status"`
	Online            bool                  `json:"online"`
	Messages          []sessionMessage      `json:"messages"`
}

func (r *remote) relayCall(ctx context.Context, method, path string, body, dst any) error {
	var b io.Reader
	if body != nil {
		data, e := json.Marshal(body)
		if e != nil {
			return e
		}
		b = bytes.NewReader(data)
	}
	req, e := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.config.Server, "/")+"/v1/session-relay"+path, b)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+string(r.token))
	req.Header.Set("X-Remote-Agent-Session-Relay-Runtime", "1")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, e := r.client.Client.Do(req)
	err := decode(resp, e, dst, "same_request")
	var f *failure
	if errors.As(err, &f) {
		f.detail.Message = relayErrorMessage(f.detail.Code)
	}
	return err
}

func relayErrorMessage(code string) string {
	switch code {
	case "SESSION_CLIENT_UPGRADE_REQUIRED":
		return "当前会话需要新版 Remote Agent Session CLI。请在原应用更新客户端后重新连接。"
	case "SESSION_NOT_ATTACHED":
		return "尚未连接会话。请在本地 Agent 会话里对 Agent 说「把当前会话连到飞书」。"
	case "SESSION_OTHER_DEVICE":
		return "当前登录无法操作这条连接。请在已连接的原会话操作；如需改连当前会话，请让本地 Agent 连接飞书。"
	case "SESSION_CHANGED":
		return "当前连接已变化，本次更新未生效。请让 Agent 查看 session status，确认当前目标；原任务的结果请到原会话核对。"
	case "SESSION_DETACHED":
		return "飞书对话已断开，本次更新未生效。需要继续时，请在飞书发送 /session resume；电脑离线时请让本地 Agent 重新连接。"
	case "SESSION_REQUEST_CONFLICT":
		return "同一请求编号对应的内容发生冲突，本次未受理。请保留原编号和原内容，让本地 Agent 核对原请求，不要重复执行或授权。"
	case "SESSION_REQUEST_INVALID":
		return "找不到这个问题。请查看最新卡片，使用其中的请求编号；没有新卡片时先查看原会话回复。"
	case "SESSION_INTERACTION_UNDISPLAYABLE":
		return "请求包含飞书会识别为提及的内容，无法完整展示。请回原会话核对后再发起。"
	case "SESSION_INTERACTION_TOO_LARGE":
		return "请求无法完整放进一张飞书卡片。请回原会话核对后再发起。"
	case "SESSION_ORIGIN_INVALID":
		return "这不是当前飞书对话的请求，未发起远程交互。请在原会话处理。"
	case "SESSION_MESSAGE_INACTIVE":
		return "当前连接已不再接收这条消息的更新，本次更新未生效。请到原会话核对结果，不要直接重发任务。"
	case "SESSION_MESSAGE_INVALID":
		return "无法在当前连接中操作这条消息，本次更新未生效。请用原登录身份到原会话核对结果。"
	case "SESSION_RESULT_CONFLICT":
		return "同一条消息的结果发生冲突，未覆盖已保存的结果。请在本地核对原会话，不要重新执行任务。"
	case "SESSION_EMPTY_OUTPUT":
		return "没有可发送的回答正文。请让本地 Agent 核对原会话结果，不要重新执行任务。"
	case "SESSION_CANCELLATION_NOT_REQUESTED":
		return "这条消息没有待处理的撤回请求。请先查看原会话结果，不要重复提交或撤回。"
	case "AUTH_REQUIRED", "AUTH_INVALID":
		return "当前登录已失效。请在本地让 Agent 帮你重新登录 Remote Agent Session，再连接原会话。"
	default:
		return "无法确认这次会话操作的结果。请先在原会话核对，并让本地 Agent 检查连接；核对前不要重复提交任务。"
	}
}
func sessionCommand(p output.Printer, r *remote, dir string, args []string) int {
	if len(args) == 0 {
		return usage(p)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var view sessionView
	switch args[0] {
	case "attach":
		f := flags("session attach")
		runtime := f.String("agent", currentRuntime(), "agent runtime")
		id := f.String("session", "", "current native session")
		name := f.String("name", "", "session display name")
		if f.Parse(args[1:]) != nil || f.NArg() != 0 {
			return usage(p)
		}
		if *id == "" {
			*id = currentNativeSession(*runtime)
		}
		if *id == "" {
			return p.Failure(output.Error{Code: "SESSION_ID_REQUIRED", Category: "usage", Message: "请在目标 Agent 的当前会话中执行，或显式指定当前会话 ID。", RetryAction: "user_action"}, 2)
		}
		if _, e := uuid.Parse(*id); *runtime == "codex" && e != nil {
			return p.Failure(output.Error{Code: "SESSION_ID_REQUIRED", Category: "usage", Message: "请在 Codex 会话中执行，或用 --session 指定当前会话 ID。", RetryAction: "user_action"}, 2)
		}
		c, instance, e := openRelayRuntime(ctx, r.store, *runtime, "", *id)
		if e != nil {
			return report(p, e)
		}
		thread, e := c.InspectRelayTarget(ctx, *id)
		if e == nil {
			_, e = c.Queued(ctx, *id, "capability-check")
		}
		if e == nil {
			e = c.CheckHistory(ctx, *id)
		}
		if codex, ok := c.(*relay.Codex); e == nil && ok {
			command, commandErr := relayHookCommand(dir)
			if commandErr != nil {
				e = commandErr
			} else {
				e = codex.RequireInteractionHooks(ctx, thread.Cwd, command)
			}
		}
		if dsh, ok := c.(*relay.DSH); e == nil && ok {
			e = dsh.CheckInteractions(ctx)
		}
		c.Close()
		if e != nil {
			return report(p, e)
		}
		*name = relaySessionName(*name, thread.Name, *runtime)
		host, e := deviceName()
		if e != nil {
			return report(p, e)
		}
		body := map[string]any{"native_session": thread.ID, "device_name": host, "project": filepath.Base(thread.Cwd), "session_name": *name, "runtime_kind": *runtime, "runtime_instance": instance}
		if e = r.relayCall(ctx, "POST", "/attach", body, &view); e != nil {
			return report(p, e)
		}
		if *runtime != "codex" && (view.RuntimeKind != *runtime || view.RuntimeInstance != instance || view.NativeSession != thread.ID) {
			return report(p, errors.New("服务端尚未确认原 Agent 的身份，请检查 Remote Agent Session 服务端与客户端版本后重试"))
		}
		controller, e := ensureRelay(r, dir)
		if e != nil {
			return report(p, e)
		}
		readyCtx, readyCancel := context.WithTimeout(ctx, 10*time.Second)
		defer readyCancel()
		view, e = waitRelayReady(readyCtx, r, view, controller)
		if e != nil {
			return report(p, e)
		}
		return p.Result(view, "已将会话「"+output.Clean(view.SessionName)+"」连接到飞书。接下来可以打开飞书，继续和我聊。")
	case "status":
		if len(args) != 1 {
			return usage(p)
		}
		if e := r.relayCall(ctx, "GET", "", nil, &view); e != nil {
			return report(p, e)
		}
		guidance := view.Guidance
		if guidance == "" {
			guidance = "服务端未提供连接说明，请让本地 Agent 检查 CLI 与服务端版本是否匹配。"
		}
		return p.Result(view, output.Clean(view.DeviceName+" · "+view.Project+" · "+view.SessionName)+"："+output.Clean(guidance))
	case "detach":
		if len(args) != 1 {
			return usage(p)
		}
		if e := r.relayCall(ctx, "GET", "", nil, &view); e != nil {
			return report(p, e)
		}
		if e := r.relayCall(ctx, "POST", "/detach", map[string]any{"generation": view.Generation}, &view); e != nil {
			return report(p, e)
		}
		guidance := view.Guidance
		if guidance == "" {
			guidance = "已断开飞书对话，已开始的工作继续运行。需要继续时，在飞书发送 /session resume。"
		}
		return p.Result(view, output.Clean(guidance))
	case "_serve":
		if len(args) != 1 {
			return usage(p)
		}
		return serveRelay(r, dir)
	default:
		return usage(p)
	}
}
func waitRelayReady(ctx context.Context, r *remote, expected sessionView, controllers ...string) (sessionView, error) {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		var view sessionView
		if err := r.relayCall(ctx, "GET", "", nil, &view); err != nil {
			if ctx.Err() != nil {
				break
			}
			return view, err
		}
		if view.Generation != expected.Generation || view.NativeSession != expected.NativeSession || view.RuntimeKind != expected.RuntimeKind || view.RuntimeInstance != expected.RuntimeInstance || !view.Desired {
			return view, fail("SESSION_CHANGED", "user_action", "连接目标已变化，本次没有确认连接成功。请先让 Agent 查看 session status，确认目标后再连接。", "user_action", exitRemote)
		}
		if view.Status == "ready" && view.Online {
			witnessed := len(controllers) == 0
			if len(controllers) > 0 && r.store != nil {
				ready, err := r.store.RelayReadiness()
				witnessed = err == nil && ready.Controller == controllers[0] && ready.Generation == view.Generation && ready.RuntimeKind == view.RuntimeKind && ready.RuntimeInstance == view.RuntimeInstance && ready.NativeSession == view.NativeSession
				owner, ownerErr := r.store.RelayOwner()
				intent, intentErr := r.store.RelayIntent()
				witnessed = witnessed && ownerErr == nil && intentErr == nil && owner == controllers[0] && intent == controllers[0]
				unlock, lockErr := r.store.RelayLock()
				if lockErr == nil {
					unlock()
					witnessed = false
				} else if !errors.Is(lockErr, localstate.ErrRelayRunning) {
					witnessed = false
				}
			}
			if witnessed {
				return view, nil
			}
		}
		if view.Status == "unavailable" {
			guidance := view.Guidance
			if guidance == "" {
				guidance = "还没连上原会话。请在本地打开原 Agent 会话，让 Agent 检查后重新连接飞书。"
			}
			return view, fail("SESSION_UNAVAILABLE", "user_action", guidance, "user_action", exitRemote)
		}
		select {
		case <-ctx.Done():
			return view, fail("SESSION_CONNECTING", "pending", "尚未收到连接确认，现在还不能确认可在飞书继续。请让本地 Agent 检查连接状态；确认尚未连上时再重试连接。", "user_action", exitPending)
		case <-ticker.C:
		}
	}
	return expected, fail("SESSION_CONNECTING", "pending", "尚未收到连接确认，现在还不能确认可在飞书继续。请让本地 Agent 检查连接状态；确认尚未连上时再重试连接。", "user_action", exitPending)
}

func confirmRelayReady(ctx context.Context, r *remote, c relay.Session, view sessionView, controllers ...string) error {
	checkCtx, checkCancel := context.WithTimeout(ctx, 3*time.Second)
	_, err := c.InspectRelayTarget(checkCtx, view.NativeSession)
	if err == nil {
		_, err = c.Queued(checkCtx, view.NativeSession, "capability-check")
	}
	if err == nil {
		err = c.CheckHistory(checkCtx, view.NativeSession)
	}
	if dsh, ok := c.(*relay.DSH); err == nil && ok {
		err = dsh.CheckInteractions(checkCtx)
	}
	checkCancel()
	resultCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var result sessionView
	if callErr := r.relayCall(resultCtx, "POST", "/readiness", map[string]any{"generation": view.Generation, "available": err == nil}, &result); callErr != nil {
		return callErr
	}
	if err != nil {
		return err
	}
	if len(controllers) > 0 && r.store != nil && result.Status == "ready" && result.Generation == view.Generation && result.Desired {
		return r.store.SaveRelayReadiness(localstate.RelayReadiness{Controller: controllers[0], Generation: view.Generation, RuntimeKind: view.RuntimeKind, RuntimeInstance: view.RuntimeInstance, NativeSession: view.NativeSession})
	}
	return nil
}

func ensureRelay(r *remote, dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = localstate.DefaultDir()
		if err != nil {
			return "", err
		}
	}
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	base, err := relayIdentity(r, binary)
	if err != nil {
		return "", err
	}
	activation := uuid.NewString()
	identity := base + ":" + activation
	if err := r.store.SaveRelayIntent(identity); err != nil {
		return "", err
	}
	cmd := relayCommand(binary, dir, activation)
	if err := cmd.Start(); err != nil {
		return "", errors.New("无法启动本地会话连接程序。请让本地 Agent 检查安装和运行权限后重新连接")
	}
	return identity, cmd.Process.Release()
}

func serveRelay(r *remote, dir string) int {
	binary, err := os.Executable()
	if err != nil {
		return exitRemote
	}
	identity, executable, err := readRelayIdentity(r, binary)
	if activation := os.Getenv("REMOTE_AGENT_SESSION_RELAY_ACTIVATION"); activation != "" {
		identity += ":" + activation
	}
	if err != nil {
		return exitRemote
	}
	var unlock func()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		unlock, err = r.store.RelayLock()
		if err == nil {
			break
		}
		if !errors.Is(err, localstate.ErrRelayRunning) {
			return exitRemote
		}
		owner, e := r.store.RelayOwner()
		if e != nil {
			return exitRemote
		}
		if owner == identity {
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}
	if err != nil {
		return exitRemote
	}
	defer unlock()
	if intent, err := r.store.RelayIntent(); err != nil || intent != identity {
		return 0
	}
	if r.store.SaveRelayOwner(identity) != nil {
		return exitRemote
	}
	upgrade := relayUpgradeCheck(r, dir, binary, identity, executable)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var lastView sessionView
	recoveries := map[string]*relayMessageRecovery{}
	var handover atomic.Bool
	var cancellationView atomic.Pointer[sessionView]
	defer func() {
		if handover.Load() {
			_ = r.store.AddRelayHandoffs(relayHandoffs(r.config.Server, lastView))
		}
	}()
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		watchRelayIntent(ctx, r, identity, func(revoke bool) { handover.Store(revoke); stop() })
	}()
	handoffsDone := make(chan struct{})
	go func() {
		defer close(handoffsDone)
		retryRelayHandoffs(ctx, r, &cancellationView)
	}()
	defer func() { stop(); <-watcherDone; <-handoffsDone }()
	for {
		if ctx.Err() != nil || upgrade() {
			return 0
		}
		openCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		var initial sessionView
		if err := r.relayCall(openCtx, "GET", "", nil, &initial); err != nil {
			var f *failure
			if errors.As(err, &f) && f.exit == exitAuth {
				cancel()
				handover.Store(true)
				return 0
			}
		}
		c, _, e := openRelayRuntime(openCtx, r.store, initial.RuntimeKind, initial.RuntimeInstance, initial.NativeSession)
		connectedThread := initial.NativeSession
		cancel()
		if e != nil {
			time.Sleep(5 * time.Second)
			continue
		}
		aliveCtx, stopAlive := context.WithCancel(ctx)
		aliveDone := make(chan struct{})
		summaryWake := make(chan struct{}, 1)
		go func() {
			defer close(aliveDone)
			questionsDone := make(chan struct{})
			go func() {
				defer close(questionsDone)
				if native, ok := c.(nativeQuestions); ok {
					runNativeQuestions(aliveCtx, r, native, &cancellationView)
				}
				if dsh, ok := c.(*relay.DSH); ok {
					runDSHInteractions(aliveCtx, r, dsh, &cancellationView)
				}
			}()
			summariesDone := make(chan struct{})
			go func() {
				defer close(summariesDone)
				runRelaySummaries(aliveCtx, r, c, initial, &cancellationView, relay.Summarize, summaryWake)
			}()
			keepRelayAlive(aliveCtx, r, c.Done())
			stopAlive()
			<-questionsDone
			<-summariesDone
		}()
		var readyGeneration int64
		for {
			select {
			case <-ctx.Done():
				if handover.Load() && retireRelay(r, c, lastView) {
					lastView = sessionView{}
				}
				stopAlive()
				<-aliveDone
				c.Close()
				return 0
			case <-c.Done():
				stopAlive()
				goto reconnect
			default:
			}
			if upgrade() {
				stopAlive()
				<-aliveDone
				c.Close()
				return 0
			}
			var view sessionView
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			e = r.relayCall(callCtx, "POST", "/poll", nil, &view)
			cancel()
			if e != nil {
				var f *failure
				if errors.As(e, &f) && (f.exit == exitAuth || f.detail.Code == "SESSION_OTHER_DEVICE") {
					handover.Store(true)
					if retireRelay(r, c, lastView) {
						lastView = sessionView{}
					}
					stopAlive()
					<-aliveDone
					c.Close()
					return 0
				}
				break
			}
			lastView = view
			cancellationView.Store(&view)
			if view.Desired && (view.NativeSession != connectedThread || view.RuntimeKind != initial.RuntimeKind || view.RuntimeInstance != initial.RuntimeInstance) {
				stopAlive()
				goto reconnect
			}
			if view.Desired && (view.Status == "connecting" || view.Status == "ready") && readyGeneration != view.Generation {
				readyCtx, readyCancel := context.WithTimeout(ctx, 5*time.Second)
				if confirmRelayReady(readyCtx, r, c, view, identity) == nil {
					readyGeneration = view.Generation
				}
				readyCancel()
				time.Sleep(200 * time.Millisecond)
				continue
			}
			if view.Status == "replaced" && len(view.Cancellations) == 0 {
				stopAlive()
				<-aliveDone
				c.Close()
				return 0
			}
			if view.Desired && view.Status == "ready" {
				next := make(map[string]*relayMessageRecovery, len(view.Messages))
				for _, message := range view.Messages {
					if ctx.Err() != nil || upgrade() {
						break
					}
					recovery := recoveries[message.ID]
					if recovery == nil {
						recovery = &relayMessageRecovery{}
					}
					processRelayMessage(ctx, r, c, view, message, recovery, summaryWake)
					next[message.ID] = recovery
				}
				recoveries = next
			}
			time.Sleep(2 * time.Second)
		}
	reconnect:
		stopAlive()
		<-aliveDone
		c.Close()
		time.Sleep(5 * time.Second)
	}
}

const relayObservationGrace = 30 * time.Second

type relayMessageRecovery struct {
	originIDs              []string
	observationFailedSince time.Time
	terminalFailure        string
}

func (r *relayMessageRecovery) observationFailed() bool {
	if r.observationFailedSince.IsZero() {
		r.observationFailedSince = time.Now()
	}
	return time.Since(r.observationFailedSince) >= relayObservationGrace
}

func dispatchRelayMessage(ctx context.Context, r *remote, c relay.Session, view sessionView, msg sessionMessage) string {
	authorize := func() error {
		var current sessionView
		if err := r.relayCall(ctx, "GET", "", nil, &current); err != nil {
			return err
		}
		if !current.Desired || !current.Online || current.Status != "ready" || current.Generation != msg.Generation || current.NativeSession != view.NativeSession || current.RuntimeKind != view.RuntimeKind || current.RuntimeInstance != view.RuntimeInstance {
			return relay.ErrInterruptUnavailable
		}
		return nil
	}
	if dsh, ok := c.(*relay.DSH); ok {
		if authorize() != nil || dsh.BindInteractions(ctx, view.NativeSession, view.Generation, dshOrigins(view)) != nil {
			return "not_submitted"
		}
	}
	if err := c.InterruptActive(ctx, view.NativeSession, authorize); err != nil {
		return "interrupt_unavailable"
	}
	if authorize() != nil {
		return "not_submitted"
	}
	if err := c.StartRemoteInput(ctx, view.NativeSession, msg.ID, msg.Content, authorize); errors.Is(err, relay.ErrRemoteInputNotSubmitted) {
		return "not_submitted"
	}
	return ""
}

func processRelayMessage(parent context.Context, r *remote, c relay.Session, view sessionView, msg sessionMessage, recovery *relayMessageRecovery, summarySignals ...chan<- struct{}) {
	ctx, cancel := context.WithTimeout(parent, 45*time.Second)
	defer cancel()
	path := "/messages/" + msg.ID
	if msg.State == "pending" && recovery.terminalFailure == "" {
		var claim struct {
			Dispatch bool `json:"dispatch"`
		}
		if r.relayCall(ctx, "POST", path+"/dispatch", map[string]any{"generation": msg.Generation}, &claim) != nil {
			return
		}
		if claim.Dispatch {
			recovery.terminalFailure = dispatchRelayMessage(ctx, r, c, view, msg)
		}
	}
	state, text := recovery.terminalFailure, ""
	if state == "" {
		readCtx, readCancel := context.WithTimeout(parent, 5*time.Second)
		state, text = observeRelayMessage(readCtx, r, c, view, msg, recovery)
		readCancel()
	}
	if state == "" {
		return
	}
	if state == "completed" && strings.TrimSpace(text) == "" {
		state = "empty_output"
		text = ""
	}
	if state == "completed" && utf8.RuneCountInString(text) > 262144 {
		state = "output_too_large"
		text = ""
	}
	summaryPending := state == "completed" && (msg.Kind == "chat" || msg.Kind == "async_answer") && relay.NeedsSummary(text)
	if summaryPending && len(summarySignals) > 0 {
		defer func() {
			select {
			case summarySignals[0] <- struct{}{}:
			default:
			}
		}()
	}
	body := map[string]any{"generation": msg.Generation, "outcome": state, "text": text, "summary": "", "summary_pending": summaryPending}
	if len(recovery.originIDs) > 0 {
		body["message_ids"] = recovery.originIDs
	}
	var result map[string]bool
	resultCtx, resultCancel := context.WithTimeout(parent, 15*time.Second)
	defer resultCancel()
	if r.relayCall(resultCtx, "POST", path+"/result", body, &result) != nil {
		return
	}

}

func observeRelayMessage(ctx context.Context, r *remote, c relay.Session, view sessionView, msg sessionMessage, recovery *relayMessageRecovery) (string, string) {
	outcome, err := c.Outcome(ctx, view.NativeSession, msg.ID, msg.CreatedAt)
	recovery.originIDs = outcome.OriginIDs
	if errors.Is(err, relay.ErrMixedOrigin) {
		var origin struct {
			Scope string `json:"scope"`
		}
		if e := r.relayCall(ctx, "POST", "/origin", map[string]any{"generation": msg.Generation, "native_session": view.NativeSession, "message_ids": outcome.OriginIDs, "for_result": true}, &origin); e != nil {
			if recovery.observationFailed() {
				return "unknown", ""
			}
			return "", ""
		}
		if origin.Scope != "remote" {
			return "mixed", ""
		}
		outcome.Text = outcome.UnverifiedText
		outcome.UnverifiedText = ""
		err = nil
	}
	if err != nil {
		if recovery.observationFailed() {
			return "unknown", ""
		}
		return "", ""
	}
	if outcome.Found {
		recovery.observationFailedSince = time.Time{}
		if !outcome.Complete {
			var result map[string]bool
			_ = r.relayCall(ctx, "POST", "/messages/"+msg.ID+"/working", map[string]any{"generation": msg.Generation}, &result)
			return "", ""
		}
		switch outcome.Status {
		case "completed":
			return "completed", outcome.Text
		case "interrupted":
			return "interrupted", ""
		default:
			return "failed", ""
		}
	}
	queued, err := c.Queued(ctx, view.NativeSession, msg.ID)
	if err != nil {
		if recovery.observationFailed() {
			return "unknown", ""
		}
		return "", ""
	}
	recovery.observationFailedSince = time.Time{}
	if !queued && msg.DispatchedAt != nil && time.Since(*msg.DispatchedAt) >= relayObservationGrace {
		return "unknown", ""
	}
	return "", ""
}

func keepRelayAlive(ctx context.Context, r *remote, disconnected <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-disconnected:
			return
		case <-ticker.C:
			requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			var view sessionView
			_ = r.relayCall(requestCtx, "POST", "/poll", nil, &view)
			cancel()
		}
	}
}

func relayIdentity(r *remote, binary string) (string, error) {
	identity, _, err := readRelayIdentity(r, binary)
	return identity, err
}

func readRelayIdentity(r *remote, binary string) (string, os.FileInfo, error) {
	executable, err := os.Open(binary)
	if err != nil {
		return "", nil, err
	}
	defer executable.Close()
	info, err := executable.Stat()
	if err != nil {
		return "", nil, err
	}
	build := sha256.New()
	if _, err := io.Copy(build, executable); err != nil {
		return "", nil, err
	}
	h := sha256.New()
	h.Write([]byte(r.config.Server))
	h.Write([]byte{0})
	h.Write(r.token)
	return hex.EncodeToString(h.Sum(nil)) + ":" + relayRuntimeIdentity() + ":" + hex.EncodeToString(build.Sum(nil)), info, nil
}

func relayRuntimeIdentity() string {
	home := os.Getenv("CODEX_HOME")
	if home == "" {
		userHome, _ := os.UserHomeDir()
		home = filepath.Join(userHome, ".codex")
	}
	h := sha256.New()
	for _, value := range []string{home, os.Getenv("REMOTE_AGENT_SESSION_CODEX_SOCKET"), currentRuntime(), os.Getenv("DSH_HOME"), os.Getenv("PATH")} {
		h.Write([]byte{0})
		h.Write([]byte(value))
	}
	return hex.EncodeToString(h.Sum(nil))
}

func watchRelayIntent(ctx context.Context, r *remote, identity string, stop func(bool)) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			intent, e := r.store.RelayIntent()
			config, configErr := r.store.Config()
			if e != nil || intent != identity || configErr != nil || config.Server != r.config.Server {
				sameLogin := e == nil && configErr == nil && config.Server == r.config.Server && strings.Split(intent, ":")[0] == strings.Split(identity, ":")[0]
				stop(!sameLogin)
				return
			}
		}
	}
}
func relayHandoffs(server string, view sessionView) []localstate.RelayHandoff {
	items := cancellationHandoffs(server, view)
	for _, msg := range view.Messages {
		items = append(items, localstate.RelayHandoff{Runtime: relayRuntimeIdentity(), Server: server, UserID: view.UserID, NativeSession: view.NativeSession, ClientID: msg.ID, RuntimeKind: view.RuntimeKind, RuntimeInstance: view.RuntimeInstance})
	}
	return items
}

func cancellationHandoffs(server string, view sessionView) []localstate.RelayHandoff {
	items := make([]localstate.RelayHandoff, 0, len(view.Cancellations))
	runtime := relayRuntimeIdentity()
	for _, item := range view.Cancellations {
		items = append(items, localstate.RelayHandoff{Runtime: runtime, Server: server, UserID: view.UserID, NativeSession: item.NativeSession, ClientID: item.ID, RuntimeKind: item.RuntimeKind, RuntimeInstance: item.RuntimeInstance})
	}
	return items
}

func retryRelayHandoffs(ctx context.Context, r *remote, latest *atomic.Pointer[sessionView]) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	open := func(ctx context.Context, item localstate.RelayHandoff) (relay.Session, error) {
		client, _, err := openRelayRuntime(ctx, r.store, item.RuntimeKind, item.RuntimeInstance, item.NativeSession)
		return client, err
	}
	for ctx.Err() == nil {
		_ = finishRelayHandoffs(ctx, r, latest.Load(), open)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func finishRelayHandoffs(ctx context.Context, r *remote, latest *sessionView, open func(context.Context, localstate.RelayHandoff) (relay.Session, error)) error {
	items, err := r.store.RelayHandoffs()
	if err != nil {
		return err
	}
	var current sessionView
	if latest != nil {
		current = *latest
	} else if len(items) > 0 {
		identityCtx, identityCancel := context.WithTimeout(ctx, 10*time.Second)
		identityErr := r.relayCall(identityCtx, "GET", "", nil, &current)
		identityCancel()
		if identityErr != nil {
			return identityErr
		}
	}
	persisted := make(map[localstate.RelayHandoff]bool, len(items))
	for _, item := range items {
		persisted[item] = true
	}
	if latest != nil {
		for _, item := range cancellationHandoffs(r.config.Server, current) {
			if !persisted[item] {
				items = append(items, item)
			}
		}
	}
	runtime := relayRuntimeIdentity()
	for _, item := range items {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if item.Server != r.config.Server || item.UserID != current.UserID || item.Runtime != runtime {
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if reconcileRelayHandoff(callCtx, r, item, open) && persisted[item] {
			_ = r.store.FinishRelayHandoff(item)
		}
		cancel()
	}
	return nil
}

func reconcileRelayHandoff(ctx context.Context, r *remote, item localstate.RelayHandoff, open func(context.Context, localstate.RelayHandoff) (relay.Session, error)) bool {
	path := "/messages/" + item.ClientID
	var status struct {
		Requested bool
		Settled   bool
	}
	if err := r.relayCall(ctx, "GET", path+"/cancellation", nil, &status); err != nil {
		return false
	}
	if !status.Requested {
		return status.Settled
	}
	c, err := open(ctx, item)
	if err != nil {
		return false
	}
	defer c.Close()
	if err := c.CancelQueued(ctx, item.NativeSession, item.ClientID); err != nil {
		return false
	}
	var result struct{ Accepted bool }
	return r.relayCall(ctx, "POST", path+"/cancelled", nil, &result) == nil && result.Accepted
}

func retireRelay(r *remote, c relay.Session, view sessionView) bool {
	if r.store.AddRelayHandoffs(relayHandoffs(r.config.Server, view)) != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, item := range cancellationHandoffs(r.config.Server, view) {
		if item.RuntimeKind != view.RuntimeKind || item.RuntimeInstance != view.RuntimeInstance || item.NativeSession != view.NativeSession {
			continue
		}
		if c.CancelQueued(ctx, item.NativeSession, item.ClientID) != nil {
			continue
		}
		var result struct{ Accepted bool }
		if r.relayCall(ctx, "POST", "/messages/"+item.ClientID+"/cancelled", nil, &result) == nil && result.Accepted {
			_ = r.store.FinishRelayHandoff(item)
		}
	}
	return true
}
