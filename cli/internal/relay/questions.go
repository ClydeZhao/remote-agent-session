package relay

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

var ErrAnswerUnconfirmed = errors.New("异步回答是否已交给原会话尚未确认")

type NativeQuestionItem struct {
	ID       string `json:"id"`
	Header   string `json:"header"`
	Question string `json:"question"`
	IsSecret bool   `json:"isSecret"`
	Options  []struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	} `json:"options"`
}
type NativeQuestion struct {
	RequestID  json.RawMessage      `json:"request_id"`
	Owner      string               `json:"owner"`
	ThreadID   string               `json:"threadId"`
	TurnID     string               `json:"turnId"`
	ItemID     string               `json:"itemId"`
	VerifyOnly bool                 `json:"-"`
	ReplyID    string               `json:"reply_id"`
	Questions  []NativeQuestionItem `json:"questions"`
	IsBlocking bool                 `json:"isBlocking"`
}

func (q NativeQuestion) StableID() string { return q.ThreadID + ":" + q.TurnID + ":" + q.ItemID }

type NativeQuestionResponse struct {
	Answers map[string]struct {
		Answers []string `json:"answers"`
	} `json:"answers"`
}
type questionReply struct {
	ID       string `json:"questionItemId"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
}
type questionHistoryItem struct {
	ID                  string `json:"id"`
	Type                string `json:"type"`
	Delivery            string `json:"delivery"`
	ClientID            string `json:"clientId"`
	ClientUserMessageID string `json:"clientUserMessageId"`
	Status              string `json:"status"`
	Questions           []struct {
		Title   string   `json:"title"`
		Options []string `json:"options"`
	} `json:"questions"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Input []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"input"`
}
type questionHistoryTurn struct {
	CompletedAt *int64                `json:"completedAt"`
	ID          string                `json:"id"`
	Status      string                `json:"status"`
	ItemsView   string                `json:"itemsView"`
	Items       []questionHistoryItem `json:"items"`
}

func (c *Codex) questionHistory(ctx context.Context, thread string) ([]questionHistoryTurn, error) {
	cursor := ""
	result := []questionHistoryTurn{}
	for page := 0; page < 20; page++ {
		var response struct {
			Data []questionHistoryTurn `json:"data"`
			Next *string               `json:"nextCursor"`
		}
		params := map[string]any{"threadId": thread, "itemsView": "full", "limit": 50, "sortDirection": "desc"}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := c.RPC.Call(ctx, "thread/turns/list", params, &response); err != nil {
			return nil, err
		}
		for _, turn := range response.Data {
			if turn.ItemsView != "full" {
				return nil, ErrHistoryIncomplete
			}
			result = append(result, turn)
		}
		if response.Next == nil || *response.Next == "" {
			return result, nil
		}
		cursor = *response.Next
	}
	return nil, ErrHistoryIncomplete
}
func questionText(item questionHistoryItem) string {
	parts := []string{}
	values := item.Content
	if item.Type == "steeringUserMessage" {
		values = item.Input
	}
	for _, p := range values {
		if p.Type == "text" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}
func parseQuestionReply(text string) []questionReply {
	text = strings.TrimSpace(text)
	const prefix = "<send_user_message_question_reply>"
	const suffix = "</send_user_message_question_reply>"
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		return nil
	}
	var result []questionReply
	if json.Unmarshal([]byte(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, prefix), suffix))), &result) != nil {
		return nil
	}
	return result
}
func pendingHistoryQuestions(turns []questionHistoryTurn, thread, owner string) ([]NativeQuestion, error) {
	result := []NativeQuestion{}
	for ti := len(turns) - 1; ti >= 0; ti-- {
		turn := turns[ti]
		for _, item := range turn.Items {
			if item.Type == "userMessage" || (item.Type == "steeringUserMessage" && item.Status == "accepted") {
				replies := parseQuestionReply(questionText(item))
				if len(replies) == 0 {
					result = nil
					continue
				}
				answered := map[string]bool{}
				for _, answer := range replies {
					answered[answer.ID] = true
				}
				remaining := []NativeQuestion{}
				for _, q := range result {
					kept := []NativeQuestionItem{}
					for _, question := range q.Questions {
						if !answered[question.ID] {
							kept = append(kept, question)
						}
					}
					q.Questions = kept
					if len(kept) > 0 {
						remaining = append(remaining, q)
					}
				}
				result = remaining
				continue
			}
			if ((turn.Status == "interrupted" || turn.Status == "failed") && turn.CompletedAt != nil) || item.Type != "agentMessage" || item.Delivery != "async" || len(item.Questions) == 0 {
				continue
			}
			if item.ID == "" {
				return nil, ErrHistoryIncomplete
			}
			requestID, _ := json.Marshal(item.ID)
			q := NativeQuestion{RequestID: requestID, Owner: owner, ThreadID: thread, TurnID: turn.ID, ItemID: item.ID}
			for index, question := range item.Questions {
				id, _ := json.Marshal([]any{"request_user_input_async", item.ID, index})
				if question.Title == "" {
					return nil, ErrHistoryIncomplete
				}
				native := NativeQuestionItem{ID: string(id), Question: question.Title}
				for _, option := range question.Options {
					native.Options = append(native.Options, struct {
						Label       string `json:"label"`
						Description string `json:"description"`
					}{Label: option})
				}
				q.Questions = append(q.Questions, native)
			}
			result = append(result, q)
		}
	}
	return result, nil
}

func (c *Codex) PendingQuestions(ctx context.Context, thread string) ([]NativeQuestion, error) {
	current, err := c.InspectRelayTarget(ctx, thread)
	if err != nil {
		return nil, err
	}
	owner := c.RPC.questionOwner
	if current.Status.Type == "notLoaded" {
		d, err := openDesktop(ctx)
		if err != nil {
			return nil, err
		}
		defer d.conn.Close()
		owner, err = d.call("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": thread}, "", nil)
		if err != nil || owner == "" {
			return nil, ErrInterruptUnavailable
		}
	}
	turns, err := c.questionHistory(ctx, thread)
	if err != nil {
		return nil, err
	}
	return pendingHistoryQuestions(turns, thread, owner)
}
func questionPresent(items []NativeQuestion, q NativeQuestion) bool {
	for _, item := range items {
		if item.Owner == q.Owner && item.StableID() == q.StableID() && reflect.DeepEqual(item.Questions, q.Questions) {
			return true
		}
	}
	return false
}
func questionAnswerText(q NativeQuestion, response NativeQuestionResponse) (string, error) {
	if len(response.Answers) != len(q.Questions) || q.ReplyID == "" {
		return "", ErrHistoryIncomplete
	}
	answers := []questionReply{}
	for _, question := range q.Questions {
		answer, ok := response.Answers[question.ID]
		if !ok || len(answer.Answers) == 0 {
			return "", ErrHistoryIncomplete
		}
		answers = append(answers, questionReply{ID: question.ID, Question: question.Question, Answer: strings.Join(answer.Answers, "\n")})
	}
	data, err := json.Marshal(answers)
	if err != nil {
		return "", err
	}
	return "<send_user_message_question_reply>\n" + string(data) + "\n</send_user_message_question_reply>", nil
}
func (c *Codex) QuestionAnswerObserved(ctx context.Context, q NativeQuestion, response NativeQuestionResponse) (bool, error) {
	text, err := questionAnswerText(q, response)
	if err != nil {
		return false, err
	}
	turns, err := c.questionHistory(ctx, q.ThreadID)
	if err != nil {
		return false, err
	}
	for _, turn := range turns {
		for _, item := range turn.Items {
			if (item.Type == "userMessage" && item.ClientID == q.ReplyID) || (item.Type == "steeringUserMessage" && item.ClientUserMessageID == q.ReplyID && item.Status == "accepted") {
				if questionText(item) != text {
					return false, ErrHistoryIncomplete
				}
				return true, nil
			}
		}
	}
	return false, nil
}
func (c *Codex) AnswerQuestion(ctx context.Context, q NativeQuestion, response NativeQuestionResponse, authorize func() error) error {
	text, err := questionAnswerText(q, response)
	if err != nil {
		return err
	}
	if observed, err := c.QuestionAnswerObserved(ctx, q, response); err != nil {
		return err
	} else if observed {
		return nil
	}
	if q.VerifyOnly {
		return ErrAnswerUnconfirmed
	}
	pending, err := c.PendingQuestions(ctx, q.ThreadID)
	if err != nil {
		return err
	}
	if !questionPresent(pending, q) {
		return ErrHistoryIncomplete
	}
	turns, err := c.questionHistory(ctx, q.ThreadID)
	if err != nil || len(turns) == 0 {
		return ErrHistoryIncomplete
	}
	current, err := c.InspectRelayTarget(ctx, q.ThreadID)
	if err != nil {
		return err
	}
	if err := authorize(); err != nil {
		return err
	}
	input := []map[string]any{{"type": "text", "text": text, "text_elements": []any{}}}
	if turns[0].CompletedAt == nil && (turns[0].Status == "inProgress" || turns[0].Status == "interrupted") {
		if current.Status.Type == "notLoaded" {
			if current.Cwd == "" {
				return ErrHistoryIncomplete
			}
			d, e := openDesktop(ctx)
			if e != nil {
				return e
			}
			defer d.conn.Close()
			owner, e := d.call("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": q.ThreadID}, "", nil)
			if e != nil || owner != q.Owner {
				return ErrInterruptUnavailable
			}
			if e := authorize(); e != nil {
				return e
			}
			var actual string
			restoreMessage := map[string]any{
				"id":        q.ReplyID,
				"text":      text,
				"cwd":       current.Cwd,
				"createdAt": time.Now().UnixMilli(),
				"context": map[string]any{
					"prompt":                text,
					"imageAttachments":      []any{},
					"fileAttachments":       []any{},
					"pastedTextAttachments": []any{},
					"addedFiles":            []any{},
				},
				"responsesapiClientMetadata": map[string]any{"source": "codex"},
			}
			actual, err = d.call("thread-follower-steer-turn", 1, map[string]any{"conversationId": q.ThreadID, "input": input, "clientUserMessageId": q.ReplyID, "restoreMessage": restoreMessage}, owner, nil)
			if actual != owner {
				return ErrAnswerUnconfirmed
			}
		} else {
			if q.Owner != c.RPC.questionOwner {
				return ErrInterruptUnavailable
			}
			err = c.RPC.Call(ctx, "turn/steer", map[string]any{"threadId": q.ThreadID, "expectedTurnId": turns[0].ID, "clientUserMessageId": q.ReplyID, "input": input}, nil)
		}
	} else {
		if turns[0].CompletedAt == nil {
			return ErrHistoryIncomplete
		}
		params := map[string]any{"threadId": q.ThreadID, "clientUserMessageId": q.ReplyID, "input": input}
		if current.Status.Type == "notLoaded" {
			d, e := openDesktop(ctx)
			if e != nil {
				return e
			}
			defer d.conn.Close()
			owner, e := d.call("thread-owner-discovery", 1, map[string]any{"hostId": "local", "conversationId": q.ThreadID}, "", nil)
			if e != nil || owner != q.Owner {
				return ErrInterruptUnavailable
			}
			if e := authorize(); e != nil {
				return e
			}
			_, err = d.call("thread-follower-start-turn", 2, map[string]any{"conversationId": q.ThreadID, "turnStart": map[string]any{"request": params, "context": map[string]any{"inheritThreadSettings": true}}}, owner, nil)
		} else {
			if q.Owner != c.RPC.questionOwner {
				return ErrInterruptUnavailable
			}
			if e := authorize(); e != nil {
				return e
			}
			err = c.RPC.Call(ctx, "turn/start", params, nil)
		}
	}
	if err != nil {
		return ErrAnswerUnconfirmed
	}
	for {
		observed, err := c.QuestionAnswerObserved(ctx, q, response)
		if err != nil {
			return ErrAnswerUnconfirmed
		}
		if observed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ErrAnswerUnconfirmed
		case <-time.After(100 * time.Millisecond):
		}
	}
}
func (c *Codex) OriginIDsForQuestion(ctx context.Context, thread, turn string) ([]string, error) {
	return c.questionOriginIDs(ctx, thread, turn, false)
}
func (c *Codex) AllOriginIDsForQuestion(ctx context.Context, thread, turn string) ([]string, error) {
	return c.questionOriginIDs(ctx, thread, turn, true)
}
func (c *Codex) questionOriginIDs(ctx context.Context, thread, turn string, includeReplies bool) ([]string, error) {
	turns, err := c.questionHistory(ctx, thread)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, entry := range turns {
		firstInput := true
		for _, item := range entry.Items {
			if item.Type != "userMessage" && (item.Type != "steeringUserMessage" || item.Status != "accepted") {
				continue
			}
			triggerInput := entry.ID == turn && firstInput
			firstInput = false
			if entry.ID != turn && len(parseQuestionReply(questionText(item))) == 0 {
				return nil, ErrMixedOrigin
			}
			if !includeReplies && !triggerInput && len(parseQuestionReply(questionText(item))) > 0 {
				continue
			}
			id := item.ClientID
			if item.Type == "steeringUserMessage" {
				id = item.ClientUserMessageID
			}
			ids = append(ids, id)
		}
		if entry.ID == turn {
			if (entry.Status == "interrupted" || entry.Status == "failed") && entry.CompletedAt != nil {
				return nil, ErrHistoryIncomplete
			}
			return ids, nil
		}
	}
	return nil, ErrHistoryIncomplete
}
