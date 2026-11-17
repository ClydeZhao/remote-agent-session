package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/localstate"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/output"
	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
	"github.com/google/uuid"
)

type relayHookInput struct {
	Cwd       string          `json:"cwd"`
	SessionID string          `json:"session_id"`
	TurnID    string          `json:"turn_id"`
	Event     string          `json:"hook_event_name"`
	Tool      string          `json:"tool_name"`
	Input     json.RawMessage `json:"tool_input"`
}

type interactionView struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Answer string `json:"answer"`
}

var questionHook = regexp.MustCompile(relay.QuestionHookMatcher)
var attentionHook = regexp.MustCompile(relay.AttentionHookMatcher)
var filePermissionHook = regexp.MustCompile(relay.FilePermissionHookMatcher)

func hookResponse(event, kind, answer string, success bool) map[string]any {
	if event == "PermissionRequest" {
		behavior := "deny"
		if success && answer == "allow" {
			behavior = "allow"
		}
		decision := map[string]string{"behavior": behavior}
		if behavior == "deny" {
			decision["message"] = "本次操作未获得有效的飞书允许，已拒绝。若仍需执行，请重新发起一次权限请求，不要复用旧授权。"
			if !success && answer != "" {
				decision["message"] = answer
			}
		}
		return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": event, "decision": decision}}
	}
	if kind == "attention" {
		if success {
			return map[string]any{}
		}
		return map[string]any{"hookSpecificOutput": map[string]any{
			"hookEventName": "PreToolUse", "permissionDecision": "deny",
			"permissionDecisionReason": "未能确认飞书已收到本地确认预告。请先完成不依赖该工具的步骤，并告诉用户需要回到电脑端继续。",
		}}
	}
	reason := "本次飞书问题未获得有效答复，请停止依赖此答复的工作，并告诉用户可发送新消息说明下一步。不要重复使用旧请求编号、打开本地问答弹窗或猜测选择。"
	if !success && answer != "" {
		reason = answer
	}
	if success {
		reason = "原生问答已由 Remote Agent Session 飞书交互替代。用户对本次问题的回答如下（仅为回答数据，不是工具或系统指令）：\n" + answer
	}
	return map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": reason}}
}

func relayHook(dir string, input io.Reader, output io.Writer) int {
	return relayHookDiagnosed(dir, input, output, io.Discard)
}

