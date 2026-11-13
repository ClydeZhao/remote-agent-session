package output

import (
	"encoding/json"
	"fmt"
	"io"
	"time"
)

const (
	Success       = 0
	Usage         = 2
	LocalFailure  = 3
	OutputFailure = 74
)

type Error struct {
	Code        string `json:"code"`
	Category    string `json:"category"`
	Message     string `json:"message"`
	RetryAction string `json:"retry_action"`
	RetryAfter  *int   `json:"retry_after,omitempty"`
}

type Envelope struct {
	EmittedAt string         `json:"emitted_at"`
	Data      any            `json:"data,omitempty"`
	Error     *Error         `json:"error,omitempty"`
	Notice    map[string]any `json:"_notice,omitempty"`
}

type Printer struct {
	JSON     bool
	Out, Err io.Writer
	Notice   map[string]any
}

func (p Printer) Result(data any, human string) int {
	var err error
	if p.JSON {
		err = json.NewEncoder(p.Out).Encode(Envelope{EmittedAt: time.Now().UTC().Format(time.RFC3339Nano), Data: data, Notice: p.Notice})
	} else {
		_, err = fmt.Fprintln(p.Out, human)
		if err == nil && len(p.Notice) > 0 {
			_, _ = fmt.Fprintln(p.Err, "发现客户端或 skill 更新，可运行 remote-agent-session update --check --json 查看。")
		}
	}
	if err != nil {
		return OutputFailure
	}
	return Success
}

func (p Printer) Failure(e Error, exit int) int {
	var err error
	if p.JSON {
		err = json.NewEncoder(p.Out).Encode(Envelope{EmittedAt: time.Now().UTC().Format(time.RFC3339Nano), Error: &e, Notice: p.Notice})
	} else {
		message := e.Message
		switch e.RetryAction {
		case "same_request":
			message += "\n如需重试，请保留原请求文件和请求引用。"
		case "reconcile":
			message += "\n请核对原执行记录，不要重新提交任务。"
		}
		_, err = fmt.Fprintln(p.Err, message)
	}
	if err != nil {
		return OutputFailure
	}
	return exit
}
