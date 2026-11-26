package extmgr

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// extensionAssistantCall is assistant.conversations / assistant.messages from extension id (#1288): the user's
// conversations with the assistant, read-only, for an extension granted assistant.read; and assistant.memory, the
// assistant's memory entries, read-only, for one granted assistant.memory.read. Each is checked against the grant, as
// kb.read is (the legacy full-hub grant covers them). The manager owns both and answers them.
func (ps *extensions) extensionAssistantCall(ctx context.Context, id, method string, params json.RawMessage) (any, error) {
	var scope string
	switch method {
	case assistantwire.MethodAssistantConversations, assistantwire.MethodAssistantMessages:
		scope = extension.ScopeAssistantRead
	case assistantwire.MethodAssistantEntriesList:
		scope = extension.ScopeAssistantEntriesRead
	case assistantwire.MethodAssistantMemory:
		scope = extension.ScopeAssistantMemoryRead
	default:
		return nil, &rpc.Error{Code: extension.CodeUnknownMethod, Message: "unknown method " + method}
	}
	if ps.reg == nil || !ps.reg.HubScopeAllowed(id, scope) {
		data, _ := json.Marshal(map[string]string{"scope": scope})
		return nil, &rpc.Error{Code: extension.CodePermissionDenied, Message: method + ": requires " + scope, Data: data}
	}
	if ps.h == nil {
		return nil, &rpc.Error{Code: assistantwire.CodeAssistantOffline, Message: errAssistantOffline.Error()}
	}
	var out json.RawMessage
	if method == assistantwire.MethodAssistantEntriesList {
		// The entries ledger names its reader itself (#13106).
		if err := ps.h.assistantAsk(assistantwire.WithEntriesReader(ctx, id), method, params, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	// Say which extension reads (#13246): what it is shown follows its trust and grants. An assistant from before that
	// answers the plain method, as a third party's read with no grant.
	err := ps.h.assistantAsk(ctx, assistantwire.MethodAssistantExtensionRead,
		assistantwire.AssistantExtensionRead{Extension: id, Method: method, Params: params}, &out)
	var re *rpc.Error
	if errors.As(err, &re) && re.Code == extension.CodeUnknownMethod {
		err = ps.h.assistantAsk(ctx, method, params, &out) // assistantAsk: the manager's answer, assistant_offline coded
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
