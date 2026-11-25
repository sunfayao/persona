package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A persona_settings change is the user's to approve (#3722): only their own allow applies it and only their own deny
// is a decline. A card that expires unanswered, is closed, or that someone else answers for them changes nothing and
// says nobody answered, so the conversation never reports that the user refused.

const interjectKey = "assistant.interject_background"

// setFrom has conversation session ask to turn interject_background on; the tool's result comes on the channel.
func setFrom(s *Service, session string) chan string {
	done := make(chan string, 1)
	go func() {
		r := s.Tool(context.Background(), ToolCall{Name: "persona_settings", MachineID: "mac", SessionID: session,
			Arguments: json.RawMessage(`{"action":"set","key":"` + interjectKey + `","value":true}`)})
		done <- r.Content[0].Text
	}()
	return done
}

func interjectOn(t *testing.T, s *Service) bool {
	t.Helper()
	r := s.Tool(context.Background(), ToolCall{Name: "persona_settings", Arguments: json.RawMessage(`{"action":"get","key":"` + interjectKey + `"}`)})
	return r.Content[0].Text == "true"
}

func resultOf(t *testing.T, done chan string, wait time.Duration) string {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-time.After(wait):
		t.Fatal("persona_settings did not return")
		return ""
	}
}
