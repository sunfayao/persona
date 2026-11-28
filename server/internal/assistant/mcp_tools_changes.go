package assistant

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// A cursor is process-local: rebuilding it after a manager restart invalidates old
// watches. Take it before listing tools so a concurrent link cannot be missed.
type mcpToolChanges struct {
	mu      sync.Mutex
	cursor  string
	changed chan struct{}
}

func (c *mcpToolChanges) snapshot() (string, <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed == nil {
		c.cursor, c.changed = randomHex(16), make(chan struct{})
	}
	return c.cursor, c.changed
}

func (c *mcpToolChanges) notify() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.changed != nil {
		close(c.changed)
	}
	c.cursor, c.changed = randomHex(16), make(chan struct{})
}

// mcpToolsHeartbeat is a discovery heartbeat, not a legacy startup wait. Revision-aware calls never use leaderListWait.
var mcpToolsHeartbeat = 30 * time.Second

// mcpToolsWatchEvent is a test-only observation hook. Production leaves it nil.
var mcpToolsWatchEvent func(string)

func noteMCPWatchEvent(event string) {
	if mcpToolsWatchEvent != nil {
		mcpToolsWatchEvent(event)
	}
}

type revisionDiscoveryKey struct{}

func (s *Service) sessionMCPToolsSince(ctx context.Context, machine, session string, assistant, guest bool, since string) map[string]any {
	if since != "" {
		ctx = context.WithValue(ctx, revisionDiscoveryKey{}, true)
	}
	ctx = context.WithValue(ctx, guardDecisionKey{}, &guardDecisionCapture{})
	base, changed := s.mcpChanges.snapshot()
	cursor := s.toolsCursor(ctx, machine, session, assistant, base)
	if since == cursor {
		noteMCPWatchEvent("wait")
		timer := time.NewTimer(mcpToolsHeartbeat)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return map[string]any{"revision": cursor, "unchanged": true}
		case <-timer.C:
			noteMCPWatchEvent("heartbeat")
			// A heartbeat also rechecks filter state: expiry and read recovery do not call notify themselves.
			base, _ = s.mcpChanges.snapshot()
			ctx = context.WithValue(ctx, guardDecisionKey{}, &guardDecisionCapture{})
			next := s.toolsCursor(ctx, machine, session, assistant, base)
			if next == cursor {
				return map[string]any{"revision": cursor, "unchanged": true}
			}
			cursor = next
		case <-changed:
			base, _ = s.mcpChanges.snapshot()
			ctx = context.WithValue(ctx, guardDecisionKey{}, &guardDecisionCapture{})
			cursor = s.toolsCursor(ctx, machine, session, assistant, base)
		}
	}
	// Filter with a fresh capture and return its decision as the revision too. An admission read and a list read may
	// disagree transiently, but the list and revision a client caches always come from the same effective decision.
	ctx = context.WithValue(ctx, guardDecisionKey{}, &guardDecisionCapture{})
	tools, err := s.sessionMCPToolsChecked(ctx, machine, session, assistant, guest)
	if err != nil {
		return map[string]any{"error": err.Error()}
	}
	if tools == nil {
		tools = []proto.Tool{}
	}
	return map[string]any{"tools": tools, "revision": s.toolsCursor(ctx, machine, session, assistant, base)}
}

// extensionToolsChanged is extmgr's word that the extensions' tools may have changed (extmgr Options.ToolsChanged: an
// extension configured, switched, installed, upgraded, removed or started again). Every tool watch lists again, and a
// conversation whose tools did change is sent tools/list_changed, so turning ego on for a Mac brings browser_look and
// browser_run to a running conversation at once.
func (s *Service) extensionToolsChanged() { s.mcpChanges.notify() }

// extensionToolsWait bounds the extensions' answer for a revision: a manager whose extensions are still starting does
// not hold a tool watch up.
const extensionToolsWait = 2 * time.Second

// extensionToolsDigest is a digest of the tools the extensions offer this kind of conversation now (Cua's, ego's, the
// connectors'), their order aside. The tool-list revision carries it (toolsCursor), so a change there that nothing
// announced still reaches a running conversation at its watch's next heartbeat. "" without extensions, or when they
// do not answer within extensionToolsWait.
func (s *Service) extensionToolsDigest(ctx context.Context, assistant bool) string {
	if s.ext == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, extensionToolsWait)
	defer cancel()
	agents, assistantOnly := s.ext.AgentTools(ctx, assistant)
	if len(agents)+len(assistantOnly) == 0 {
		return ""
	}
	items := make([]string, 0, len(agents)+len(assistantOnly))
	for _, t := range slices.Concat(agents, assistantOnly) {
		items = append(items, t.Name+"\x00"+t.Description+"\x00"+string(t.InputSchema))
	}
	slices.Sort(items)
	h := sha256.New()
	for _, item := range items {
		h.Write([]byte(item))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// toolsCursor is the revision of machine's conversation session's tool list: the guard's (guardDiscoveryCursor, on
// base) and the extensions' tools (extensionToolsDigest).
func (s *Service) toolsCursor(ctx context.Context, machine, session string, assistant bool, base string) string {
	cursor := s.guardDiscoveryCursor(ctx, machine, session, assistant, base)
	if digest := s.extensionToolsDigest(ctx, assistant); digest != "" {
		cursor += ":ext=" + digest
	}
	return cursor + s.toolNotesRevision(ctx, assistant)
}
