// Package agentrows holds the pure parts of agents_list's rows: how they are ordered and what model they name.
package agentrows

import "github.com/sunfayao/persona/server/internal/proto"

// Rank orders rows: needs you, working, finished unread (and asking the user), then every other row together, so
// that the latest update comes first among them. state is the row's listing state (needs_you, working, ...).
func Rank(state string, awaitsReply bool) int {
	switch state {
	case proto.StateNeedsYou:
		return 0
	case proto.StateWorking:
		return 1
	case proto.StateUnread:
		return 2
	}
	if awaitsReply {
		return 2
	}
	return 3
}

// Model is the model c runs on: the transcript's, else the one Persona started it with.
func Model(c proto.Session) string {
	if c.Model != "" {
		return c.Model
	}
	if c.RequestedModel != nil {
		return *c.RequestedModel
	}
	return ""
}
