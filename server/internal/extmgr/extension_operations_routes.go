package extmgr

import (
	"encoding/json"
	"net/http"

	"github.com/sunfayao/persona/server/internal/i18n"
)

// withOperation is v's JSON with "operation_id" added (the synchronous answers name their operation too).
func withOperation(v any, op *extOp) any {
	if op == nil {
		return v
	}
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return v
	}
	m["operation_id"], _ = json.Marshal(op.v.ID) // set before the operation is shared, never changed
	return m
}

// writeOp answers 202 with op.
func (h *Manager) writeOp(w http.ResponseWriter, op *extOp) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	raw, err := json.Marshal(map[string]any{"operation": h.snapshot(op)})
	if err == nil {
		_, _ = w.Write(i18n.ExtensionClientJSON(i18n.OfWriter(w), raw))
	}
}