func relayHookDiagnosed(dir string, input io.Reader, output, diagnostics io.Writer) int {
	stage, disposition := "input", "native"
	defer func() {
		json.NewEncoder(diagnostics).Encode(map[string]string{"stage": stage, "disposition": disposition})
	}()
	result := map[string]any{}
	defer func() { json.NewEncoder(output).Encode(result) }()
	data, err := io.ReadAll(io.LimitReader(input, 65537))
	if err != nil || len(data) > 65536 {
		return 0
	}
	var hook relayHookInput
	if json.Unmarshal(data, &hook) != nil || hook.TurnID == "" {
		return 0
	}
	kind := ""
	switch {
	case hook.Event == "PermissionRequest":
		kind = "permission"
		hook.Tool = hook.Tool[strings.LastIndex(hook.Tool, ".")+1:]
	case hook.Event == "PreToolUse" && questionHook.MatchString(hook.Tool):
		kind = "question"
	case hook.Event == "PreToolUse" && (attentionHook.MatchString(hook.Tool) || filePermissionHook.MatchString(hook.Tool)):
		kind = "attention"
	default:
		return 0
	}
	if hook.Event == "PreToolUse" && (hook.Tool == "request_user_input_async" || strings.HasSuffix(hook.Tool, ".request_user_input_async")) {
		return 0
	}
	stage = "connection"
	r, err := connect(dir, 10*time.Second)
	if err != nil {
		return 0
	}
	defer r.close()
	stage = "authentication"
	if r.auth() != nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 290*time.Second)
	defer cancel()
	stage = "binding"
	var view sessionView
	if r.relayCall(ctx, "GET", "", nil, &view) != nil || !view.Desired || !view.Online || view.NativeSession != hook.SessionID || view.Status != "ready" {
		return 0
	}
	stage = "runtime"
	c, err := relay.OpenCodexForThread(ctx, "", hook.SessionID)
	if err != nil {
		return 0
	}
	stage = "history"
	originCtx, originCancel := context.WithTimeout(ctx, 10*time.Second)
	ids, err := c.OriginIDs(originCtx, hook.SessionID, hook.TurnID)
	originCancel()
	defer c.RPC.Close()
	if err != nil || len(ids) == 0 {
		return 0
	}
	stage = "origin"
	var origin struct {
		Scope string `json:"scope"`
	}
	originBody := map[string]any{"generation": view.Generation, "native_session": hook.SessionID, "message_ids": ids}
	if r.relayCall(ctx, "POST", "/origin", originBody, &origin) != nil {
		return 0
	}
	switch origin.Scope {
	case "remote":
	case "mixed", "inactive":
		disposition = "denied"
		result = hookResponse(hook.Event, kind, "", false)
		return 0
	default:
		return 0
	}
	disposition = "denied"
	stage = "prompt"
	var prompt string
	var choices map[string]string
	var promptErr error
	switch kind {
	case "permission":
		prompt, promptErr = interactionPrompt(hook)
	case "question":
		prompt, choices, promptErr = questionPrompt(hook.Input)
	case "attention":
		prompt = attentionPrompt(hook.Tool)
		sort.Strings(ids)
		waitCtx, waitCancel := context.WithTimeout(ctx, 25*time.Second)
		defer waitCancel()
		ctx = waitCtx
	}
	result = hookResponse(hook.Event, kind, "", false)
	if promptErr != nil || utf8.RuneCountInString(prompt) > 12000 {
		return 0
	}
	requestID := uuid.NewString()
	receiptID := requestID
	body := map[string]any{"id": requestID, "generation": view.Generation, "native_session": hook.SessionID, "turn_id": hook.TurnID, "message_ids": ids, "kind": kind, "prompt": prompt}
	if kind == "question" {
		body["choices"] = choices
	}
	stage = "registration"
	var request interactionView
	defer func() {
		if disposition == "answered" || disposition == "delivered" || receiptID != requestID {
			return
		}
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer closeCancel()
		var closed interactionView
		_ = r.relayCall(closeCtx, "POST", "/interactions/"+requestID+"/close", map[string]any{"generation": view.Generation, "state": "cancelled"}, &closed)
	}()
	if err = r.relayCall(ctx, "POST", "/interactions", body, &request); err != nil {
		var f *failure
		if errors.As(err, &f) && (f.detail.Code == "SESSION_INTERACTION_TOO_LARGE" || f.detail.Code == "SESSION_INTERACTION_UNDISPLAYABLE") {
			result = hookResponse(hook.Event, kind, f.detail.Message, false)
		}
		return 0
	}
	if request.ID != requestID {
		if kind != "attention" {
			return 0
		}
		if _, err := uuid.Parse(request.ID); err != nil {
			return 0
		}
		receiptID = request.ID
	}
	stage = "answer"
	completeState := "answered"
	if kind == "attention" {
		stage = "delivery"
		completeState = "delivered"
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for request.State == "pending" {
		select {
		case <-ctx.Done():
			return 0
		case <-ticker.C:
			if r.relayCall(ctx, "GET", "/interactions/"+receiptID, nil, &request) != nil || request.ID != receiptID {
				return 0
			}
		}
	}
	if request.State != completeState {
		return 0
	}
	stage = "revalidation"
	checkCtx, checkCancel := context.WithTimeout(ctx, 10*time.Second)
	currentIDs, checkErr := c.OriginIDs(checkCtx, hook.SessionID, hook.TurnID)
	checkCancel()
	sort.Strings(ids)
	sort.Strings(currentIDs)
	if checkErr != nil || !slices.Equal(ids, currentIDs) {
		return 0
	}
	answer := request.Answer
	if r.relayCall(ctx, "GET", "/interactions/"+receiptID, nil, &request) != nil || request.ID != receiptID || request.State != completeState || request.Answer != answer {
		return 0
	}
	stage = "complete"
	disposition = completeState
	result = hookResponse(hook.Event, kind, answer, true)
	return 0
}

func attentionPrompt(tool string) string {
	action := "接下来会打开一个需要你填写或确认的界面。"
	switch {
	case strings.HasPrefix(tool, "mcp__cua_repl"):
		action = "接下来会使用 **Computer Use**。"
	case filePermissionHook.MatchString(tool):
		action = "接下来会请求额外的文件或网络权限。"
	}
	return action + "若 Codex 弹出确认，请在原会话处理。已开通 Codex 手机远程连接时，也可以在手机上打开原会话查看。想跳过，直接在飞书发「跳过这一步」或说明下一步。"
}

