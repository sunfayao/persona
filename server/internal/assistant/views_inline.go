package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/sdk/go/wire"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
)

type inlineViewStore interface {
	RegisterAssistantViewBlock(context.Context, db.AssistantViewBlockRow) (bool, error)
	AssistantViewBlockKeys(context.Context, string) ([]string, error)
	RecordAssistantViewErrors(context.Context, string, string, string, []string, time.Time) error
}

func (s *Service) registerInlineViews(parent context.Context, p SessionItems) bool {
	st, ok := s.store().(inlineViewStore)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	if session, known := s.session(ctx, p.MachineID, p.SessionID); !known || !session.Assistant {
		return false
	}
	complete := true
	for _, it := range p.Items {
		if it.ID == "" || it.Role != "assistant" || it.Kind != proto.KindText {
			continue
		}
		if it.FullSize > 0 {
			var full proto.Item
			if err := s.call(ctx, p.MachineID, "session.item", map[string]any{"session_id": p.SessionID, "item_id": it.ID}, &full); err != nil || full.ID != it.ID || full.Role != "assistant" || full.Kind != proto.KindText || full.FullSize > 0 {
				complete = false
				continue
			}
			it = full
		}
		if !wire.HasAssistantView(it.Text) {
			continue
		}
		at := it.TS
		if at.IsZero() {
			at = time.Now().UTC()
		}
		var problems []string
		valid := 0 // complete views that passed and are registered
		for _, b := range wire.AssistantViewBlocks(it.Text) {
			if b.Language != "persona-view" {
				continue
			}
			v, err := b.Parse()
			if err != nil {
				problems = append(problems, scrubViewError(scrubViewNames(fmt.Sprintf("persona-view block %d: %v", b.Index, err), b.Raw)))
				continue
			}
			raw, err := json.Marshal(v)
			if err != nil {
				continue
			}
			row := db.AssistantViewBlockRow{Conversation: p.SessionID, Key: wire.AssistantBlockKey(it.ID, b.Index), MachineID: p.MachineID, ItemID: it.ID, Index: b.Index, View: raw, CreatedAt: at}
			created, err := st.RegisterAssistantViewBlock(ctx, row)
			if err != nil {
				complete = false
				s.logf("assistant inline view registration failed: %v", err)
				continue
			}
			valid++
			if created {
				s.event(ctx, map[string]any{"type": "assistant_view_block", "conversation": p.SessionID, "key": row.Key})
			}
		}
		if len(problems) == 0 {
			if valid == 0 {
				continue // no card at all (only talks about one): it neither fails nor redoes anything
			}
			at = it.TS // a passing reply clears earlier problems only by when it was really written, not when it was replayed
		}
		if err := st.RecordAssistantViewErrors(ctx, p.SessionID, p.MachineID, it.ID, problems, at); err != nil {
			complete = false
			s.logf("assistant view errors could not be recorded: %v", err)
		}
	}
	return complete
}

type inlineMainSnapshot struct {
	Ref       SessionRef
	Rev       int64
	UpdatedAt time.Time
}

type inlineMainBook struct {
	mu      sync.Mutex
	checked inlineMainSnapshot
}

func (s *Service) inlineMainSnapshot(ctx context.Context, ref SessionRef) (inlineMainSnapshot, bool) {
	cfg := s.settings(ctx)
	if cfg.Main != ref.SessionID || !s.sameMachine(cfg.mainRouteMachine(), ref.MachineID) {
		return inlineMainSnapshot{}, false
	}
	row, known := s.session(ctx, ref.MachineID, ref.SessionID)
	if !known || !row.Assistant || row.Launcher != "agy" || row.Rev <= 0 || (row.Status != proto.StatusDone && row.Status != proto.StatusIdle) {
		return inlineMainSnapshot{}, false
	}
	return inlineMainSnapshot{Ref: ref, Rev: row.Rev, UpdatedAt: row.UpdatedAt}, true
}

