package rulefiles

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistantwire"
)

type memStore struct {
	mu   sync.Mutex
	data map[string][]byte
}

func (m *memStore) Setting(_ context.Context, key string, out any) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, ok := m.data[key]
	if !ok {
		return false, nil
	}
	return true, json.Unmarshal(raw, out)
}

func (m *memStore) PutSetting(_ context.Context, key string, v any) error {
	raw, err := json.Marshal(v)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data == nil {
		m.data = map[string][]byte{}
	}
	m.data[key] = raw
	return err
}

// ended is the set of conversations the fake node no longer has.
type ended struct {
	mu  sync.Mutex
	ids map[string]bool
}

func (e *ended) alive(_ context.Context, _, id string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.ids[id]
}

func (e *ended) end(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ids == nil {
		e.ids = map[string]bool{}
	}
	e.ids[id] = true
}

func newBook(st *memStore, gone *ended) *Book {
	return New(Dependencies{Store: func() Store { return st }, Alive: gone.alive})
}

const machine, dir = "m1", "assistant"

var (
	before = Values{Notes: "I work on backend.", Language: "English", Fixed: true}
	after  = Values{Notes: "I work on the mobile app.", Language: "English", Fixed: true}
)

// write is a prepareSpaceIf: what the files say is what Render gave, noted by Wrote.
func write(b *Book, cur Values) Values {
	ctx := context.Background()
	v := b.Render(ctx, machine, dir, cur)
	b.Wrote(ctx, machine, dir)
	return v
}

func TestNoConversationFollowsTheSettings(t *testing.T) {
	t.Parallel()
	b := newBook(&memStore{}, &ended{})
	if got := write(b, before); got != before {
		t.Fatalf("first write %+v", got)
	}
	if got := write(b, after); got != after {
		t.Fatalf("with no conversation the files follow the settings: %+v", got)
	}
}

func TestLiveConversationHoldsTheFilesAndGetsTheBlockOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBook(&memStore{}, &ended{})
	write(b, before)
	if _, _, owed := b.Notice(ctx, machine, dir, "c1", before, false); owed {
		t.Fatal("nothing changed yet, nothing owed")
	}
	if got := write(b, after); got != before {
		t.Fatalf("a change under a live conversation rewrote the files: %+v", got)
	}
	text, receipt, owed := b.Notice(ctx, machine, dir, "c1", after, false)
	if !owed || receipt.ItemID != ReceiptID {
		t.Fatalf("owed %v receipt %+v", owed, receipt)
	}
	for _, want := range []string{`<persona-context kind="about-me">`, "I work on the mobile app.", "replaces the About me section", "</persona-context>"} {
		if !strings.Contains(text, want) {
			t.Errorf("block lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Answer in") {
		t.Errorf("the language did not change but the block names it:\n%s", text)
	}
	// Until the node confirms the message, the next one carries it again.
	if _, _, owed := b.Notice(ctx, machine, dir, "c1", after, false); !owed {
		t.Fatal("an undelivered block must be offered again")
	}
	if rest := b.Delivered(ctx, machine, "c1", []assistantwire.ViewErrorReceipt{receipt, {ItemID: "other", Digest: "x"}}); len(rest) != 1 || rest[0].ItemID != "other" {
		t.Fatalf("the other receipts must stay: %v", rest)
	}
	if _, _, owed := b.Notice(ctx, machine, dir, "c1", after, false); owed {
		t.Fatal("a delivered block is not repeated")
	}
	// A later change is owed again, as the new text.
	third := Values{Notes: "I work on infrastructure.", Language: "English", Fixed: true}
	if text, _, owed := b.Notice(ctx, machine, dir, "c1", third, false); !owed || !strings.Contains(text, "infrastructure") {
		t.Fatalf("a second change: %v %q", owed, text)
	}
}

func TestReplyLanguageIsTreatedTheSame(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBook(&memStore{}, &ended{})
	write(b, before)
	b.Notice(ctx, machine, dir, "c1", before, false)
	japanese := Values{Notes: before.Notes, Language: "Japanese", Fixed: true}
	if got := write(b, japanese); got != before {
		t.Fatalf("language change rewrote the files: %+v", got)
	}
	text, _, owed := b.Notice(ctx, machine, dir, "c1", japanese, false)
	if !owed || !strings.Contains(text, "Answer in Japanese.") || strings.Contains(text, "About the user") {
		t.Fatalf("owed %v:\n%s", owed, text)
	}
}

func TestCompactionAndStartCatchTheFilesUp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBook(&memStore{}, &ended{})
	write(b, before)
	b.Notice(ctx, machine, dir, "c1", before, false)
	_, receipt, _ := b.Notice(ctx, machine, dir, "c1", after, false)
	b.Delivered(ctx, machine, "c1", []assistantwire.ViewErrorReceipt{receipt})
	// Compacted: the message that tells it carries nothing, and the next write takes the settings.
	if _, _, owed := b.Notice(ctx, machine, dir, "c1", after, true); owed {
		t.Fatal("the block was delivered before the compaction")
	}
	if got := write(b, after); got != after {
		t.Fatalf("after the compaction the files carry the new text: %+v", got)
	}
	if _, _, owed := b.Notice(ctx, machine, dir, "c1", after, false); owed {
		t.Fatal("the block is not sent again after the files caught up")
	}
	// A change after that is held again.
	later := Values{Notes: "later", Language: "English", Fixed: true}
	if got := write(b, later); got != after {
		t.Fatalf("held again: %+v", got)
	}
}

