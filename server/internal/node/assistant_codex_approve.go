package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/compat"
	"github.com/sunfayao/persona/server/internal/identity"
	"github.com/sunfayao/persona/server/internal/node/harness/codex/appserver"
	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/shellcmd"
)

// serve answers a request from the app-server. Approvals may wait for the phone, so each runs on its own.
func (c *codexConn) serve(m codexMessage) {
	ctx, cancel := context.WithCancel(context.Background())
	key := string(m.ID)
	c.mu.Lock()
	c.asks[key] = cancel
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.asks, key)
		c.mu.Unlock()
		cancel()
	}()
	result, err := c.decide(ctx, m.Method, m.Params)
	if err != nil {
		c.replyError(m.ID, -32601, err.Error())
		return
	}
	c.reply(m.ID, result)
}

// decide works out the answer to one app-server request.
func (c *codexConn) decide(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	n, p := c.p.ownerOr(c.n), c.p
	ctx = context.WithValue(ctx, codexApprovalOwner{}, c)
	var r appserver.Request
	_ = json.Unmarshal(raw, &r)
	mode, _ := n.modesOf(p.id)
	cwd := firstNonEmpty(r.Cwd, p.cwd) // where the request runs: what the gate and the memory checks go by
	// The card is the conversation's: placed in its directory, where it started, not a subfolder the request runs in
	// (identity.CardCwd, #2368).
	ix := proto.Interaction{
		ID: appserver.ApprovalID(p.id, method, r.ItemID, r.ApprovalID, r.ServerName), SessionID: p.id, Cwd: identity.CardCwd(p.cwd, cwd),
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(nodeclient.HookTimeout).UTC(), ToolUseID: r.ItemID,
		SessionTitle: n.assistantTitle(p.id, firstNonEmpty(p.cwd, r.Cwd)),
	}
	switch method {
	case "item/commandExecution/requestApproval":
		command := r.Command
		r.PopulateCard(method, &ix, nil)
		gateCard := ix
		gateCard.Kind = ""
		decision, reason, err := n.larkCheck(ctx, p.id, cwd, command, gateCard)
		if err != nil {
			return map[string]any{"decision": "cancel"}, nil
		}
		switch decision {
		case "allow":
			return map[string]any{"decision": "accept"}, nil
		case "deny":
			c.tellModel(reason)
			return map[string]any{"decision": "decline"}, nil
		}
		command = unwrapShell(command) // display and ordinary permission keys only, after the shared gate
		if mode == proto.ModeBypass || readsOnlyMemory(command, cwd, c.memDir) {
			return map[string]any{"decision": "accept"}, nil // Claude reads its auto-memory unasked too
		}
		ix.Summary = withReason(command, r.Reason)
		ix.AlwaysKeys = []string{"codex-command:" + command} // keep the legacy command scope
		// The hub remembers Always; accepting once keeps the next request revocable.
		return c.askPhone(ctx, ix, "accept", "accept", "decline"), nil

	case "item/fileChange/requestApproval":
		c.mu.Lock()
		paths := c.files[r.ItemID]
		c.mu.Unlock()
		// Writing its memory is unasked, like Claude's auto-memory (not in plan mode, which writes nothing).
		if mode == proto.ModeBypass || mode == "acceptEdits" || (mode != "plan" && r.GrantRoot == "" && allUnder(paths, cwd, c.memDir)) {
			return map[string]any{"decision": "accept"}, nil
		}
		r.PopulateCard(method, &ix, paths)
		ix.Summary = withReason(strings.Join(paths, "\n"), r.Reason)
		if ix.Summary == "" {
			ix.Summary = "修改文件"
		}
		for _, p := range paths {
			ix.AlwaysKeys = append(ix.AlwaysKeys, "codex-edit:"+p)
		}
		if r.GrantRoot != "" {
			ix.AlwaysKeys = append(ix.AlwaysKeys, "codex-root:"+r.GrantRoot)
		}
		return c.askPhone(ctx, ix, "accept", "accept", "decline"), nil

	case "mcpServer/elicitation/request":
		if r.Meta.Kind != "mcp_tool_call" {
			return map[string]any{"action": "decline", "content": nil, "_meta": nil}, nil // forms: nobody to fill them in
		}
		accept := map[string]any{"action": "accept", "content": map[string]any{}, "_meta": nil}
		// Persona's own tools run unasked; the hub asks before the ones that act for the user (mail_send, phone writes).
		if compat.IsMCPServer(r.ServerName) || mode == proto.ModeBypass {
			return accept, nil
		}
		r.PopulateCard(method, &ix, nil)
		res, ok, err := n.ask(ctx, ix)
		if err != nil || !ok || res.Allow == nil || !*res.Allow {
			if ok && res.Message != "" {
				c.tellModel(res.Message)
			}
			return map[string]any{"action": "decline", "content": nil, "_meta": nil}, nil
		}
		return accept, nil

	case "item/permissions/requestApproval":
		deny := map[string]any{"permissions": map[string]any{}, "scope": "turn"}
		granted := r.PermissionGrant()
		if mode != proto.ModeBypass {
			r.PopulateCard(method, &ix, nil)
			b, _ := json.Marshal(granted)
			ix.Summary = withReason("Codex 要更多权限："+string(b), r.Reason)
			ix.AlwaysKeys = []string{"codex-permissions:" + string(b)}
			res, ok, err := n.ask(ctx, ix)
			if err != nil || !ok || res.Allow == nil || !*res.Allow {
				return deny, nil
			}
			// The hub remembers Always; a runtime session grant would bypass a later revocation.
		}
		return map[string]any{"permissions": granted, "scope": "turn"}, nil

	case "item/tool/requestUserInput":
		r.PopulateCard(method, &ix, nil)
		answers := map[string]any{}
		res, ok, err := n.ask(ctx, ix)
		if err == nil && ok && !res.Cancel {
			for _, q := range r.Questions {
				if a, ok := res.Answers[q.Question]; ok {
					answers[q.ID] = map[string]any{"answers": []string{a}}
				}
			}
		}
		return map[string]any{"answers": answers}, nil
	}
	return nil, fmt.Errorf("persona does not handle %s", method)
}

