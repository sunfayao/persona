package node

import (
	"bytes"
	"log"
	"strings"
	"testing"
	"time"
)

const snapA = `**This is the main conversation**: the example user usually finds you here; carry on from what you talked about before.

- Now: 2026-11-01 Sun 15:04 (Asia/Shanghai)
- Computers online: Server (server)
- Agents: 1 need you, 0 finished unread, 0 working (details: agents_list)
  - Need you: Fix login (server, session_id=aaaaaaaa)
- Feishu calendar (meetings today that have not ended yet; ones already finished are not listed): 16:00–17:00 Review
- Scheduled follow-ups: none`

// The first message carries the whole snapshot; the next ones only what changed, and the time.
func TestContextDelta(t *testing.T) {
	first, seen := contextDelta(nil, snapA)
	if !strings.HasPrefix(first, "<persona-context>\nLive snapshot (") || !strings.HasSuffix(first, "</persona-context>") {
		t.Fatalf("first block:\n%s", first)
	}
	for _, want := range []string{"**This is the main conversation**", "- Computers online", "  - Need you: Fix login", "- Feishu calendar (meetings today", "- Scheduled follow-ups: none"} {
		if !strings.Contains(first, want) {
			t.Errorf("first block lacks %q:\n%s", want, first)
		}
	}

	same := strings.Replace(snapA, "15:04", "15:30", 1)
	block, seen := contextDelta(seen, same)
	if want := "<persona-context>\n" + deltaIntro + "\n- Now: 2026-11-01 Sun 15:30 (Asia/Shanghai)\n</persona-context>"; block != want {
		t.Fatalf("nothing changed but the time:\n%s", block)
	}

	// The waiting agent answered (its indented line goes with the Agents section), the meeting is over (the section
	// is named the same either way).
	later := strings.NewReplacer(
		"1 need you", "0 need you", "\n  - Need you: Fix login (server, session_id=aaaaaaaa)", "",
		"- Feishu calendar (meetings today that have not ended yet; ones already finished are not listed): 16:00–17:00 Review", "- Feishu calendar (no more meetings today; ones already finished are not listed)",
	).Replace(same)
	block, _ = contextDelta(seen, later)
	for _, want := range []string{"- Now: ", "- Agents: 0 need you", "- Feishu calendar (no more meetings today"} {
		if !strings.Contains(block, want) {
			t.Errorf("lacks %q:\n%s", want, block)
		}
	}
	for _, not := range []string{"This is the main conversation", "Computers online", "Fix login", "follow-ups"} {
		if strings.Contains(block, not) {
			t.Errorf("repeats %q, unchanged:\n%s", not, block)
		}
	}
	if b, s := contextDelta(seen, ""); b != "" || s == nil {
		t.Fatal("no snapshot (the hub didn't answer): no block, and what was seen stays")
	}
}

func TestContextDeltaReplacesDoneAndMutedDetails(t *testing.T) {
	const doneRow = "  - Done (last 24 hours): "
	const mutedRow = "  - Muted (the user turned off their notifications; the counts above exclude them; do not bring them up unprompted, answer only when asked): "
	before := strings.Replace(snapA, "  - Need you: Fix login (server, session_id=aaaaaaaa)", `  - Need you: Fix login (server, session_id=aaaaaaaa)
`+doneRow+`Old done title (server, session_id=done-1): old verdict
`+mutedRow+`Old muted title (server, session_id=mute-1)`, 1)
	_, seen := contextDelta(nil, before)
	counts := strings.NewReplacer(
		"Old done title (server, session_id=done-1): old verdict", "1 in total, query with agents_list state=done",
		"Old muted title (server, session_id=mute-1)", "1 in total, query with agents_list state=muted",
	).Replace(before)
	block, seen := contextDelta(seen, counts)
	for _, want := range []string{"- Agents (only changed", "agents_list state=done", "agents_list state=muted"} {
		if !strings.Contains(block, want) {
			t.Errorf("the count-only Agents rows lack %q:\n%s", want, block)
		}
	}
	for _, unwanted := range []string{"Old done title", "Old muted title", "old verdict", "done-1", "mute-1"} {
		if strings.Contains(block, unwanted) {
			t.Errorf("stale detail %q survived the rows' replacement:\n%s", unwanted, block)
		}
	}
	empty := strings.NewReplacer(
		"\n"+doneRow+"1 in total, query with agents_list state=done", "",
		"\n"+mutedRow+"1 in total, query with agents_list state=muted", "",
	).Replace(counts)
	block, _ = contextDelta(seen, empty)
	if !strings.Contains(block, "(removed rows: Done; Muted)") || strings.Contains(block, "agents_list state=") {
		t.Fatalf("the done and muted rows that are gone were not told removed:\n%s", block)
	}
}

type stdinBuf struct{ bytes.Buffer }

func (*stdinBuf) Close() error { return nil }

