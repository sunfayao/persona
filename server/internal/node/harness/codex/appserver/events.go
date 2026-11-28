package appserver

import (
	"encoding/json"

	"github.com/sunfayao/persona/server/internal/transcript"
)

// EventKind identifies a notification by what the node needs to do with it.
type EventKind uint8

const (
	ThreadSettings EventKind = iota + 1
	ThreadReverted
	TurnStarted
	TurnCompleted
	ItemStarted
	ItemCompleted
	AgentMessageDelta
	ReasoningDelta
	TokenUsageUpdated
	ThreadCompacted
	UnretriedError
	RequestResolved
)

// Event is the decoded app-server notification for either connection owner.
type Event struct {
	Kind      EventKind       `json:"-"`
	ThreadID  string          `json:"threadId"`
	TurnID    string          `json:"turnId"`
	Delta     string          `json:"delta"`
	RequestID json.RawMessage `json:"requestId"`
	Settings  struct {
		Model       string          `json:"model"`
		Effort      string          `json:"effort"`
		ServiceTier json.RawMessage `json:"serviceTier"`
	} `json:"threadSettings"`
	Turn struct {
		ID     string          `json:"id"`
		Status string          `json:"status"`
		Error  json.RawMessage `json:"error"`
	} `json:"turn"`
	Item  EventItem `json:"item"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
	WillRetry  bool `json:"willRetry"`
	TokenUsage struct {
		Last struct {
			InputTokens int64 `json:"inputTokens"`
		} `json:"last"`
		ModelContextWindow int64 `json:"modelContextWindow"`
	} `json:"tokenUsage"`

	Failure             string `json:"-"`
	FailureKind         string `json:"-"`
	ModelServiceFailure bool   `json:"-"`
}

// EventItem is the app-server's live ThreadItem and its small fields used for approval and streaming.
type EventItem struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Text    string `json:"text"`
	Changes []struct {
		Path string `json:"path"`
	} `json:"changes"`
	Raw json.RawMessage `json:"-"`
}

func (it *EventItem) UnmarshalJSON(raw []byte) error {
	type plain EventItem
	var envelope struct {
		Payload json.RawMessage `json:"payload"`
		*plain
	}
	envelope.plain = (*plain)(it)
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}
	if len(envelope.Payload) > 0 && string(envelope.Payload) != "null" {
		raw = envelope.Payload
		if err := json.Unmarshal(raw, (*plain)(it)); err != nil {
			return err
		}
	}
	it.Raw = append(json.RawMessage(nil), raw...)
	return nil
}

// Paths lists the files in a file change so both approval modes show the same scope.
func (i EventItem) Paths() []string {
	paths := make([]string, len(i.Changes))
	for j, change := range i.Changes {
		paths[j] = change.Path
	}
	return paths
}

// DecodeEvent consumes a supported native notification and classifies the result of a turn once.
func DecodeEvent(method string, params json.RawMessage) (Event, bool) {
	var ev Event
	switch method {
	case "thread/settings/updated":
		ev.Kind = ThreadSettings
	case "thread/reverted":
		ev.Kind = ThreadReverted
	case "turn/started":
		ev.Kind = TurnStarted
	case "turn/completed":
		ev.Kind = TurnCompleted
	case "item/started":
		ev.Kind = ItemStarted
	case "item/completed":
		ev.Kind = ItemCompleted
	case "item/agentMessage/delta":
		ev.Kind = AgentMessageDelta
	case "item/reasoning/textDelta", "item/reasoning/summaryTextDelta":
		ev.Kind = ReasoningDelta
	case "thread/tokenUsage/updated":
		ev.Kind = TokenUsageUpdated
	case "thread/compacted":
		ev.Kind = ThreadCompacted
	case "error":
		ev.Kind = UnretriedError
	case "serverRequest/resolved":
		ev.Kind = RequestResolved
	default:
		return Event{}, false
	}
	if err := json.Unmarshal(params, &ev); err != nil || (ev.Kind == UnretriedError && ev.WillRetry) {
		return Event{}, false
	}
	if ev.Kind == TurnCompleted && ev.Turn.Status != "interrupted" {
		ev.Failure, ev.FailureKind, ev.ModelServiceFailure = transcript.CodexTurnErrorRecovery(ev.Turn.Error)
		if ev.Failure == "" && ev.Turn.Status != "completed" && ev.Turn.Status != "" {
			ev.Failure = transcript.CodexTurnFailed
		}
	}
	return ev, true
}
