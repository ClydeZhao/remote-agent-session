package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/api"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"github.com/google/uuid"
)

const exitAuth = 4
const exitNetwork = 5
const exitPending = 6
const exitRemote = 7
const exitIncomplete = 8

type failure struct {
	detail output.Error
	exit   int
}

func (f *failure) Error() string { return f.detail.Message }
func fail(code, category, message, action string, exit int) *failure {
	return &failure{output.Error{Code: code, Category: category, Message: message, RetryAction: action}, exit}
}
func report(p output.Printer, err error) int {
	var f *failure
	if errors.As(err, &f) {
		return p.Failure(f.detail, f.exit)
	}
	return localFailure(p, err)
}

type remote struct {
	client *api.Client
	store  *localstate.Store
	config localstate.Config
	token  []byte
}

func connect(dir string, timeout time.Duration) (*remote, error) {
	if timeout <= 0 || timeout > 5*time.Minute {
		return nil, fail("CLI_USAGE", "usage", "请求超时需大于 0 且不超过 5 分钟。", "user_action", 2)
	}
	if dir == "" {
		var err error
		dir, err = localstate.DefaultDir()
		if err != nil {
			return nil, err
		}
	}
	store, err := localstate.Open(dir)
	if err != nil {
		return nil, err
	}
	cfg, err := store.Config()
	if err != nil {
		return nil, err
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableKeepAlives = true
	client, err := api.NewClient(cfg.Server, api.WithHTTPClient(&http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}))
	if err != nil {
		return nil, fail("CLI_CONFIG", "local_state", "无法连接所配置的服务，请检查服务地址。", "user_action", 3)
	}
	return &remote{client: client, store: store, config: cfg}, nil
}
func (r *remote) auth() error {
	token, err := r.store.Credential(r.config.Server)
	if err != nil {
		message := "尚未登录，请运行 remote-agent-session setup 完成配对。"
		if data, pairingErr := r.store.Pairing(r.config.Server); pairingErr == nil {
			var pending api.DeviceCreated
			if json.Unmarshal(data, &pending) == nil && completePairing(pending) && time.Now().Before(pending.ExpiresAt) {
				message = "本机已有待领取的配对。完成飞书确认后，请使用当前 CLI 和相同 --state-dir 运行 login finish，领取登录结果后再继续。"
			}
		}
		return fail("CLI_LOGIN_REQUIRED", "authentication", message, "user_action", exitAuth)
	}
	r.token = token
	r.client.RequestEditors = []api.RequestEditorFn{func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+string(token))
		return nil
	}}
	return nil
}
func (r *remote) close() {
	for i := range r.token {
		r.token[i] = 0
	}
}

func decode(resp *http.Response, err error, dst any, uncertainAction string) error {
	if err != nil {
		return fail("CLI_TRANSPORT", "transport", "暂时无法获取服务响应，操作结果尚未确认。", uncertainAction, exitNetwork)
	}
	defer resp.Body.Close()
	data, e := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024+1))
	if e != nil || len(data) > 2*1024*1024 {
		return fail("CLI_RESPONSE", "transport", "服务响应不完整或超出大小限制。", uncertainAction, exitNetwork)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var body api.ErrorBody
		if json.Unmarshal(data, &body) != nil || body.Code == "" {
			return fail("CLI_HTTP", "protocol", "服务返回了无法识别的响应，请联系管理员。", uncertainAction, exitRemote)
		}
		action := string(body.RetryAction)
		switch body.RetryAction {
		case api.None, api.UserAction, api.SameRequest, api.Reconcile:
		default:
			action = "none"
		}
		code := exitRemote
		category := "remote"
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			code = exitAuth
			category = "authentication"
		} else if action == "reconcile" {
			code = exitPending
		}
		message := "服务未能完成此操作，请联系管理员核对。"
		f := fail(body.Code, category, message, action, code)
		f.detail.RetryAfter = body.RetryAfter
		return f
	}
	if dst == nil {
		return nil
	}
	if json.Unmarshal(data, dst) != nil {
		return fail("CLI_RESPONSE", "protocol", "服务响应格式不兼容，请检查客户端和服务版本。", uncertainAction, exitRemote)
	}
	valid := string(bytes.TrimSpace(data)) != "null"
	switch value := dst.(type) {
	case *api.UserView:
		valid = valid && value.UserId != uuid.Nil && value.SessionId != uuid.Nil
	case *api.DeviceCreated:
		valid = valid && value.DeviceId != uuid.Nil && !value.ExpiresAt.IsZero()
	}
	if !valid {
		return fail("CLI_RESPONSE", "protocol", "服务响应缺少必要信息，操作结果尚未确认。", uncertainAction, exitRemote)
	}
	return nil
}

func (r *remote) me() (u api.UserView, err error) {
	resp, e := r.client.MeV1MeGet(context.Background())
	err = decode(resp, e, &u, "same_request")
	return
}
func readJSON(path string, value any) error {
	if path == "" {
		return fail("CLI_USAGE", "usage", "使用 --file 指定请求文件。", "user_action", 2)
	}
	f, e := os.Open(path)
	if e != nil {
		return fail("CLI_INPUT", "usage", "无法打开请求文件。", "user_action", 2)
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if e != nil || len(data) > 1024*1024 {
		return fail("CLI_INPUT", "usage", "请求文件无法读取或超过 1 MiB。", "user_action", 2)
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if e = d.Decode(value); e != nil {
		return fail("CLI_INPUT", "usage", "请求 JSON 格式或字段不正确，请检查请求文件。", "user_action", 2)
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return fail("CLI_INPUT", "usage", "请求文件只能包含一个 JSON 对象。", "user_action", 2)
	}
	return nil
}
