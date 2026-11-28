package transcript

import (
	"errors"
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