func TestAnotherConversationJoinsTheHoldInsteadOfEndingIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gone := &ended{}
	b := newBook(&memStore{}, gone)
	write(b, before)
	b.Notice(ctx, machine, dir, "main", before, false)
	if got := write(b, after); got != before {
		t.Fatalf("held: %+v", got)
	}
	// A scheduled run starts: it is written for like a refresh, gets the held files, and is owed the block once.
	if got := write(b, after); got != before {
		t.Fatalf("a start under a live conversation must not rewrite: %+v", got)
	}
	text, receipt, owed := b.Notice(ctx, machine, dir, "run", after, false)
	if !owed || !strings.Contains(text, "mobile app") {
		t.Fatalf("the run is owed the block: %v", owed)
	}
	b.Delivered(ctx, machine, "run", []assistantwire.ViewErrorReceipt{receipt})
	if _, _, owed := b.Notice(ctx, machine, dir, "run", after, false); owed {
		t.Fatal("once")
	}
	// The main conversation is compacted while the run lives: the run still holds the old files.
	b.Notice(ctx, machine, dir, "main", after, true)
	if got := write(b, after); got != before {
		t.Fatalf("the run still holds the old files: %+v", got)
	}
	// The run ends: everything left was compacted, so the files take the settings.
	gone.end("run")
	if got := write(b, after); got != after {
		t.Fatalf("all compacted or ended: %+v", got)
	}
}

func TestCompactionWithTheOtherAlreadyEndedRewrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gone := &ended{}
	b := newBook(&memStore{}, gone)
	write(b, before)
	b.Notice(ctx, machine, dir, "main", before, false)
	b.Notice(ctx, machine, dir, "run", before, false)
	gone.end("run")
	write(b, after) // prunes the run
	b.Notice(ctx, machine, dir, "main", after, true)
	if got := write(b, after); got != after {
		t.Fatalf("%+v", got)
	}
}

func TestStartWithNoLiveConversationWritesTheNewFilesAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gone := &ended{}
	b := newBook(&memStore{}, gone)
	write(b, before)
	b.Notice(ctx, machine, dir, "main", before, false)
	gone.end("main")
	if got := write(b, after); got != after {
		t.Fatalf("%+v", got)
	}
}

func TestCompactionOwesTheUndeliveredBlockOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	b := newBook(&memStore{}, &ended{})
	write(b, before)
	b.Notice(ctx, machine, dir, "c1", before, false)
	if _, _, owed := b.Notice(ctx, machine, dir, "c1", after, true); !owed {
		t.Fatal("the message that reports the compaction still carries a block the conversation never got")
	}
	if got := write(b, after); got != after {
		t.Fatalf("%+v", got)
	}
}

func TestRestartKeepsTheHoldAndWhatWasTold(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	st, gone := &memStore{}, &ended{}
	b := newBook(st, gone)
	write(b, before)
	b.Notice(ctx, machine, dir, "c1", before, false)
	_, receipt, _ := b.Notice(ctx, machine, dir, "c1", after, false)
	b.Delivered(ctx, machine, "c1", []assistantwire.ViewErrorReceipt{receipt})

	again := newBook(st, gone) // the manager restarted: its memory is empty
	if got := write(again, after); got != before {
		t.Fatalf("a restart rewrote the files under a live conversation: %+v", got)
	}
	if _, _, owed := again.Notice(ctx, machine, dir, "c1", after, false); owed {
		t.Fatal("a restart tells the conversation again")
	}
}

func TestEndedConversationNoLongerHoldsTheFiles(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	gone := &ended{}
	b := newBook(&memStore{}, gone)
	write(b, before)
	b.Notice(ctx, machine, dir, "c1", before, false)
	b.Notice(ctx, machine, dir, "c2", before, false)
	gone.end("c1")
	if got := write(b, after); got != before {
		t.Fatalf("c2 still lives: %+v", got)
	}
	gone.end("c2")
	if got := write(b, after); got != after {
		t.Fatalf("no conversation lives: %+v", got)
	}
}

func TestGivenIsPerDirectoryAndInMemory(t *testing.T) {
	t.Parallel()
	b := newBook(&memStore{}, &ended{})
	g := Given{MD: "a", Skills: "s"}
	if b.Given("k") == g {
		t.Fatal("nothing was given yet")
	}
	b.Remember("k", g)
	if b.Given("k") != g || b.Given("other") == g {
		t.Fatal("remembered per key")
	}
	b.Forget("k")
	b.Remember("j", g)
	b.ForgetAll()
	if b.Given("k") == g || b.Given("j") == g {
		t.Fatal("forgotten")
	}
}

func TestSkillDigests(t *testing.T) {
	t.Parallel()
	d, files := SkillDigests([]byte(`[{"name":"n","plugin":"p","digest":"d1"},{"name":"m","plugin":"p","digest":"d2","files":{"SKILL.md":"x"}}]`))
	if d != "p/n d1\np/m d2\n" || !files {
		t.Fatalf("%q %v", d, files)
	}
	if d, files := SkillDigests([]byte("null")); d != "" || files {
		t.Fatalf("%q %v", d, files)
	}
}