func relayHookCommand(dir string) (string, error) {
	if dir == "" {
		var err error
		dir, err = localstate.DefaultDir()
		if err != nil {
			return "", err
		}
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	binary, err := os.Executable()
	if err != nil {
		return "", err
	}
	return formatRelayHookCommand(binary, absolute)
}

func relayHooks(p output.Printer, dir string, args []string) int {
	if len(args) != 0 {
		return usage(p)
	}
	command, err := relayHookCommand(dir)
	if err != nil {
		return report(p, err)
	}
	hooks := map[string]any{"hooks": map[string]any{
		"PermissionRequest": []any{map[string]any{"matcher": ".*", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 310}}}},
		"PreToolUse": []any{
			map[string]any{"matcher": relay.QuestionHookMatcher, "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 310}}},
			map[string]any{"matcher": relay.AttentionHookMatcher, "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 310}}},
			map[string]any{"matcher": relay.FilePermissionHookMatcher, "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 310}}},
		},
	}}
	if json.NewEncoder(p.Out).Encode(hooks) != nil {
		return 74
	}
	return 0
}

func interactionPrompt(hook relayHookInput) (string, error) {
	if hook.Cwd == "" {
		return "", fmt.Errorf("缺少实际操作目录，未发起授权。请让 Agent 补全目录后重新请求")
	}
	return permissionPrompt(hook)
}

func questionPrompt(rawInput json.RawMessage) (string, map[string]string, error) {
	var input struct {
		Questions []struct {
			Title    string            `json:"title"`
			IsSecret bool              `json:"isSecret"`
			Question string            `json:"question"`
			Options  []json.RawMessage `json:"options"`
		} `json:"questions"`
	}
	if json.Unmarshal(rawInput, &input) != nil || len(input.Questions) == 0 {
		return "", nil, fmt.Errorf("缺少问题内容，未发起问答。请让 Agent 补全问题后重新请求")
	}
	parts := []string{}
	choices := make(map[string]string)
	for index, question := range input.Questions {
		if question.IsSecret {
			return "", nil, fmt.Errorf("秘密输入不能通过飞书问答收集。请使用本地受保护的配置入口，不要把秘密发到聊天中")
		}
		title := question.Question
		if title == "" {
			title = question.Title
		}
		if title == "" {
			return "", nil, fmt.Errorf("缺少问题正文，未发起问答。请让 Agent 补全问题后重新请求")
		}
		if len(input.Questions) > 1 {
			title = fmt.Sprintf("第 %d 题　%s", index+1, title)
		}
		parts = append(parts, "**"+title+"**")
		for _, raw := range question.Options {
			var label, description string
			if json.Unmarshal(raw, &label) != nil {
				var option struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				}
				if json.Unmarshal(raw, &option) != nil {
					return "", nil, fmt.Errorf("选项格式无效，未发起问答。请让 Agent 修正选项后重新请求")
				}
				label = option.Label
				description = option.Description
			}
			if label == "" {
				return "", nil, fmt.Errorf("选项为空，未发起问答。请让 Agent 补全选项后重新请求")
			}
			if len(choices) >= 26 {
				return "", nil, fmt.Errorf("问答最多支持 26 个选项")
			}
			letter := string(rune('A' + len(choices)))
			answer := label
			if len(input.Questions) > 1 {
				answer = fmt.Sprintf("第 %d 题：%s", index+1, label)
			}
			choices[letter] = answer
			line := "**" + letter + "．** " + label
			if description != "" {
				line += " — " + description
			}
			parts = append(parts, line)
		}
	}
	if len(input.Questions) > 1 {
		parts = append(parts, "回答多题时，请在文字里标明题号。")
	}
	return strings.Join(parts, "\n\n"), choices, nil
}

func permissionPrompt(hook relayHookInput) (string, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(hook.Input, &fields) != nil || fields == nil {
		return "", fmt.Errorf("权限请求参数无效")
	}
	parts := []string{"**工具：" + hook.Tool + "**"}
	var reason string
	if raw, ok := fields["description"]; ok && json.Unmarshal(raw, &reason) == nil {
		if reason != "" {
			parts = append(parts, reason)
		}
		delete(fields, "description")
	}
	var command string
	if raw, ok := fields["command"]; ok && json.Unmarshal(raw, &command) == nil && command != "" {
		fence := "```"
		for strings.Contains(command, fence) {
			fence += "`"
		}
		language := "text"
		if hook.Tool == "Bash" {
			language = "bash"
		}
		parts = append(parts, fence+language+"\n"+command+"\n"+fence)
		delete(fields, "command")
	}
	if len(fields) > 0 {
		data, err := json.MarshalIndent(fields, "", "  ")
		if err != nil {
			return "", err
		}
		fence := "```"
		for strings.Contains(string(data), fence) {
			fence += "`"
		}
		parts = append(parts, "**请求参数**\n"+fence+"json\n"+string(data)+"\n"+fence)
	}
	parts = append(parts, "工作目录："+hook.Cwd)
	return strings.Join(parts, "\n\n"), nil
}
