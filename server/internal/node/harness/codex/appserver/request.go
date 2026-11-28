package appserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/sunfayao/persona/server/internal/proto"
)

// Request is the union of app-server request params the node reads. Used by the assistant path (one app-server per
// conversation, one thread in it) and by the remote/shared-server observer path, so the two cannot drift field names.
// Every field that either side needs lives here.
type Request struct {
	ThreadID   string `json:"threadId"`
	TurnID     string `json:"turnId"`
	ItemID     string `json:"itemId"`
	ApprovalID string `json:"approvalId"`
	Kind       string `json:"kind"`
	Reason     string `json:"reason"`
	Command    string `json:"command"`
	Cwd        string `json:"cwd"`
	GrantRoot  string `json:"grantRoot"`
	ServerName string `json:"serverName"`
	Message    string `json:"message"`
	Mode       string `json:"mode"`
	Meta       struct {
		Kind       string         `json:"codex_approval_kind"`
		ToolParams map[string]any `json:"tool_params"`
	} `json:"_meta"`
	Questions []struct {
		ID       string `json:"id"`
		Header   string `json:"header"`
		Question string `json:"question"`
		Options  []struct {
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"options"`
	} `json:"questions"`
	Permissions json.RawMessage `json:"permissions"`
}

// PopulateCard projects the native request fields shared by assistant and observed interactions.
// Callers attach identity, authorization and the scopes their connection can grant.
func (r Request) PopulateCard(method string, card *proto.Interaction, paths []string) {
	switch method {
	case "item/commandExecution/requestApproval":
		card.Kind, card.ToolName = proto.IxPermission, "Bash"
		card.ToolInput, _ = json.Marshal(map[string]string{"command": r.Command})
	case "item/fileChange/requestApproval":
		card.Kind, card.ToolName = proto.IxPermission, "Edit"
		file := r.GrantRoot
		for _, path := range paths {
			if path != "" {
				file = path
				break
			}
		}
		card.ToolInput, _ = json.Marshal(map[string]string{"file_path": file})
	case "item/permissions/requestApproval":
		card.Kind, card.ToolName, card.ToolInput = proto.IxPermission, "权限", r.Permissions // i18n-ignore: legacy tool name
	case "mcpServer/elicitation/request":
		card.Kind, card.ToolName, card.Summary = proto.IxPermission, "mcp__"+r.ServerName, r.Message
		card.ToolInput, _ = json.Marshal(r.Meta.ToolParams)
	case "item/tool/requestUserInput":
		card.Kind, card.ToolName, card.CanCancel = proto.IxQuestion, "request_user_input", true
		for _, q := range r.Questions {
			row := proto.Question{Question: q.Question, Header: q.Header}
			for _, option := range q.Options {
				row.Options = append(row.Options, proto.QuestionOption{Label: option.Label, Description: option.Description})
			}
			card.Questions = append(card.Questions, row)
		}
	}
}

// PermissionGrant preserves only the scopes actually requested by the app-server.
func (r Request) PermissionGrant() map[string]any {
	permissions := map[string]any{}
	_ = json.Unmarshal(r.Permissions, &permissions)
	for key, scope := range permissions {
		if scope == nil {
			delete(permissions, key)
		}
	}
	if permissions == nil {
		return map[string]any{}
	}
	return permissions
}

// ApprovalID identifies a request on an assistant-owned server.
func ApprovalID(session, method, item, approval, server string) string {
	return "ix_" + interactionHash(session, method, item, approval, server)[:16]
}

// ObservedRequestID identifies a request on a shared server and its connection generation.
func ObservedRequestID(machine, instance, generation, thread string, id json.RawMessage) string {
	return "codex-" + interactionHash(machine, instance, generation, thread, RequestKey(id))
}

func interactionHash(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}
