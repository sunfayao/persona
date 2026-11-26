package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nwverse/persona/server/internal/connector"
)

const chatRulesKey = "connector_chat_rules"

// readUserRules reads stored rules and skips malformed elements with a log entry.
func (h *Manager) readUserRules(ctx context.Context) ([]connector.UserRule, error) {
	if h.store == nil {
		return nil, errNoDatabase
	}
	var raw json.RawMessage
	if _, err := h.store.Setting(ctx, chatRulesKey, &raw); err != nil {
		return nil, fmt.Errorf("read %s: %w", chatRulesKey, err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var elements []json.RawMessage
	if err := json.Unmarshal(raw, &elements); err != nil {
		h.logf("user rules: %s is not a list (%v): taken as none, the gate asks", chatRulesKey, err)
		return nil, nil
	}
	var list []connector.UserRule
	for i, e := range elements {
		var u *connector.UserRule // a null leaves it nil: no rule either
		err := json.Unmarshal(e, &u)
		if err == nil && u == nil {
			err = errors.New("null")
		}
		if err != nil {
			h.logf("user rules: element %d of %s is not a rule (%v): skipped", i, chatRulesKey, err)
			continue
		}
		list = append(list, *u)
	}
	return list, nil
}
