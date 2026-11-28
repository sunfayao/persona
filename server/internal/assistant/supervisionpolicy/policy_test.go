package supervisionpolicy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

type memStore map[string]json.RawMessage

func (m memStore) Setting(_ context.Context, key string, out any) (bool, error) {
	raw, ok := m[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (m memStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	m[key] = raw
	return err
}

func book(st memStore) *Book { return New(Dependencies{Store: func() Store { return st }}) }

func TestAConversationKeepsWhatItStartedWith(t *testing.T) {
	ctx := context.Background()
	st := memStore{}
	b := book(st)
	asked := 0
	supervises := func(own bool) func() (bool, error) {
		return func() (bool, error) { asked++; return own, nil }
	}
	if full, _ := b.Standing(ctx, "m", "live", supervises(false)); !full || asked != 0 {
		t.Fatalf("a conversation with no record has the policy and is not asked about: %v, %d", full, asked)
	}
	b.Started(ctx, "m", "new", true)
	if full, _ := b.Standing(ctx, "m", "new", supervises(true)); full || asked != 0 {
		t.Fatalf("a conversation that started without the policy gained it: %v, %d", full, asked)
	}
	if _, _, owed := b.Notice(ctx, "m", "live", "policy"); owed {
		t.Fatal("a conversation with the policy in its standing text is owed it")
	}

	// A new manager reads the same decisions.
	b = book(st)
	if full, _ := b.Standing(ctx, "m", "new", supervises(true)); full {
		t.Fatal("a restart changed a decision")
	}
	if text, receipt, owed := b.Notice(ctx, "m", "new", "policy"); !owed || text != "policy" || receipt.ItemID != ReceiptID {
		t.Fatalf("notice %q %v %v", text, receipt, owed)
	}
	b.Delivered(ctx, "m", "new", []assistantwire.ViewErrorReceipt{{ItemID: ReceiptID, Digest: "1"}})
	if _, _, owed := b.Notice(ctx, "m", "new", "policy"); owed {
		t.Fatal("told twice")
	}

	// Compacted: owed again at once, and decided again at its next start.
	b.Compacted(ctx, "m", "new")
	if _, _, owed := b.Notice(ctx, "m", "new", "policy"); !owed {
		t.Fatal("not owed again after a compaction")
	}
	if full, _ := b.Standing(ctx, "m", "new", supervises(true)); !full || asked != 1 {
		t.Fatalf("decided again after a compaction: %v, %d", full, asked)
	}
	if _, _, owed := b.Notice(ctx, "m", "new", "policy"); owed {
		t.Fatal("a conversation that has the policy again is owed it")
	}
	b.Compacted(ctx, "m", "live") // one that was live before the book: decided at its next start too
	if full, _ := b.Standing(ctx, "m", "live", supervises(false)); full || asked != 2 {
		t.Fatalf("a live conversation compacted: %v, %d", full, asked)
	}
}
