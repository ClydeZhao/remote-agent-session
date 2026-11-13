package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ClydeZhao/remote-agent-session/cli/internal/relay"
)

func dshQuestionAnswer(question relay.DSHQuestion, value string) map[string]any {
	item := map[string]any{"id": question.ID, "selected": []string{}, "custom": value}
	for _, option := range question.Options {
		if option.Label == value {
			item["selected"] = []string{value}
			delete(item, "custom")
			return item
		}
	}
	if !question.MultiSelect {
		return item
	}
	var labels []string
	if json.Unmarshal([]byte(value), &labels) != nil || len(labels) == 0 {
		return item
	}
	known := map[string]bool{}
	for _, option := range question.Options {
		known[option.Label] = true
	}
	seen := map[string]bool{}
	for _, label := range labels {
		if !known[label] || seen[label] {
			return item
		}
		seen[label] = true
	}
	item["selected"] = labels
	delete(item, "custom")
	return item
}

func dshQuestionPrompt(question relay.DSHQuestion) (string, map[string]string, error) {
	raw, _ := json.Marshal(map[string]any{"questions": []relay.DSHQuestion{question}})
	prompt, choices, err := questionPrompt(raw)
	if err != nil {
		return "", nil, err
	}
	if question.Detail != "" {
		prompt += "\n\n" + question.Detail
	}
	if question.MultiSelect {
		labels := []string{}
		for _, option := range question.Options {
			labels = append(labels, option.Label)
			if len(labels) == 2 {
				break
			}
		}
		example, _ := json.Marshal(labels)
		prompt += fmt.Sprintf("\n\n本题可多选。多个选项请回复完整名称的 JSON 数组，例如 `%s`；也可直接写自己的答案。不能识别为完整选项数组的文字会原样作为自定义答案。", strings.ReplaceAll(string(example), "`", "\\u0060"))
	}
	return prompt, choices, nil
}
