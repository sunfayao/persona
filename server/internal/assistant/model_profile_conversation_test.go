package assistant

import (
	"context"
	"encoding/json"

	"github.com/nwverse/persona/server/internal/proto"
)

func (h *spawnHarness) openConversation(args string) proto.ToolResult {
	return h.s.Tool(context.Background(), ToolCall{Name: "conversation_new", MachineID: "server", SessionID: h.parent.SessionID, Arguments: json.RawMessage(args)})
}
