package relay

import (
	"encoding/json"
	"strconv"
	"strings"
)

type DSHEvent struct {
	Seq  int             `json:"seq"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

type DSHHistory struct {
	SessionID string     `json:"session_id"`
	NextSeq   int        `json:"next_seq"`
	Events    []DSHEvent `json:"events"`
}

func (h DSHHistory) Outcome(sessionID, requestID string) (Outcome, error) {
	if h.SessionID != sessionID || sessionID == "" || requestID == "" || h.NextSeq != len(h.Events) {
		return Outcome{}, ErrHistoryIncomplete
	}
	var current Outcome
	var matched *Outcome
	var finalText string
	for i, event := range h.Events {
		if event.Seq != i {
			return Outcome{}, ErrHistoryIncomplete
		}
		switch event.Type {
		case "turn/start":
			var data struct{ Turn int }
			if json.Unmarshal(event.Data, &data) != nil || data.Turn < 1 || current.TurnID != "" {
				return Outcome{}, ErrHistoryIncomplete
			}
			current = Outcome{TurnID: strconv.Itoa(data.Turn), Status: "inProgress"}
			finalText = ""
		case "user/message":
			if current.TurnID == "" {
				continue
			}
			var data struct {
				Source struct {
					Kind  string `json:"kind"`
					RPCID string `json:"rpcId"`
				} `json:"source"`
			}
			if json.Unmarshal(event.Data, &data) != nil {
				return Outcome{}, ErrHistoryIncomplete
			}
			switch data.Source.Kind {
			case "runtime-context", "skill-catalog", "time-context":
				continue
			}
			id := ""
			if data.Source.Kind == "user" {
				id = data.Source.RPCID
			}
			current.OriginIDs = append(current.OriginIDs, id)
			if id == requestID {
				current.Found = true
			} else {
				current.Mixed = true
			}
		case "assistant/message":
			var data struct {
				Turn    int `json:"turn"`
				Message struct {
					Role    string `json:"role"`
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
			}
			if json.Unmarshal(event.Data, &data) != nil || current.TurnID == "" || strconv.Itoa(data.Turn) != current.TurnID || data.Message.Role != "assistant" {
				return Outcome{}, ErrHistoryIncomplete
			}
			parts := []string{}
			for _, part := range data.Message.Content {
				if part.Type == "text" {
					parts = append(parts, part.Text)
				}
			}
			finalText = strings.Join(parts, "")
		case "turn/end":
			var data struct {
				Turn   int `json:"turn"`
				Reason struct {
					Kind string `json:"kind"`
				} `json:"reason"`
			}
			if json.Unmarshal(event.Data, &data) != nil || current.TurnID == "" || strconv.Itoa(data.Turn) != current.TurnID || data.Reason.Kind == "" {
				return Outcome{}, ErrHistoryIncomplete
			}
			current.Complete = true
			current.Status = "failed"
			if data.Reason.Kind == "completed" {
				current.Status = "completed"
				current.UnverifiedText = finalText
			} else if data.Reason.Kind == "aborted" {
				current.Status = "interrupted"
			}
			if current.Found {
				if matched != nil {
					return Outcome{}, ErrHistoryIncomplete
				}
				value := current
				matched = &value
			}
			current = Outcome{}
		}
	}
	if current.Found {
		if matched != nil {
			return Outcome{}, ErrHistoryIncomplete
		}
		matched = &current
	}
	if matched == nil {
		return Outcome{}, nil
	}
	if matched.Mixed {
		return *matched, ErrMixedOrigin
	}
	matched.Text = matched.UnverifiedText
	matched.UnverifiedText = ""
	return *matched, nil
}