// askPhone asks about a command / file change and maps the answer to Codex's decision names.
func (c *codexConn) askPhone(ctx context.Context, ix proto.Interaction, accept, always, decline string) map[string]any {
	res, ok, err := c.askResumable(ctx, ix)
	if err != nil || !ok || res.Allow == nil {
		return map[string]any{"decision": "cancel"}
	}
	if !*res.Allow {
		if res.Message != "" {
			c.tellModel(res.Message)
		}
		return map[string]any{"decision": decline}
	}
	if res.Always {
		return map[string]any{"decision": always}
	}
	return map[string]any{"decision": accept}
}

// tellModel passes why something was refused (the user's words on the phone, the gate's reason) into the running
// turn: Codex's "decline" carries no message, unlike a Claude hook's deny.
func (c *codexConn) tellModel(reason string) {
	if strings.TrimSpace(reason) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		c.mu.Lock()
		thread, turn := c.threadID, c.turnID
		c.mu.Unlock()
		if turn == "" {
			return
		}
		input := []map[string]any{{"type": "text", "text": prompts.RenderRefused(reason), "text_elements": []any{}}}
		if err := c.call(ctx, "turn/steer", map[string]any{"threadId": thread, "input": input, "expectedTurnId": turn}, nil); err != nil {
			c.p.ownerOr(c.n).logger.Printf("assistant codex %s: pass the refusal on: %v", short(thread), err)
		}
	}()
}

func withReason(s, reason string) string {
	if reason = strings.TrimSpace(reason); reason != "" && s != "" {
		return s + "\n\n" + reason
	}
	return firstNonEmpty(s, reason)
}

// unwrapShell shortens command text for display. Approval classification uses the original command.
func unwrapShell(cmd string) string {
	segs, ok := shellcmd.Split(cmd)
	if !ok || len(segs) != 1 {
		return cmd
	}
	w := segs[0].Words
	if len(w) == 3 && (w[1] == "-c" || w[1] == "-lc") {
		switch filepath.Base(w[0]) {
		case "sh", "bash", "zsh":
			return w[2]
		}
	}
	return cmd
}

func (n *Node) assistantTitle(id, cwd string) string {
	if p := n.transcriptPath(id); p != "" {
		if st, err := os.Stat(p); err == nil {
			if t := n.meta(p, st).title; t != "" {
				return t
			}
		}
	}
	return filepath.Base(cwd)
}
