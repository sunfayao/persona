package node

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A rollout listed before its first line is written (Codex 0.167 creates it empty and writes session_meta some
// milliseconds later) gets its directory once the line is there: what an empty or half-written line said is not kept.
func TestRolloutsMetaAfterTheFileIsWritten(t *testing.T) {
	codex := filepath.Join(t.TempDir(), ".codex")
	const id = "d1111111-1111-1111-1111-111111111111"
	now := time.Now()
	dir := filepath.Join(codex, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "rollout-"+now.Format("2006-01-02T15-04-05")+"-"+id+".jsonl")
	whole := []byte(`{"timestamp":"2026-01-01T00:00:00Z","type":"session_meta","payload":{"id":"` + id + `","cwd":"/w"}}` + "\n")
	for _, part := range [][]byte{nil, whole[:len(whole)/2]} {
		if err := os.WriteFile(p, part, 0o644); err != nil {
			t.Fatal(err)
		}
		expireRollouts(codex, true)
		if got := rollouts(codex); len(got) != 1 || got[0].Cwd != "" {
			t.Fatalf("%d bytes written: %+v", len(part), got)
		}
		if m := metaOf(codex, p); m.Cwd != "" {
			t.Fatalf("%d bytes written: metaOf %+v", len(part), m)
		}
	}
	if err := os.WriteFile(p, whole, 0o644); err != nil {
		t.Fatal(err)
	}
	expireRollouts(codex, true)
	if got := rollouts(codex); len(got) != 1 || got[0].Cwd != "/w" {
		t.Fatalf("written: %+v", got)
	}
	if m := metaOf(codex, p); m.Cwd != "/w" {
		t.Fatalf("written: metaOf %+v", m)
	}
}

func rolloutMetaCount(home string) int {
	rolloutCaches.Lock()
	defer rolloutCaches.Unlock()
	return len(cacheFor(home).metas)
}

// The session_meta cache keeps only rollouts still on disk: once the older days are listed again, deleted threads
// (recent, or older ones resumed lately) leave it, while a rollout outside the window read on demand stays.
func TestRolloutsMetasPruned(t *testing.T) {
	home := t.TempDir()
	codex := filepath.Join(home, ".codex")
	now := time.Now()
	older := now.AddDate(0, 0, -40)
	const (
		recentKept    = "a1111111-1111-1111-1111-111111111111"
		recentGone    = "a2222222-2222-2222-2222-222222222222"
		resumedKept   = "b1111111-1111-1111-1111-111111111111"
		resumedGone   = "b2222222-2222-2222-2222-222222222222"
		outsideWindow = "c1111111-1111-1111-1111-111111111111"
	)
	writeRollout(t, home, recentKept, "/w", now, now)
	writeRollout(t, home, recentGone, "/w", now, now)
	writeRollout(t, home, resumedKept, "/w", older, now)
	writeRollout(t, home, resumedGone, "/w", older, now)
	writeRollout(t, home, outsideWindow, "/w", older.AddDate(0, 0, -1), older)

	if got := len(rollouts(codex)); got != 4 {
		t.Fatalf("rollouts in the window: %d, want 4", got)
	}
	p := rolloutPath(codex, outsideWindow)
	if p == "" || metaOf(codex, p).Cwd != "/w" {
		t.Fatalf("rollout outside the window: %q", p)
	}
	if got := rolloutMetaCount(codex); got != 5 {
		t.Fatalf("metas before: %d, want 5", got)
	}

	for _, id := range []string{recentGone, resumedGone} {
		if err := os.Remove(rolloutPath(codex, id)); err != nil {
			t.Fatal(err)
		}
	}
	expireRollouts(codex, true)
	got := rollouts(codex)
	if len(got) != 2 || got[0].id == got[1].id {
		t.Fatalf("rollouts after deleting two: %+v", got)
	}
	for _, r := range got {
		if r.id != recentKept && r.id != resumedKept {
			t.Fatalf("unexpected rollout %q", r.id)
		}
	}
	// The two kept rollouts and the one outside the window, still on disk.
	if n := rolloutMetaCount(codex); n != 3 {
		t.Fatalf("metas after the older days are listed again: %d, want 3", n)
	}
}
