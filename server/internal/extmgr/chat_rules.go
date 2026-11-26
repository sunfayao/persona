package extmgr

import (
	"context"
	"errors"

	"github.com/sunfayao/persona/server/internal/connector"
)

// validChatRules are the rules of list that a gate could run (connector.UserRule.Validate), one to a chat, the first: duplicate or invalid rules remain in storage.
func validChatRules(list []connector.UserRule) []connector.UserRule {
	var out []connector.UserRule
	seen := map[string]bool{}
	for _, u := range list {
		if u.Validate() == nil && !seen[u.ID()] {
			seen[u.ID()] = true
			out = append(out, u)
		}
	}
	return out
}

// startConnectorGate loads the configured gate and the per-chat rules stored by earlier managers.
func (h *Manager) startConnectorGate(ctx context.Context) {
	h.connectors = h.newConnectors(h.cfg.ConnectorGate)
	rules, err := h.readUserRules(ctx)
	if err != nil {
		if !errors.Is(err, errNoDatabase) {
			h.logf("user rules: %v", err)
		}
		return
	}
	if err := h.connectors.SetUserRules(validChatRules(rules)); err != nil {
		h.logf("user rules: %v", err)
	}
}