// reconcileInlineMain repairs a missed completion after the runtime exports its final reply. Use the current
// main chat, a settled revision and the native tail. Never promote a previous turn or a cached partial page.
func (s *Service) reconcileInlineMain(parent context.Context, ref SessionRef) {
	if _, ok := s.store().(inlineViewStore); !ok {
		return
	}
	book := &s.main.inline
	if !book.mu.TryLock() {
		return
	}
	defer book.mu.Unlock()
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	before, ok := s.inlineMainSnapshot(ctx, ref)
	if !ok || before == book.checked {
		return
	}
	var tail historyPage
	if err := s.historySource(ctx, ref.MachineID, DeviceSessionItems, map[string]any{"session_id": ref.SessionID, "limit": 1, "lazy_images": true}, &tail); err != nil || tail.Partial || tail.MoreAfter || int64(intValue(tail.ToSeq)) != before.Rev {
		return
	}
	after, ok := s.inlineMainSnapshot(ctx, ref)
	if !ok || after != before {
		return
	}
	// agy's sequence is the native step. One sequence may have thought and text items. Only its final text
	// can answer, and only under the stable source item key that the client and the action endpoint both use.
	for _, it := range tail.Items {
		if int64(it.Seq) == before.Rev && it.ID != "" && it.Role == "assistant" && it.Kind == proto.KindText {
			if !s.registerInlineViews(context.WithValue(ctx, historySourceKey{}, true), SessionItems{MachineID: ref.MachineID, SessionID: ref.SessionID, Items: []proto.Item{it}}) {
				return
			}
			break
		}
	}
	book.checked = before
}

// registerInlineTurn also registers cards when no app was watching the conversation during its reply.
func (s *Service) registerInlineTurn(parent context.Context, r SessionRef) {
	if _, ok := s.store().(inlineViewStore); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	if session, known := s.session(ctx, r.MachineID, r.SessionID); !known || !session.Assistant {
		return
	}
	var result struct {
		Items []proto.Item `json:"items"`
	}
	if err := s.call(ctx, r.MachineID, DeviceSessionItems, map[string]any{"session_id": r.SessionID, "turns": 1, "lazy_images": true}, &result); err != nil {
		return
	}
	s.registerInlineViews(ctx, SessionItems{MachineID: r.MachineID, SessionID: r.SessionID, Items: result.Items})
}

// registerInlineTurnSettled re-runs registerInlineTurn after the turn settles so runtimes that flush their native
// transcript after TurnEnded (replacing temporary live/unexported item ids with final persisted ids) still register
// their final persona-view blocks idempotently.
func (s *Service) registerInlineTurnSettled(ctx context.Context, machine, sid string) {
	s.registerInlineTurn(ctx, SessionRef{MachineID: machine, SessionID: sid})
}

func (s *Service) conversationViewBlockKeys(ctx context.Context, conversation string) ([]string, error) {
	st, ok := s.store().(inlineViewStore)
	if !ok {
		return []string{}, nil
	}
	return st.AssistantViewBlockKeys(ctx, conversation)
}

// viewName is a page name or action id that can be said back to the model as it is.
var viewName = lazyre.New(`^[A-Za-z0-9_\-]{1,40}$`)

// scrubViewNames replaces the page names and action ids of a failed view (raw) in a check's message with "…", unless
// they are short plain names: the checks write them unquoted ("pages.<name>: …", "action.<id>: …"), and a name can
// be anything the model wrote, a long sentence or markup included. The reason after them is kept.
func scrubViewNames(msg string, raw json.RawMessage) string {
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return msg // not JSON: the check's message has no names in it
	}
	var names []string
	var walk func(v any, pages bool)
	walk = func(v any, pages bool) {
		switch v := v.(type) {
		case map[string]any:
			for k, child := range v {
				if pages {
					names = append(names, k)
				}
				if id, ok := child.(string); ok && k == "id" {
					names = append(names, id)
				}
				walk(child, k == "pages" && !pages)
			}
		case []any:
			for _, child := range v {
				walk(child, false)
			}
		}
	}
	walk(doc, false)
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) }) // a name inside a longer one
	for _, n := range names {
		if n != "" && !viewName.MatchString(n) {
			for _, at := range []string{"pages.", "action.", "action "} { // where the checks name them
				msg = strings.ReplaceAll(msg, at+n, at+"…")
			}
		}
	}
	return msg
}
