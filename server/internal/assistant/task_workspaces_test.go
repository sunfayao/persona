package assistant

import (
	"context"
	"encoding/json"
)

// Interleave another Manager call after registration, before the first prepare reaches Node.
type heldWorkspacePrepareHub struct {
	Hub
	beforePrepare func()
}

func (h *heldWorkspacePrepareHub) DeviceCall(ctx context.Context, call DeviceCall) (json.RawMessage, error) {
	if call.Method == "task.workspace.prepare" && h.beforePrepare != nil {
		before := h.beforePrepare
		h.beforePrepare = nil
		before()
	}
	return h.Hub.DeviceCall(ctx, call)
}
