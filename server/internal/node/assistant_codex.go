package node

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
	"github.com/sunfayao/persona/server/internal/node/harness/codex/appserver"
)

// The assistant on a Codex launcher runs `codex app-server` (Codex's JSON-RPC server for IDEs, newline-delimited JSON
// on stdio; protocol: `codex app-server generate-ts`) instead of claude -p. One app-server per conversation, one
// thread in it: the thread id is the session id, its rollout is the transcript (transcript/codex.go), so the app
// shows it like any other conversation. docs/assistant/assistant.md "Codex".
//
// What the node does in between, compared with the Claude version:
//   - messages: turn/start when idle, turn/steer while a turn runs (Claude's queued messages are taken up mid-turn too);
//   - live text: item/agentMessage/delta → session.delta;
//   - stop: turn/interrupt (the process stays);
//   - approvals come to the node as requests (commands, file changes, MCP tool calls, permissions, request_user_input):
//     the Feishu gate first (larkCheck), Persona's own MCP tools pass, the rest by the conversation's mode, asking
//     the phone like Claude's PermissionRequest hook does. Full access uses the native never policy;
//     connector proxies and gated CLI wrappers still enforce their independent business confirmations.

// codexToolTimeout: ask_user waits for the phone (up to nodeclient.HookTimeout); Codex's default MCP tool timeout is 60 s.
const codexToolTimeout = 86400

// codexStartTimeout bounds the app-server's startup handshake (initialize, thread/start or thread/resume).
const codexStartTimeout = 90 * time.Second

type codexMessage = appserver.Message
type codexError = appserver.RPCError

type codexConn struct {
	n      *Node
	p      *assistantProc
	key    string // what the persona MCP server reports as its session until the thread exists (resolveAssistantKey)
	memDir string // the assistant's memory, shared with Claude (codexMemoryPrompt)
	// nameGates tells the gate proxy the thread's id once there is one: the process's addresses were opened before it
	// (assistantLarkEnvNamed). nil when the id was known at the start.
	nameGates     func(id string)
	transport     *appserver.Wire
	transportOnce sync.Once

	mu                  sync.Mutex
	asks                map[string]context.CancelFunc // server request id → the phone question waiting for it
	turnID              string                        // the turn running, "" when idle
	files               map[string][]string           // fileChange item id → paths, for the approval card
	threadID            string
	speedModel          string
	serviceTier         string
	serviceTierRevision uint64
	models              map[string]codex.ModelEfforts
	loop                codexCompactionLoop // the running turn's compactions that made no room (#10278)
}

func (c *codexConn) turn() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turnID
}

func short(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// resolveAssistantKey turns the key a Codex conversation's MCP server was started with into its session id (the
// thread id, known only after thread/start). Anything else is returned as it is.
func (n *Node) resolveAssistantKey(id string) string {
	if !strings.HasPrefix(id, "proc-") {
		return id
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if p := n.akeys[id]; p != nil && p.id != "" {
		return p.id
	}
	return id
}

func (c *codexConn) rpcWire() *appserver.Wire {
	c.transportOnce.Do(func() {
		if c.transport == nil {
			c.transport = c.n.codexClient().Bind(c.p.write, c.p.exited, func(m codexMessage) {
				if m.Method != "" && len(m.ID) > 0 {
					go c.serve(m)
				} else if m.Method != "" {
					c.notification(m.Method, m.Params)
				}
			})
		}
	})
	return c.transport
}

func (c *codexConn) send(v any) error { return c.rpcWire().Send(v) }

func (c *codexConn) call(ctx context.Context, method string, params any, out any) error {
	return c.rpcWire().Call(ctx, method, params, out)
}

func (c *codexConn) reply(id json.RawMessage, result any) { c.rpcWire().Reply(id, result) }
func (c *codexConn) replyError(id json.RawMessage, code int, msg string) {
	c.rpcWire().ReplyError(id, code, msg)
}
