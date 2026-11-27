package codex

import "github.com/sunfayao/persona/server/internal/proto"

// PermissionMode is the permission mode a Codex thread ran its latest turn in, in the words of the modes a Codex start
// takes: Full access (bypassPermissions) for Codex's own full access, no approvals and no sandbox, as
// --dangerously-bypass-approvals-and-sandbox and the App's Full access run it; default, Codex's own approval, for any
// other policy. approval and sandbox are the turn_context's approval_policy and sandbox_policy.type of its rollout. ""
// when the rollout names neither, so nothing is shown rather than a guess.
func PermissionMode(approval, sandbox string) string {
	switch {
	case approval == "" && sandbox == "":
		return ""
	case approval == "never" && sandbox == "danger-full-access":
		return proto.ModeBypass
	}
	return proto.ModeDefault
}
