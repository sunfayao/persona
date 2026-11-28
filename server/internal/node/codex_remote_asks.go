package node

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex/appserver"
	"github.com/sunfayao/persona/server/internal/nodeclient"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// Shared-server observers never auto-answer, including errors, declines, timeouts and unsupported requests.
func (c *codexRemote) request(m codexMessage) {
	var r appserver.Request
	if json.Unmarshal(m.Params, &r) != nil {
		return
	}
	key := appserver.RequestKey(m.ID)
	if key == "" {
		return
	}
	c.mu.Lock()
	t, ok := c.threads[r.ThreadID]
	if !c.ready || !ok || !t.joined || c.finished[key] || c.asks[key] != nil {
		c.mu.Unlock()
		return
	}
	paths := append([]string(nil), t.files[r.ItemID]...)
	c.mu.Unlock()
	c.n.mu.Lock()
	machineID := c.n.machineID
	c.n.mu.Unlock()
	ix := proto.Interaction{ID: appserver.ObservedRequestID(machineID, c.spec.ID, c.generation, r.ThreadID, m.ID), SessionID: t.publicID, Cwd: firstNonEmpty(r.Cwd, t.cwd), SessionTitle: t.title, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(nodeclient.HookTimeout).UTC(), ToolUseID: r.ItemID,
		Agent: "codex", Reason: strings.TrimSpace(r.Reason)}
	if c.n.sessionBlocked(ix.SessionID, ix.Cwd) {
		return // a blocked Agent folder (agentdirs.go): no card; an observer never answers, Codex's own client decides
	}
	r.Command = unwrapShell(r.Command)
	r.PopulateCard(m.Method, &ix, paths)
	var response func(proto.Resolution) (any, bool)
	switch m.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if m.Method == "item/commandExecution/requestApproval" {
			ix.AlwaysLabel, ix.Summary = "本会话不再问这个命令", r.Command
		} else {
			ix.AlwaysLabel = "本会话不再问这些文件"
			ix.Summary = strings.Join(paths, "\n")
			if ix.Summary == "" {
				ix.Summary = firstNonEmpty(r.GrantRoot, "修改文件")
			}
		}
		response = func(res proto.Resolution) (any, bool) {
			if res.Allow == nil {
				return nil, false
			}
			d := "decline"
			if *res.Allow {
				d = "accept"
				if res.Always {
					d = "acceptForSession"
				}
			}
			return map[string]any{"decision": d}, true
		}
	case "item/permissions/requestApproval":
		ix.Summary = "Codex 要更多权限：" + compactJSON(r.Permissions)
		ix.AlwaysLabel = "本会话都给这些权限"
		response = func(res proto.Resolution) (any, bool) {
			if res.Allow == nil {
				return nil, false
			}
			p := map[string]any{}
			scope := "turn"
			if *res.Allow {
				p = r.PermissionGrant()
				if res.Always {
					scope = "session"
				}
			}
			return map[string]any{"permissions": p, "scope": scope}, true
		}
	case "item/tool/requestUserInput":
		response = func(res proto.Resolution) (any, bool) {
			answers := map[string]any{}
			if res.Cancel {
				return map[string]any{"answers": answers}, true
			}
			for _, q := range r.Questions {
				a, ok := res.Answers[q.Question]
				if !ok {
					return nil, false
				}
				answers[q.ID] = map[string]any{"answers": []string{a}}
			}
			return map[string]any{"answers": answers}, len(answers) > 0
		}
	case "mcpServer/elicitation/request":
		if r.Meta.Kind != "mcp_tool_call" {
			return
		}
		response = func(res proto.Resolution) (any, bool) {
			if res.Allow == nil {
				return nil, false
			}
			action := "decline"
			var content any
			if *res.Allow {
				action = "accept"
				content = map[string]any{}
			}
			return map[string]any{"action": action, "content": content, "_meta": nil}, true
		}
	default:
		return
	}
	ctx, cancel := context.WithCancel(c.ctx)
	ask := &remoteAsk{cancel: cancel, thread: r.ThreadID, interaction: ix.ID}
	c.mu.Lock()
	if !c.ready || c.finished[key] || c.asks[key] != nil {
		c.mu.Unlock()
		cancel()
		return
	}
	c.asks[key] = ask
	c.mu.Unlock()
	go func() {
		defer cancel()
		if ctx.Err() != nil {
			return
		}
		res, ok, err := c.n.ask(ctx, ix)
		c.mu.Lock()
		current := c.asks[key] == ask
		if current {
			delete(c.asks, key)
		}
		joined := c.threads[r.ThreadID].joined
		ready := c.ready && joined
		c.mu.Unlock()
		if !current || !ready || ctx.Err() != nil || err != nil || !ok || res.Dismiss {
			return
		}
		owner, _, err := c.n.codexOwner(t.publicID)
		if err != nil || owner != c {
			return
		}
		result, valid := response(res)
		if !valid {
			return
		}
		c.mu.Lock()
		c.finished[key] = true
		c.mu.Unlock()
		c.wire.Reply(m.ID, result)
		// A refusal with the user's words: Codex's decline carries none, so they go into the turn after it (as the
		// assistant's tellModel does).
		if ix.Kind == proto.IxPermission && res.Allow != nil && !*res.Allow && strings.TrimSpace(res.Message) != "" {
			go func() {
				sctx, scancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer scancel()
				if err := c.n.sendCodex(sctx, t.publicID, prompts.RenderRefused(strings.TrimSpace(res.Message)), nil); err != nil {
					c.n.logf("codex %s: pass the refusal on: %v", t.publicID, err)
				}
			}()
		}
	}()
}

// compactJSON is a raw JSON value on one line ("" for none).
func compactJSON(raw json.RawMessage) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}