// A long conversation about to be let go is compacted first; a compaction makes the next message carry the whole
// snapshot again.
func TestAssistantIdleCompaction(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	in := &stdinBuf{}
	p := &assistantProc{id: id, agent: "claude", stdin: in}
	n.procs[id] = p
	stream := func(lines ...string) { n.readAssistant(p, strings.NewReader(strings.Join(lines, "\n")+"\n")) }
	_, commit := n.turnContext(id, contextAnswer{Text: snapA})
	commit()

	stream(`{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":2,"cache_read_input_tokens":180000,"cache_creation_input_tokens":900}}},"parent_tool_use_id":null}`,
		`{"type":"stream_event","event":{"type":"message_start","message":{"usage":{"input_tokens":2,"cache_read_input_tokens":9000}}},"parent_tool_use_id":"toolu_1"}`,
		`{"type":"result","subtype":"success"}`)
	later := time.Now().Add(assistantIdleTimeout + time.Minute)
	p.mu.Lock()
	tokens, reap, compact := p.context, p.reapable(later), p.idleCompactable()
	p.mu.Unlock()
	if tokens != 180902 {
		t.Fatalf("context %d: the main thread's last request, not a subagent's", tokens)
	}
	if !reap || !compact {
		t.Fatalf("idle and long: reapable %v, compact first %v", reap, compact)
	}

	n.compactIdle(p)
	if got := strings.TrimSpace(in.String()); got != `{"message":{"content":"/compact","role":"user"},"type":"user"}` {
		t.Fatalf("sent %s", got)
	}
	p.mu.Lock()
	reap, stuck := p.reapable(time.Now().Add(5*time.Minute)), p.reapable(time.Now().Add(11*time.Minute))
	p.mu.Unlock()
	if reap || !stuck {
		t.Fatalf("while compacting: let go %v; one that never reports back let go after 10 minutes %v", reap, stuck)
	}
	stream(`{"type":"system","subtype":"compact_boundary","compact_metadata":{"trigger":"manual","pre_tokens":180902,"post_tokens":9500}}`,
		`{"type":"result","subtype":"success"}`)
	p.mu.Lock()
	tokens, busy := p.context, p.busy
	reap, compact = p.reapable(time.Now().Add(assistantIdleTimeout+time.Minute)), p.idleCompactable()
	p.mu.Unlock()
	if tokens != 9500 || busy {
		t.Fatalf("after the compaction: context %d, busy %v", tokens, busy)
	}
	if !reap || compact {
		t.Fatalf("compacted: let go next time (reapable %v), not compacted again (%v)", reap, compact)
	}
	if block, _ := n.turnContext(id, contextAnswer{Text: snapA}); !strings.Contains(block, "Live snapshot (") {
		t.Fatalf("after a compaction the whole snapshot again:\n%s", block)
	}
}

// A process that starts with other tools than the conversation's last one says so: that start writes the whole
// history again.
func TestToolsChangedIsLogged(t *testing.T) {
	n := assistantNode(t)
	var logs bytes.Buffer
	n.logger = log.New(&logs, "", 0)
	const id = "11111111-2222-3333-4444-555555555555"
	p := &assistantProc{id: id, agent: "claude"}
	start := func(tools string) {
		n.readAssistant(p, strings.NewReader(`{"type":"system","subtype":"init","tools":[`+tools+`]}`+"\n"))
	}
	start(`"Read","mcp__persona__agents_list","mcp__persona__phone_device_info"`)
	start(`"mcp__persona__phone_device_info","Read","mcp__persona__agents_list"`)
	if logs.Len() != 0 {
		t.Fatalf("the same tools in another order: %s", logs.String())
	}
	start(`"Read","mcp__persona__agents_list"`)
	if !strings.Contains(logs.String(), "tools changed since its last start (+[] -[mcp__persona__phone_device_info])") {
		t.Fatalf("logged %q", logs.String())
	}
}

// The hub's one-off About me block rides in the message after the snapshot, and its receipt is acked with the others.
func TestTurnContextCarriesTheRuleNotice(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	notice := `<persona-context kind="about-me">` + "\nnew notes\n</persona-context>"
	block, _ := n.turnContext(id, contextAnswer{Text: snapA, RuleNotice: notice})
	snapshot := strings.Index(block, "Live snapshot")
	if snapshot < 0 || !strings.HasSuffix(block, notice) || strings.Index(block, "kind=\"about-me\"") < snapshot {
		t.Fatalf("block:\n%s", block)
	}
	if block, _ := n.turnContext(id, contextAnswer{}); strings.Contains(block, "about-me") {
		t.Fatalf("no notice, no block: %q", block)
	}
}

// A supervision message carries no snapshot (the hub answers with none): what the conversation has seen stays as it
// was, and the owner's next message gets the delta since the last one that carried a snapshot, not the whole.
func TestTurnContextWithoutASnapshotLeavesWhatWasSeen(t *testing.T) {
	n := assistantNode(t)
	const id = "11111111-2222-3333-4444-555555555555"
	_, commit := n.turnContext(id, contextAnswer{Text: snapA})
	commit()
	seen := n.ctxSent[id]

	notice := `<persona-context kind="supervision-policy">` + "\nthe policy\n</persona-context>"
	for range 3 { // supervision messages, one with a one-off block
		block, commit := n.turnContext(id, contextAnswer{RuleNotice: notice})
		commit()
		if strings.Contains(block, "Live snapshot") || strings.Contains(block, "Now:") {
			t.Fatalf("a supervision message carries a snapshot:\n%s", block)
		}
		notice = ""
	}
	if len(n.ctxSent[id]) != len(seen) || n.ctxSent[id]["- Now"] != seen["- Now"] {
		t.Fatalf("what the conversation has seen changed: %v", n.ctxSent[id])
	}

	later := strings.NewReplacer("15:04", "16:20", "1 need you", "0 need you", "\n  - Need you: Fix login (server, session_id=aaaaaaaa)", "").Replace(snapA)
	block, _ := n.turnContext(id, contextAnswer{Text: later})
	if strings.Contains(block, "Live snapshot (") || !strings.Contains(block, "- Agents: 0 need you") || !strings.Contains(block, "- Now: 2026-11-01 Sun 16:20") ||
		strings.Contains(block, "Computers online") || strings.Contains(block, "Feishu calendar") {
		t.Fatalf("the owner's next message is not the delta:\n%s", block)
	}
}
