package node

import (
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/node/harness/agy"
)

// agy summarizes its own context (a CHECKPOINT step): the next message carries the whole snapshot once, not a delta
// that says unlisted rows are unchanged. Reading the same transcript again, or after a restart, does not reset again.
func TestAgyOwnCheckpointResetsWhatTheConversationHasSeenOnce(t *testing.T) {
	n := assistantNode(t)
	root := t.TempDir()
	const sid = "agy-" + agyTestID
	n.state.put(sid, sessionState{Launcher: "agy", Cwd: "/home/u/.persona/assistant"})
	reader := agy.New(agy.Dependencies{Clock: agyClock{}})
	ref := agy.Ref{Root: root, Conversation: agyTestID}
	read := func() *agy.Projection {
		t.Helper()
		h, err := reader.Read(ref)
		if err != nil {
			t.Fatal(err)
		}
		return h
	}
	send := func() string {
		t.Helper()
		block, commit := n.turnContext(sid, contextAnswer{Text: snapA})
		commit()
		return block
	}
	const before = `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","created_at":"2026-01-01T00:00:00Z","content":"hello"}
{"step_index":1,"type":"PLANNER_RESPONSE","source":"MODEL","status":"DONE","created_at":"2026-01-01T00:00:01Z","content":"hi"}
`
	agyTestFile(t, root, agyTestID, before)
	n.agyCheckpointed(sid, read())
	if !strings.Contains(send(), "Live snapshot (") || strings.Contains(send(), "Live snapshot (") {
		t.Fatal("the first message carries the whole snapshot and the second only a delta")
	}

	agyTestFile(t, root, agyTestID, before+`{"step_index":2,"type":"CHECKPOINT","status":"DONE","created_at":"2026-01-01T00:00:02Z","content":"summary of the talk"}`+"\n")
	n.agyCheckpointed(sid, read())
	if st, _ := n.state.get(sid); st.RecoveryPending == "" {
		t.Fatal("the checkpoint did not ask for the compaction recovery text")
	}
	if !strings.Contains(send(), "Live snapshot (") {
		t.Fatal("after agy's own compaction the next message must carry the whole snapshot")
	}
	n.agyCheckpointed(sid, read()) // the same transcript read again
	if strings.Contains(send(), "Live snapshot (") {
		t.Fatal("a second read of the same checkpoint reset again")
	}
	// A restart keeps the state record, so the checkpoint is still handled.
	restarted := assistantNode(t)
	st, _ := n.state.get(sid)
	restarted.state.put(sid, st)
	restarted.agyCheckpointed(sid, read())
	if again, _ := restarted.state.get(sid); again.RecoveryPending != st.RecoveryPending || again.AgyCheckpoint != 3 {
		t.Fatalf("a restarted node saw the checkpoint as new: %+v, before %+v", again, st)
	}
}

// A crash between persisting the checkpoint and the reset must not lose the reset: what reached the disk before the
// crash never holds the sequence without the reset.
func TestAgyCheckpointCrashBetweenWritesDoesNotLoseTheReset(t *testing.T) {
	n := assistantNode(t)
	root := t.TempDir()
	const sid = "agy-" + agyTestID
	n.state.put(sid, sessionState{Launcher: "agy", Cwd: "/home/u/.persona/assistant"})
	var disk sessionState
	writes := 0
	n.state.persist = func(_ string, v sessionState) {
		if writes++; writes == 2 {
			panic("crash")
		}
		disk = v
	}
	agyTestFile(t, root, agyTestID, `{"step_index":0,"type":"USER_INPUT","source":"USER_EXPLICIT","status":"DONE","created_at":"2026-01-01T00:00:00Z","content":"hello"}
{"step_index":1,"type":"CHECKPOINT","status":"DONE","created_at":"2026-01-01T00:00:02Z","content":"summary"}
`)
	h, err := agy.New(agy.Dependencies{Clock: agyClock{}}).Read(agy.Ref{Root: root, Conversation: agyTestID})
	if err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() { _ = recover() }()
		n.agyCheckpointed(sid, h)
	}()
	if disk.AgyCheckpoint != 0 && disk.RecoveryPending == "" && !disk.RecoveryHookPending {
		t.Fatalf("the checkpoint sequence was stored without its reset: %+v", disk)
	}
	if disk.RecoveryPending == "" {
		t.Fatalf("the reset was not stored first: %+v", disk)
	}
}
