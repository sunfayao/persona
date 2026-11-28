package transcript

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexedHistoryRetriesWhenStorageRevisionChanges(t *testing.T) {
	t.Parallel()
	revision := "first"
	calls := 0
	bad := errors.New("unreadable indexed history")
	f := NewIndexedFile("fixture:retry", IndexedSource{
		Revision: func() string { return revision },
		Read: func() ([]CodexRecord, error) {
			calls++
			if revision == "first" {
				return nil, bad
			}
			return []CodexRecord{{Type: "session_meta", Payload: []byte(`{"cwd":"/work/example"}`)}}, nil
		},
	}, Sources{})
	for range 2 {
		if err := f.PrefetchIndexed(); !errors.Is(err, bad) {
			t.Fatalf("initial error = %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("queried the unchanged failed revision %d times", calls)
	}
	revision = "repaired"
	if err := f.PrefetchIndexed(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Refresh(); err != nil || calls != 2 || f.Meta().Cwd != "/work/example" {
		t.Fatalf("repair: reads=%d, metadata=%+v, err=%v", calls, f.Meta(), err)
	}
}

func TestIndexedMigrationKeepsRolloutUntilSourceIsReadable(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	raw := `{"type":"event_msg","payload":{"type":"user_message","message":"Readable transcript"}}` + "\n"
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	f := NewFile(path)
	_, before, err := f.RefreshSpan()
	if err != nil || before.To == 0 {
		t.Fatalf("rollout: %+v, %v", before, err)
	}
	revision := "locked"
	unavailable := errors.New("database temporarily unavailable")
	source := IndexedSource{
		Revision: func() string { return revision },
		Read: func() ([]CodexRecord, error) {
			if revision == "locked" {
				return nil, unavailable
			}
			return []CodexRecord{{Type: "session_meta", Payload: []byte(`{"cwd":"/work/example"}`)}}, nil
		},
	}
	if err := f.BindIndexed("fixture:migration", source); !errors.Is(err, unavailable) {
		t.Fatalf("expected database error, got %v", err)
	}
	if f.Indexed() || f.Where() != path || f.End() != before {
		t.Fatalf("failed migration changed usable history: %s, %+v", f.Where(), f.End())
	}
	revision = "readable"
	if err := f.BindIndexed("fixture:migration", source); err != nil {
		t.Fatal(err)
	}
	if _, span, err := f.RefreshSpan(); err != nil || !strings.HasPrefix(span.History, "indexed-") || span.History == before.History {
		t.Fatalf("migrated history: %+v, %v", span, err)
	}
}

func TestIndexedHistoryColdCursorDetectsReplacedRecords(t *testing.T) {
	t.Parallel()
	revision := "one"
	header := CodexRecord{Type: "session_meta", Payload: []byte(`{"id":"example"}`)}
	record := CodexRecord{Type: "event_msg", Payload: []byte(`{"type":"user_message","message":"First turn"}`)}
	records := []CodexRecord{header, record}
	source := IndexedSource{
		Revision: func() string { return revision },
		Read:     func() ([]CodexRecord, error) { return records, nil },
	}
	open := func() (*File, Span) {
		t.Helper()
		f := NewIndexedFile("fixture:cursor", source, Sources{})
		_, span, err := f.RefreshSpan()
		if err != nil {
			t.Fatal(err)
		}
		return f, span
	}
	warm, first := open()
	if _, next := open(); next.History != first.History {
		t.Fatalf("same snapshot changed its cold cursor: %q, %q", first.History, next.History)
	}
	revision = "append"
	records = append(records, record)
	if _, span, err := warm.RefreshSpan(); err != nil || span.History != first.History || span.From != first.To {
		t.Fatalf("append reset the warm cursor: %+v, %v", span, err)
	}
	revision = "replacement"
	records = []CodexRecord{header, {Type: "event_msg", Payload: []byte(`{"type":"user_message","message":"Replacement"}`)}}
	_, cold := open()
	if _, span, err := warm.RefreshSpan(); err != nil || cold.History == first.History || span.History != cold.History || span.From != 0 {
		t.Fatalf("cold and warm rewind cursors: old %q, cold %+v, warm %+v, %v", first.History, cold, span, err)
	}
}
