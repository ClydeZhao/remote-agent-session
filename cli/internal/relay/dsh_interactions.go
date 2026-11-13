package relay

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

type DSHPermissionPolicy struct {
	Mode                    string   `json:"mode"`
	Profile                 string   `json:"profile"`
	ProfileDirectory        string   `json:"profile_directory"`
	WorkspaceRoot           string   `json:"workspace_root"`
	AdditionalWritableRoots []string `json:"additional_writable_roots"`
}

func (d *DSH) PermissionPolicy(ctx context.Context, sessionID string) (DSHPermissionPolicy, error) {
	var policy DSHPermissionPolicy
	params, err := d.params(sessionID)
	if err != nil {
		return policy, err
	}
	if err = d.rpc.Call(ctx, "permission_policy", params, &policy); err != nil {
		return policy, errors.New("无法读取原 DSH 的目录权限策略，请安装并重新加载匹配的 Remote Agent Session 插件后重试；未修改任何权限")
	}
	if policy.Mode != "workspace-write" || policy.Profile == "" || !filepath.IsAbs(policy.ProfileDirectory) || !filepath.IsAbs(policy.WorkspaceRoot) || policy.AdditionalWritableRoots == nil {
		return DSHPermissionPolicy{}, errors.New("无法确认原 DSH 的正常沙箱目录策略，请更新匹配插件并在原会话选择 workspace-write；不会自动改变权限模式")
	}
	for _, root := range policy.AdditionalWritableRoots {
		if !filepath.IsAbs(root) {
			return DSHPermissionPolicy{}, ErrInterruptUnavailable
		}
	}
	return policy, nil
}

type DSHQuestion struct {
	ID          string `json:"id"`
	Header      string `json:"header"`
	Question    string `json:"question"`
	Detail      string `json:"detail"`
	MultiSelect bool   `json:"multiSelect"`
	Options     []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}

type DSHInteraction struct {
	AllOriginIDs []string        `json:"all_origin_ids"`
	ID           string          `json:"id"`
	Kind         string          `json:"kind"`
	TurnID       string          `json:"turn_id"`
	OriginIDs    []string        `json:"origin_ids"`
	ToolName     string          `json:"tool_name"`
	Reason       string          `json:"reason"`
	CallID       string          `json:"call_id"`
	ToolCall     json.RawMessage `json:"tool_call"`
	Questions    []DSHQuestion   `json:"questions"`
	Timed        bool            `json:"timed"`
	Continued    bool            `json:"continued"`
}

func (d *DSH) BindInteractions(ctx context.Context, sessionID string, generation int64, origins []string) error {
	params, err := d.params(sessionID)
	if err != nil {
		return err
	}
	if origins == nil {
		origins = []string{}
	}
	params["generation"], params["origins"], params["lease_ms"] = generation, origins, 30000
	var result struct{ Bound bool }
	if err = d.rpc.Call(ctx, "bind_interactions", params, &result); err != nil {
		return err
	}
	if !result.Bound {
		return errors.New("DeepSeek Harness 交互通道尚未就绪")
	}
	return nil
}

func (d *DSH) UnbindInteractions(ctx context.Context, sessionID string, generation int64) error {
	params, err := d.params(sessionID)
	if err != nil {
		return err
	}
	params["generation"] = generation
	var result struct{ Unbound bool }
	if err = d.rpc.Call(ctx, "unbind_interactions", params, &result); err != nil {
		return err
	}
	if !result.Unbound {
		return errors.New("DeepSeek Harness 交互断开尚未确认")
	}
	return nil
}

func (d *DSH) Interactions(ctx context.Context, sessionID string, generation int64) ([]DSHInteraction, error) {
	params, err := d.params(sessionID)
	if err != nil {
		return nil, err
	}
	params["generation"] = generation
	var result struct {
		Requests []DSHInteraction `json:"requests"`
	}
	if err = d.rpc.Call(ctx, "interactions", params, &result); err != nil {
		return nil, err
	}
	if result.Requests == nil {
		return nil, ErrHistoryIncomplete
	}
	return result.Requests, nil
}

func (d *DSH) AnswerInteraction(ctx context.Context, sessionID string, generation int64, id string, answer any, authorize func() error) error {
	params, err := d.params(sessionID)
	if err != nil {
		return err
	}
	if err = authorize(); err != nil {
		return err
	}
	params["generation"], params["interaction_id"], params["answer"] = generation, id, answer
	var result struct{ Accepted bool }
	if err = d.rpc.Call(ctx, "answer_interaction", params, &result); err != nil {
		return err
	}
	if !result.Accepted {
		return ErrAnswerUnconfirmed
	}
	return nil
}

func (d *DSH) CheckInteractions(ctx context.Context) error {
	var result struct {
		Protocol     int
		Interactions bool
		Continued    bool
	}
	if err := d.rpc.Call(ctx, "capabilities", map[string]any{}, &result); err != nil {
		return err
	}
	if result.Protocol != 1 || !result.Interactions || !result.Continued {
		return errors.New("请更新原 DeepSeek Harness 的 Remote Agent Session 插件后重新连接")
	}
	return nil
}

func (d *DSH) ContinuedInteractions(ctx context.Context, sessionID string, generation int64) ([]DSHInteraction, error) {
	params, err := d.params(sessionID)
	if err != nil {
		return nil, err
	}
	params["generation"] = generation
	params["candidates"] = true
	var result struct {
		Requests []DSHInteraction `json:"requests"`
	}
	if err = d.rpc.Call(ctx, "continued_interactions", params, &result); err != nil {
		return nil, err
	}
	if result.Requests == nil {
		return nil, ErrHistoryIncomplete
	}
	return result.Requests, nil
}

func (d *DSH) AnswerContinued(ctx context.Context, sessionID string, generation int64, callID, requestID string, answer any, verifyOnly bool, authorize func() error) (string, error) {
	params, err := d.params(sessionID)
	if err != nil {
		return "", err
	}
	if err = authorize(); err != nil {
		return "", err
	}
	params["generation"], params["call_id"], params["request_id"], params["answer"] = generation, callID, requestID, answer
	params["verify_only"] = verifyOnly
	var result struct {
		Accepted  bool   `json:"accepted"`
		MessageID string `json:"message_id"`
	}
	if err = d.rpc.Call(ctx, "answer_continued", params, &result); err != nil {
		return "", err
	}
	if !result.Accepted || result.MessageID == "" {
		return "", ErrAnswerUnconfirmed
	}
	return result.MessageID, nil
}
