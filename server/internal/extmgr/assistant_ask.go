package extmgr

import (
	"context"
	"errors"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// assistantAsk is a call to the assistant in this process.
func (h *Manager) assistantAsk(ctx context.Context, method string, params, out any) error {
	err := h.callAssistant(ctx, method, params, out)
	if errors.Is(err, errAssistantOffline) { // coded as the bus did: an extension's program checks the code
		return &rpc.Error{Code: assistantwire.CodeAssistantOffline, Message: err.Error()}
	}
	return err
}

// assistantOffline: err is the assistant not being there now (not connected yet: try again).
func assistantOffline(err error) bool {
	var re *rpc.Error
	return errors.Is(err, errAssistantOffline) || errors.As(err, &re) && re.Code == assistantwire.CodeAssistantOffline
}
