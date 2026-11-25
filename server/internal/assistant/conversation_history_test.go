package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/nwverse/persona/server/internal/conversationlog"
	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// historyRuntime is a fake normalized runtime. No CLI or model process is started.
type historyRuntime struct {
	mu      sync.Mutex
	items   []proto.Item
	history string
	offline bool
	reads   int
	after   []int
}

func (rt *historyRuntime) page(_ string, raw json.RawMessage) (any, error) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reads++
	if rt.offline {
		return nil, errors.New("fake runtime is offline")
	}
	var p historyParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	if p.Strict {
		return map[string]any{"items": rt.items, "strict_read": true}, nil
	}
	from, end := 0, 0
	if p.After != nil {
		from = *p.After
		rt.after = append(rt.after, from)
	}
	for _, it := range rt.items {
		end = max(end, it.Seq)
	}
	end = max(end, from)
	items := []proto.Item{}
	for _, it := range rt.items {
		if it.Seq <= from || (p.Before != nil && *p.Before > 0 && it.Seq >= *p.Before) {
			continue
		}
		items = append(items, it)
	}
	limit := p.Limit
	if limit <= 0 {
		limit = 500
	}
	if p.Turns > 0 {
		limit = p.Turns * 2
	}
	more, hasMore := false, false
	if len(items) > limit {
		if p.After != nil {
			items = items[:limit]
			end = items[len(items)-1].Seq
			more = true
		} else {
			items = items[len(items)-limit:]
			from = items[0].Seq - 1
			hasMore = true
		}
	}
	if p.Before != nil && *p.Before > 0 {
		end = *p.Before - 1
	}
	return map[string]any{"items": items, "history": rt.history, "from_seq": from, "to_seq": end,
		"has_more": hasMore, "more_after": more}, nil
}

type failedHistoryStore struct{ *db.DB }

func (failedHistoryStore) ConversationHistory(context.Context, string, string) (*db.ConversationHistory, error) {
	return nil, errors.New("fake database failure")
}

type gatedHistoryStore struct {
	*db.DB
	entered, release chan struct{}
}

func (st gatedHistoryStore) ConversationItems(ctx context.Context, machine, sid, history string, after, through, limit int, newest bool) ([]conversationlog.Item, error) {
	rows, err := st.DB.ConversationItems(ctx, machine, sid, history, after, through, limit, newest)
	close(st.entered)
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-st.release:
	}
	return rows, err
}

func historyFixture(t *testing.T, logs ...func(string, ...any)) (*Service, *fakeHub, *db.DB, *historyRuntime) {
	t.Helper()
	database := todoTestDB(t)
	logf := testLogf(t)
	if len(logs) > 0 {
		logf = logs[0]
	}
	f := newFakeHub().withMachine()
	s := withSettings(t, f, New(Options{Hub: f, Store: func() Store { return database }, Logf: logf}))
	setSettings(t, s, func(cfg *Settings) { cfg.Main = "main-chat" })
	for _, id := range []string{"main-chat", "lead-chat", "side-chat", "coding-agent"} {
		f.known[id] = true
	}
	f.setSessions("server",
		proto.Session{SessionID: "main-chat", Assistant: true, Rev: 100},
		proto.Session{SessionID: "lead-chat", Assistant: true, Rev: 100},
		proto.Session{SessionID: "side-chat", Assistant: true, Archived: true, Rev: 100},
		proto.Session{SessionID: "coding-agent", PaneID: "pane-1", Rev: 100})
	rt := &historyRuntime{history: "h1", items: []proto.Item{
		{ID: "u1", Seq: 1, Role: "user", Kind: proto.KindText, Text: "question one"},
		{ID: "a1", Seq: 2, Role: "assistant", Kind: proto.KindText, Text: "answer one"},
		{ID: "u2", Seq: 3, Role: "user", Kind: proto.KindText, Text: "same words"},
		{ID: "a2", Seq: 4, Role: "assistant", Kind: proto.KindText, Text: "answer two"},
		{ID: "u3", Seq: 5, Role: "user", Kind: proto.KindText, Text: "same words"},
		{ID: "a3", Seq: 6, Role: "assistant", Kind: proto.KindText, Text: "answer three"},
	}}
	f.answerNode(DeviceSessionItems, rt.page)
	f.answerNode("session.item", func(_ string, raw json.RawMessage) (any, error) {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.offline {
			return nil, errors.New("fake runtime is offline")
		}
		var p historyParams
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, err
		}
		for _, it := range rt.items {
			if it.ID == p.ItemID {
				it.FullSize = 0
				return it, nil
			}
		}
		return nil, errors.New("fake source item not found")
	})
	f.answerNode(DeviceAssistantHandoff, func(_ string, _ json.RawMessage) (any, error) {
		rt.mu.Lock()
		defer rt.mu.Unlock()
		if rt.offline {
			return nil, errors.New("fake runtime is offline")
		}
		return map[string]any{"summary": "source engine compaction"}, nil
	})
	return s, f, database, rt
}
