package appserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
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
