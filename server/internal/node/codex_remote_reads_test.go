package node

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// readCounts is how many thread/read calls each thread has had, counted in one pass.
func (f *remoteFixture) readCounts() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	counts := map[string]int{}
	for _, m := range f.got {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		if m.Method == "thread/read" && json.Unmarshal(m.Params, &p) == nil {
			counts[p.ThreadID]++
		}
	}
	return counts
}

// readsSince is the threads read after before was counted, sorted.
func readsSince(before, after map[string]int) []string {
	var read []string
	for raw, n := range after {
		if n > before[raw] {
			read = append(read, raw)
		}
	}
	slices.Sort(read)
	return read
}

// ephemeralReads answers thread/read for helpers as an app-server-compatible daemon answers for a finished sub-Agent
// helper it keeps loaded: ephemeral, with no rollout.
func ephemeralReads(home string, helpers map[string]bool) func(string, map[string]any) (any, bool) {
	return func(method string, p map[string]any) (any, bool) {
		thread, _ := p["threadId"].(string)
		if method != "thread/read" || !helpers[thread] {
			return nil, false
		}
		return map[string]any{"thread": map[string]any{"id": thread, "ephemeral": true, "path": nil, "cwd": home,
			"source": map[string]any{"subAgent": map[string]any{"thread_spawn": map[string]any{}}},
			"status": map[string]any{"type": "idle"}}}, true
	}
}

// An ephemeral thread is read once: a thread is ephemeral or not from its creation, so later refreshes read only the
// other threads, however many ephemeral ones the app-server keeps loaded (#12517). One that leaves the loaded list is
// forgotten, and read once more if it comes back.
func TestCodexRemoteRemembersEphemeralThreads(t *testing.T) {
	n := remoteTestNode(t)
	home := t.TempDir()
	f, c := connectRemoteFixture(t, n, home, "one", remoteRaw)
	normal := []string{remoteRaw}
	for i := range 19 {
		normal = append(normal, fmt.Sprintf("cccccccc-0000-0000-0000-%012d", i))
	}
	helpers := map[string]bool{}
	var loaded []string
	for i := range 600 {
		raw := fmt.Sprintf("dddddddd-0000-0000-0000-%012d", i)
		helpers[raw] = true
		loaded = append(loaded, raw)
		if i%30 == 29 {
			loaded = append(loaded, normal[i/30])
		}
	}
	f.mu.Lock()
	f.threads, f.response = loaded, ephemeralReads(home, helpers)
	f.mu.Unlock()
	want := slices.Sorted(slices.Values(normal))

	before := f.readCounts()
	if err := c.refresh(); err != nil {
		t.Fatal(err)
	}
	if read := readsSince(before, f.readCounts()); len(read) != len(loaded) {
		t.Fatalf("the first refresh read %d threads, want all %d", len(read), len(loaded))
	}
	for i := range 2 {
		before = f.readCounts()
		if err := c.refresh(); err != nil {
			t.Fatal(err)
		}
		if read := readsSince(before, f.readCounts()); !slices.Equal(read, want) {
			t.Fatalf("refresh %d after the first read %d threads, want only the %d that are not ephemeral", i+1, len(read), len(want))
		}
	}
	var listed []string
	for _, s := range n.codexSessions(map[string]bool{}) {
		listed = append(listed, s.SourceThreadID)
	}
	slices.Sort(listed)
	if !slices.Equal(listed, want) {
		t.Fatalf("sessions = %v, want the %d threads that are not ephemeral", listed, len(want))
	}

	// One leaves the loaded list: forgotten. Back in it: read once more, then remembered again.
	gone := loaded[0]
	f.mu.Lock()
	f.threads = loaded[1:]
	f.mu.Unlock()
	if err := c.refresh(); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	_, kept := c.seen[gone]
	remembered := len(c.seen)
	c.mu.Unlock()
	if kept || remembered != len(loaded)-1 {
		t.Fatalf("after a thread left the loaded list: still remembered %v, %d threads remembered, want %d", kept, remembered, len(loaded)-1)
	}
	f.mu.Lock()
	f.threads = loaded
	f.mu.Unlock()
	before = f.readCounts()
	for range 2 {
		if err := c.refresh(); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.readCounts()[gone] - before[gone]; got != 1 {
		t.Fatalf("an ephemeral thread back in the loaded list was read %d times in two refreshes, want once", got)
	}
}

// The read order: threads with a turn running or a request waiting first, then by when each was last read, never read
// first; a read cut short does not count as read, but goes behind threads not asked; the loaded list's order breaks
// ties.
func TestCodexReadOrder(t *testing.T) {
	at := time.Now()
	c := &codexRemote{
		threads: map[string]remoteThread{
			"working":  {status: proto.StatusWorking},
			"blocked":  {status: proto.StatusBlocked},
			"done-old": {status: proto.StatusDone},
			"done-new": {status: proto.StatusDone},
		},
		seen: map[string]codexSeen{
			"working":  {read: at},
			"blocked":  {read: at.Add(-time.Minute)},
			"done-old": {read: at.Add(-time.Hour)},
			"done-new": {read: at},
			"cut-once": {cut: at.Add(-time.Second)}, // asked once, cut short by the refresh's time: not listed yet
		},
	}
	reads := []string{"done-new", "never-a", "working", "done-old", "cut-once", "never-b", "blocked"}
	c.readOrder(reads)
	want := []string{"blocked", "working", "never-a", "never-b", "cut-once", "done-old", "done-new"}
	if !slices.Equal(reads, want) {
		t.Fatalf("order = %v, want %v", reads, want)
	}
}
