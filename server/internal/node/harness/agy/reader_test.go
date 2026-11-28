package agy

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

const agyTestID = "11111111-2222-4333-8444-555555555555"
const otherID = "22222222-3333-4444-8555-666666666666"
const stamp = "2026-01-01T00:00:00Z"

type testClock struct{}

func (testClock) Now() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }

func agyTestFile(t testing.TB, root, id, content string) string {
	t.Helper()
	return writeFixture(t, Ref{root, id}, TranscriptPath, content)
}

func writeFixture(t testing.TB, ref Ref, rel, content string) string {
	t.Helper()
	name := filepath.Join(ref.Root, "brain", ref.Conversation, rel)
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return name
}

func appendFixture(t testing.TB, file, content string) {
	t.Helper()
	f, err := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(content)
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("append: %v / %v", err, closeErr)
	}
}

func readFixture(t testing.TB, r *Reader, ref Ref) *Projection {
	t.Helper()
	h, err := r.Read(ref)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func parseAgySteps(raw []byte, conversation string) ([]agyStep, agyRevision) {
	f := agyFold{conversation: conversation}
	for line := range strings.SplitSeq(string(raw), "\n") {
		f.add([]byte(line))
	}
	return f.steps, f.revision(0, [32]byte{})
}

func mapAgySteps(steps []agyStep, fallback time.Time, _ any) *Projection {
	h := empty(Ref{Conversation: agyTestID})
	h.mapSteps(steps, fallback)
	return h
}

func agyParseLine(index int, typ, status, at, content string) string {
	return fmt.Sprintf(`{"step_index":%d,"type":%q,"status":%q,"created_at":%q,"content":%q}`+"\n", index, typ, status, at, content)
}

func TestReaderReusesProjectionAndParsesOnlyTheAppendedBytes(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	r := New(Dependencies{Clock: testClock{}})
	user := agyParseLine(0, "USER_INPUT", "DONE", stamp, "Hello")
	think := agyParseLine(1, "PLANNER_RESPONSE", "RUNNING", stamp, "Draft")
	done := agyParseLine(1, "PLANNER_RESPONSE", "DONE", stamp, "Final")
	late := agyParseLine(2, "GENERIC", "ERROR", stamp, "Result")
	tool := agyParseLine(3, "GENERIC", "DONE", stamp, "Command output")
	file := agyTestFile(t, ref.Root, ref.Conversation, user+think)
	first := readFixture(t, r, ref)
	if again := readFixture(t, r, ref); first != again {
		t.Fatal("unchanged metadata produced a new projection")
	}
	before := r.current(ref)
	before.fold.steps[0].Content = "Marked cached step" // a whole parse cannot see this marker
	appendFixture(t, file, done+tool[:20])
	second := readFixture(t, r, ref)
	if second == first || second.items[0].Text != "Marked cached step" || second.items[1].Text != "Final" || second.seq != 2 || second.token != first.token {
		t.Fatalf("append or partial record: %+v", second.items)
	}
	if first.items[0].Text != "Hello" || first.items[1].Text != "Draft" {
		t.Fatalf("append mutated the published projection: %+v", first.items)
	}
	if got := second.Updates(first, 2); len(got) != 1 || got[0].Text != "Final" {
		t.Fatalf("in-place completion updates: %+v", got)
	}
	appendFixture(t, file, tool[20:]+late)
	third := readFixture(t, r, ref)
	if third.seq != 4 || third.token == second.token || third.items[2].Text != "Result" {
		t.Fatalf("late step: %+v", third.items)
	}
	// A file that grew but whose old prefix changed must be parsed afresh.
	rewind := agyParseLine(1, "PLANNER_RESPONSE", "DONE", "2026-01-01T00:00:09Z", "Different branch")
	agyTestFile(t, ref.Root, ref.Conversation, strings.Replace(user, "Hello", "Howdy", 1)+think+done+tool+late+rewind)
	fourth := readFixture(t, r, ref)
	if fourth.items[0].Text != "Howdy" || fourth.seq != 2 || fourth.token == third.token {
		t.Fatalf("rewritten grown file: %+v", fourth.items)
	}
	fresh := readFixture(t, New(Dependencies{Clock: testClock{}}), ref)
	if !reflect.DeepEqual(fresh.items, fourth.items) || fresh.token != fourth.token {
		t.Fatal("incremental and whole folds disagree")
	}
	// A same-inode, same-size rewrite is detected when the mtime changes.
	oldAt := fourth.ModTime()
	agyTestFile(t, ref.Root, ref.Conversation, strings.Replace(user, "Hello", "Howdy", 1)+think+done+tool+late+strings.Replace(rewind, "branch", "answer", 1))
	if err := os.Chtimes(file, oldAt.Add(time.Second), oldAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if updated := readFixture(t, r, ref); updated.token == fourth.token || updated.items[1].Text != "Different answer" {
		t.Fatalf("same-size rewrite: %+v", updated.items)
	}
}

func TestReaderRejectsAStaleGenerationCommit(t *testing.T) {
	t.Parallel()
	r := New(Dependencies{Clock: testClock{}})
	ref := Ref{t.TempDir(), agyTestID}
	agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "First"))
	readFixture(t, r, ref)
	before := r.current(ref)
	attempt := make(chan bool, 1)
	commit := make(chan struct{})
	go func() {
		stale := &generation{ref: ref, projection: empty(ref)}
		<-commit
		attempt <- r.commit(ref, before, stale)
	}()
	agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Replacement"))
	wanted := readFixture(t, r, ref)
	close(commit)
	if <-attempt || r.current(ref).projection != wanted || wanted.items[0].Text != "Replacement" {
		t.Fatal("stale generation displaced a committed read")
	}
}

func TestReaderNeverFallsBackToTheCompactExport(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	writeFixture(t, ref, ".system_generated/logs/transcript.jsonl", agyParseLine(0, "USER_INPUT", "DONE", stamp, "Not the source"))
	r := New(Dependencies{Clock: testClock{}})
	if h := readFixture(t, r, ref); !ref.Valid() || h.seq != 0 || len(h.items) != 0 || h.kind != Missing {
		t.Fatalf("compact-only projection: %+v", h)
	}
	file := agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Full export"))
	if h := readFixture(t, r, ref); len(h.items) != 1 || h.items[0].Text != "Full export" {
		t.Fatalf("full export: %+v", h)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if h := readFixture(t, r, ref); h.seq != 0 || len(h.items) != 0 {
		t.Fatalf("removed export: %+v", h)
	}
}

func BenchmarkReadTranscript64MiB(b *testing.B) {
	ref := Ref{b.TempDir(), agyTestID}
	path := agyTestFile(b, ref.Root, ref.Conversation, "")
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	w := bufio.NewWriter(f)
	content := strings.Repeat("synthetic output ", 1024)
	var size int64
	for i := 0; size < 64<<20; i++ {
		n, err := w.WriteString(agyParseLine(i, "GENERIC", "DONE", stamp, content))
		if err != nil {
			b.Fatal(err)
		}
		size += int64(n)
	}
	if err := w.Flush(); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
	b.Run("cold", func(b *testing.B) {
		b.SetBytes(size)
		b.ReportAllocs()
		for b.Loop() {
			readFixture(b, New(Dependencies{Clock: testClock{}}), ref)
		}
	})
	b.Run("cached", func(b *testing.B) {
		r := New(Dependencies{Clock: testClock{}})
		readFixture(b, r, ref)
		b.ReportAllocs()
		for b.Loop() {
			h := readFixture(b, r, ref)
			if h.items[0].Kind != proto.KindToolResult {
				b.Fatal("missing projection")
			}
		}
	})
}
