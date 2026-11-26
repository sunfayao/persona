package node

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/prompts"
)

// The words a block tells a section's rows with (prompts/assistant/turn_context.md), as a reader of the block meets them.
const (
	rowsMark    = "(only changed and new rows are listed; rows not listed are unchanged)"
	rowsRemoved = "  - (removed rows: "
	rowsOrder   = "  - (current row order: "
	goneSection = "- Sections no longer in this snapshot"
	goneNote    = " (discard their earlier content):"
	deltaIntro  = "Rows of the live snapshot that changed since the previous one (rows not listed are unchanged):"
	fullIntro   = "Live snapshot (as of when this message was sent; use tools to check later changes):"
	tasksHead   = "- Open task details (shown by priority, including personal to-dos; recently ended conversation tasks are listed too; task_plan; for existing results look here, and if that is not enough read its conversation with agent_read; use agent_send only to have it look something up, do something new or change direction):"
	tasksKey    = "- Open task details"
	// partialNote follows a section's name when the block tells it by its rows and its own heading did not change.
	partialNote = " " + rowsMark + ":"
)

func deltaBlock(lines ...string) string {
	return "<persona-context>\n" + deltaIntro + "\n" + strings.Join(lines, "\n") + "\n</persona-context>"
}

// readBlock is what a conversation holds of the snapshot (section key → text) once it has read block, going only by
// what the block says: a whole snapshot starts over, a section replaces the one it had, a section told by its rows
// changes those rows, and the sections named gone are dropped. It fails on a block that cannot be followed: a row
// taken away that was never there, an order that is not of the rows it has.
func readBlock(t *testing.T, view map[string]string, block string) map[string]string {
	t.Helper()
	body, ok := strings.CutPrefix(block, "<persona-context>\n")
	body, ok2 := strings.CutSuffix(body, "\n</persona-context>")
	intro, body, _ := strings.Cut(body, "\n")
	switch {
	case !ok || !ok2:
		t.Fatalf("not a block:\n%s", block)
	case intro == fullIntro:
		view = map[string]string{}
	case intro != deltaIntro || view == nil:
		t.Fatalf("a block of changes for a conversation that has %d sections starts %q", len(view), intro)
	}
	for _, s := range snapshotSections(body) {
		head, rows := sectionRows(s.text)
		if s.key == goneSection {
			for _, r := range rows {
				key := listMark + strings.TrimPrefix(strings.TrimSpace(r), listMark)
				if _, had := view[key]; !had {
					t.Fatalf("%q is told gone and was never there:\n%s", key, block)
				}
				delete(view, key)
			}
			continue
		}
		name, unchanged := strings.CutSuffix(head, partialNote)
		switch {
		case unchanged:
			head = ""
		case len(rows) > 0 && rows[0] == "  - "+rowsMark:
			name, rows = s.key, rows[1:]
		default:
			view[s.key] = s.text
			continue
		}
		old, had := view[name]
		id := rowIDs[name]
		if !had || id == nil {
			t.Fatalf("%q is told by its rows and the conversation has no such rows:\n%s", name, block)
		}
		oldHead, have := sectionRows(old)
		if head == "" {
			head = oldHead
		}
		at := func(k string) int { return slices.IndexFunc(have, func(r string) bool { return id(r) == k }) }
		var order []string
		for _, r := range rows {
			if list, ok := strings.CutPrefix(r, rowsRemoved); ok {
				for k := range strings.SplitSeq(strings.TrimSuffix(list, ")"), "; ") {
					i := at(k)
					if i < 0 {
						t.Fatalf("%s: row %q is removed and was never there:\n%s", name, k, block)
					}
					have = slices.Delete(have, i, i+1)
				}
			} else if list, ok := strings.CutPrefix(r, rowsOrder); ok {
				order = strings.Split(strings.TrimSuffix(list, ")"), "; ")
			} else if i := at(id(r)); i >= 0 {
				have[i] = r
			} else {
				have = append(have, r)
			}
		}
		if order != nil {
			sorted := make([]string, 0, len(have))
			for _, k := range order {
				if i := at(k); i >= 0 {
					sorted = append(sorted, have[i])
				}
			}
			if len(sorted) != len(have) || len(order) != len(have) {
				t.Fatalf("%s: the order %v is not of its %d rows:\n%s", name, order, len(have), block)
			}
			have = sorted
		}
		view[name] = strings.Join(append([]string{head}, have...), "\n")
	}
	return view
}

// sameAsSnapshot fails unless view, in the snapshot's order, is the snapshot byte for byte (less its blank lines,
// which no block carries).
func sameAsSnapshot(t *testing.T, view map[string]string, snapshot, block string) {
	t.Helper()
	var lines []string
	for line := range strings.SplitSeq(snapshot, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	snapshot = strings.Join(lines, "\n")
	sections := snapshotSections(snapshot)
	texts := make([]string, 0, len(sections))
	for _, s := range sections {
		texts = append(texts, view[s.key])
	}
	if got := strings.Join(texts, "\n"); got != snapshot || len(view) != len(sections) {
		t.Fatalf("after the block the conversation has %d sections:\n%s\n\nthe snapshot has %d:\n%s\n\nthe block:\n%s", len(view), got, len(sections), snapshot, block)
	}
}

func snapshotOf(sections ...string) string { return strings.Join(sections, "\n") }

func tasksOf(rows ...string) string { return strings.Join(append([]string{tasksHead}, rows...), "\n") }

const (
	rowLogin  = "  - Fix login (todo_1): working, server session_id=aaaaaaaa: running tests"
	rowDocs   = "  - Write docs (todo_2, link http://hub.example.com/todos?id=todo_2): paused: waiting for review"
	rowDeploy = "  - Deploy: canary (10% first) (todo_3): done (the user has not seen it yet)"
)

// One task's row changed: that row is all the block says of the tasks, on a heading that says the others stand.
func TestContextDeltaSendsTheChangedRowOnly(t *testing.T) {
	now := "- Now: 2026-11-01 Sun 15:04 (Asia/Shanghai)"
	before := snapshotOf(now, "- Computers online: Server (server)", tasksOf(rowLogin, rowDocs, rowDeploy), "- Scheduled follow-ups: none")
	_, seen := contextDelta(nil, before)
	changed := strings.Replace(rowLogin, "running tests", "tests passed, opening a PR", 1)
	block, seen := contextDelta(seen, strings.Replace(before, rowLogin, changed, 1))
	if want := deltaBlock(now, tasksKey+partialNote, changed); block != want {
		t.Fatalf("one changed row:\n%s\nwant\n%s", block, want)
	}

	// A row gone and a new one: the new row, and the id of the one that is gone.
	rowNew := "  - Check logs (todo_4): working"
	block, seen = contextDelta(seen, snapshotOf(now, "- Computers online: Server (server)", tasksOf(changed, rowDeploy, rowNew), "- Scheduled follow-ups: none"))
	if want := deltaBlock(now, tasksKey+partialNote, rowNew, rowsRemoved+"todo_2)"); block != want {
		t.Fatalf("a row removed, a row added:\n%s\nwant\n%s", block, want)
	}

	// The same rows in another order: only the order. A new row that is not the last: the row and the order.
	block, seen = contextDelta(seen, snapshotOf(now, "- Computers online: Server (server)", tasksOf(rowDeploy, changed, rowNew), "- Scheduled follow-ups: none"))
	if want := deltaBlock(now, tasksKey+partialNote, rowsOrder+"todo_3; todo_1; todo_4)"); block != want {
		t.Fatalf("rows reordered:\n%s\nwant\n%s", block, want)
	}
	block, seen = contextDelta(seen, snapshotOf(now, "- Computers online: Server (server)", tasksOf(rowDocs, rowDeploy, changed, rowNew), "- Scheduled follow-ups: none"))
	if want := deltaBlock(now, tasksKey+partialNote, rowDocs, rowsOrder+"todo_2; todo_3; todo_1; todo_4)"); block != want {
		t.Fatalf("a new first row:\n%s\nwant\n%s", block, want)
	}

	// The section is gone from the snapshot, and so is another: both are named, and seen forgets them, so one that
	// comes back is told whole.
	block, seen = contextDelta(seen, snapshotOf(now, "- Computers online: Server (server)"))
	if want := deltaBlock(now, goneSection+goneNote, "  - Open task details", "  - Scheduled follow-ups"); block != want {
		t.Fatalf("sections gone:\n%s\nwant\n%s", block, want)
	}
	block, _ = contextDelta(seen, snapshotOf(now, "- Computers online: Server (server)", tasksOf(rowDocs)))
	if want := deltaBlock(now, tasksOf(rowDocs)); block != want {
		t.Fatalf("a section that came back:\n%s\nwant\n%s", block, want)
	}
}

// The Agents rows go by their label. A heading that changed is sent as it is now, with the note under it; rows whose
// label is gone are named by it.
func TestContextDeltaAgentRowsByLabel(t *testing.T) {
	now := "- Now: 2026-11-01 Sun 15:04 (Asia/Shanghai)"
	needs := "  - Need you: Fix login (server, session_id=aaaaaaaa): waiting for your confirmation of the migration"
	unread := "  - Finished unread: Write docs (server, session_id=cccccccc); plan 2/5"
	waiting := "  - Waiting (the hub wakes them when it is time; ignore them): Run regression (server, session_id=bbbbbbbb): waiting for CI"
	head := "- Agents: 1 need you, 1 finished unread, 2 working (details: agents_list)"
	_, seen := contextDelta(nil, snapshotOf(now, head+"\n"+needs+"\n"+unread+"\n"+waiting))

	moved := strings.Replace(unread, "plan 2/5", "plan 3/5", 1)
	block, seen := contextDelta(seen, snapshotOf(now, head+"\n"+needs+"\n"+moved+"\n"+waiting))
	if want := deltaBlock(now, "- Agents"+partialNote, moved); block != want {
		t.Fatalf("one changed row:\n%s\nwant\n%s", block, want)
	}

	answered := "- Agents: 0 need you, 2 finished unread, 3 working (details: agents_list)"
	two := moved + "; Check logs (server, session_id=dddddddd)"
	block, _ = contextDelta(seen, snapshotOf(now, answered+"\n"+two+"\n"+waiting))
	if want := deltaBlock(now, answered, "  - "+rowsMark, two, rowsRemoved+"Need you)"); block != want {
		t.Fatalf("the heading and a row changed, a row gone:\n%s\nwant\n%s", block, want)
	}
}

// Working agents render only the heading count (#11261): when only that count changes, the new heading still reaches
// the conversation and reconstructs the snapshot cleanly, whether Agents has rows under it or not.
func TestContextDeltaWorkingCountChangesWithoutRows(t *testing.T) {
	base := prompts.Snapshot{
		Now:     time.Date(2026, 11, 1, 15, 4, 0, 0, time.UTC),
		Zone:    "UTC",
		Working: []prompts.SnapshotAgent{{Label: "Background task 1", Machine: "server", Session: "11111111"}},
	}
	for name, withRows := range map[string]bool{"head only": false, "with other rows": true} {
		s1 := base
		if withRows {
			s1.Needs = []prompts.SnapshotAgent{{Label: "Fix login", Machine: "server", Session: "aaaaaaaa", Line: "waiting for your confirmation"}}
			s1.Done = []prompts.SnapshotAgent{{Label: "Check logs", Machine: "server", Session: "bbbbbbbb"}}
		}
		snap1 := prompts.RenderSnapshot(s1)
		first, seen := contextDelta(nil, snap1)
		view := readBlock(t, nil, first)
		sameAsSnapshot(t, view, snap1, first)

		s2 := s1
		s2.Working = []prompts.SnapshotAgent{
			{Label: "Background task 1", Machine: "server", Session: "11111111"},
			{Label: "Background task 2", Machine: "server", Session: "22222222"},
			{Label: "Background task 3", Machine: "server", Session: "33333333"},
		}
		snap2 := prompts.RenderSnapshot(s2)
		block, _ := contextDelta(seen, snap2)
		if !strings.Contains(block, "3 working (details: agents_list)") {
			t.Fatalf("%s: updated working count missing from delta:\n%s", name, block)
		}
		if strings.Contains(block, "Background task") {
			t.Fatalf("%s: working agent title leaked into delta:\n%s", name, block)
		}
		view = readBlock(t, view, block)
		sameAsSnapshot(t, view, snap2, block)
	}
}

// Rows that cannot be told apart by an id (two with the same one, one with none), sections with no ids of their
// own, and a section told no shorter by its rows are sent whole, as before: unmarked, replacing what the conversation had.
func TestContextDeltaSendsWholeSections(t *testing.T) {
	now := "- Now: 2026-11-01 Sun 15:04 (Asia/Shanghai)"
	following := "- Following (2 item(s)): \n  - “Contract”; where: email; waiting on: legal; next step: chase; check again: 11-02 10:00\n  - “Expenses”; where: not written; waiting on: not written; next step: not written; check again: no time set"
	for name, c := range map[string]struct{ before, after string }{
		"two rows with one id":              {tasksOf(rowLogin, rowDocs, rowDeploy, rowLogin+" (duplicate)"), tasksOf(rowLogin+"！", rowDocs, rowDeploy, rowLogin+" (duplicate)")},
		"a row without an id":               {tasksOf(rowLogin, rowDocs, "  - A row without a number: working"), tasksOf(rowLogin+"！", rowDocs, "  - A row without a number: working")},
		"inbox follow-ups":                  {following, strings.Replace(following, "next step: chase", "next step: wait for a reply", 1)},
		"every row and the heading changed": {"- Agents: 1 need you, 1 finished unread, 1 working\n  - Need you: Fix login\n  - Finished unread: Write docs", "- Agents: 2 need you, 2 finished unread, 2 working\n  - Need you: Fix login; Check logs\n  - Finished unread: Write docs; Run regression"},
		"rows left out for size": {tasksOf(rowLogin, rowDocs, rowDeploy),
			"- Open task details (including personal to-dos): 3 in total, too many to fit in this snapshot, use todo_list"},
	} {
		_, seen := contextDelta(nil, snapshotOf(now, c.before))
		block, _ := contextDelta(seen, snapshotOf(now, c.after))
		if want := deltaBlock(now, c.after); block != want {
			t.Errorf("%s:\n%s\nwant\n%s", name, block, want)
		}
	}
}

// What the conversation is, and a one-time notice, come before the snapshot's own sections: a line of them that is no
// longer repeated is not told gone (the notice still holds), and is not sent again when it comes back unchanged.
func TestContextDeltaKeepsLinesBeforeTheSnapshot(t *testing.T) {
	now := "- Now: 2026-11-01 Sun 15:04 (Asia/Shanghai)"
	main := "**This is the main conversation**: the example user usually finds you here; carry on from what you talked about before."
	notice := "From now on the tasks you hand out that have no supervisor are followed by the background supervision conversation (conv_bg): their supervision messages go there."
	direction := "- conv_a: growth"
	_, seen := contextDelta(nil, snapshotOf(main, notice, direction, now))
	block, seen := contextDelta(seen, snapshotOf(main, now))
	if want := deltaBlock(now, goneSection+goneNote, "  - conv_a"); block != want {
		t.Fatalf("the notice was delivered and a direction is gone:\n%s\nwant\n%s", block, want)
	}
	if block, _ = contextDelta(seen, snapshotOf(main, notice, now)); block != deltaBlock(now) {
		t.Fatalf("the same notice again:\n%s", block)
	}
}

// rowIDs names sections of the snapshot the hub renders, and reads its rows' ids from their text: this renders the
// real templates, so a section renamed or a row laid out another way fails here, not silently as whole sections.
func TestSnapshotRowIDs(t *testing.T) {
	agent := func(label, session string) prompts.SnapshotAgent {
		return prompts.SnapshotAgent{Label: label, Machine: "server", Session: session, Line: "waiting: confirm (plan A; plan B)", Link: "http://hub.example.com/s/server/" + session}
	}
	s := prompts.Snapshot{
		Now: time.Date(2026, 11, 1, 15, 4, 0, 0, time.UTC), Zone: "UTC",
		Needs: []prompts.SnapshotAgent{agent("Fix login", "aaaaaaaa")}, Unread: []prompts.SnapshotAgent{agent("Write docs", "bbbbbbbb")},
		Awaiting: []prompts.SnapshotAgent{agent("Pick a plan", "cccccccc")}, Waiting: []prompts.SnapshotAgent{agent("Run regression", "dddddddd")},
		Done: []prompts.SnapshotAgent{agent("Check logs", "eeeeeeee")}, Muted: []prompts.SnapshotAgent{agent("Clean branches", "ffffffff")},
		Working: []prompts.SnapshotAgent{agent("Upgrade dependencies", "11111111")},
		Tasks: []prompts.SnapshotTask{
			{ID: "todo_0123456789abcdef", Title: "Research: login (iOS); options", State: "working", Link: "http://hub.example.com/todos?id=todo_0123456789abcdef", Last: "finished reading (todo_ffff): writing"},
			{ID: "todo_2", Title: "Write docs", State: "paused", Health: "normal", Machine: "server", Session: "bbbbbbbb"},
		},
		OpenTodosLoaded: true, OpenTodosTotal: 2,
		Chats:            &prompts.SnapshotChats{AtMe: []prompts.SnapshotChat{{Name: "Example group", Unread: 2, Preview: "look: this"}}, Other: []prompts.SnapshotChat{{Name: "Chatter group", Unread: 1}}},
		FollowingEnabled: true, FollowingTotal: 1, Following: []prompts.SnapshotFollowing{{Title: "Contract"}},
	}
	want := map[string][]string{
		"- Agents":      {"Need you", "Finished unread", "Asking the user, seen but unanswered", "Waiting", "Done", "Muted"},
		"- Open task details": {"todo_0123456789abcdef", "todo_2"},
		"- Feishu unread chats": {"@me", "Other"},
	}
	scoped := s
	scoped.Scoped = true
	wantScoped := map[string][]string{"- Agents": want["- Agents"], "- Tasks this conversation handed out or supervises": want["- Open task details"], "- Feishu unread chats": want["- Feishu unread chats"]}
	seenKeys := map[string]bool{}
	for name, c := range map[string]struct {
		text string
		want map[string][]string
	}{"main": {prompts.RenderSnapshot(s), want}, "scoped": {prompts.RenderSnapshot(scoped), wantScoped}} {
		for _, sec := range snapshotSections(c.text) {
			_, rows := sectionRows(sec.text)
			id := rowIDs[sec.key]
			if id == nil {
				if len(c.want[sec.key]) > 0 {
					t.Errorf("%s: section %q has no rowIDs", name, sec.key)
				}
				continue
			}
			seenKeys[sec.key] = true
			if ids, ok := idsOf(rows, id); !ok || !slices.Equal(ids, c.want[sec.key]) {
				t.Errorf("%s: section %q: row ids %q (each row its own: %v), want %q\n%s", name, sec.key, ids, ok, c.want[sec.key], sec.text)
			}
		}
	}
	for key := range rowIDs {
		if !seenKeys[key] {
			t.Errorf("rowIDs has %q, which the snapshot does not render:\n%s", key, prompts.RenderSnapshot(s))
		}
	}
	// The sections with rows and no ids of their own stay whole.
	for _, key := range []string{"- Following"} {
		found := false
		for _, sec := range snapshotSections(prompts.RenderSnapshot(s)) {
			if _, rows := sectionRows(sec.text); sec.key == key && len(rows) > 0 {
				found = true
			}
		}
		if !found || rowIDs[key] != nil {
			t.Errorf("section %q: rendered with rows %v, in rowIDs %v", key, found, rowIDs[key] != nil)
		}
	}
}

// A section is the same one whatever it says at the moment: its count, its note in parentheses and its empty form
// do not make it another, so the old one is replaced and not left beside the new.
func TestSectionKeyIsTheSubject(t *testing.T) {
	for _, same := range [][]string{
		{"- Feishu calendar (meetings today that have not ended yet; ones already finished are not listed): 16:00–17:00 Review", "- Feishu calendar (no more meetings today; ones already finished are not listed)"},
		{"- Following (3 item(s)):", "- Following: none"},
		{"- Feishu unread chats 3 (muted chats are not counted; view them with lark_chats / lark_messages)", "- Feishu unread chats 12 (muted chats are not counted; view them with lark_chats / lark_messages)"},
		{"- Waiting for the user's go-ahead to ship (2 item(s); list them together for the user to approve in one go, do not ask one by one): “a”", "- Waiting for the user's go-ahead to ship (3 item(s); list them together for the user to approve in one go, do not ask one by one): “a”; “b”"},
		{"- Open to-do index (IDs only): todo_1", "- Open to-do index (IDs and titles): todo_1 “Fix login”"},
	} {
		if a, b := sectionKey(same[0]), sectionKey(same[1]); a != b || a == "" {
			t.Errorf("%q and %q are one section: keys %q and %q", same[0], same[1], a, b)
		}
	}
	keys := map[string]string{}
	for _, line := range []string{"- Open to-dos: 3 in total (full list: todo_list)", "- Open to-do index (IDs only): todo_1", "- Feishu: no unread (muted chats are not counted)",
		"- Feishu calendar (no more meetings today)", "- Scheduled follow-ups: none", "- Daily routines: Morning brief (every day 08:00)", "- conv_a: growth", "- conv_b: growth"} {
		k := sectionKey(line)
		if other, taken := keys[k]; taken {
			t.Errorf("%q and %q are two sections with one key %q", other, line, k)
		}
		keys[k] = line
	}
	// Two sections that do share a subject stay two.
	twice := snapshotSections("- Note: a\n- Note: b")
	if len(twice) != 2 || twice[0].key == twice[1].key {
		t.Errorf("two sections with one subject: %+v", twice)
	}
}

// A random run of snapshots, with every kind of change at once (rows changed, added, removed, reordered; headings
// changed; sections gone and back, left out for size, with rows that have no id or share one): reading the blocks one
// after another leaves a conversation with exactly the snapshot each was made from, and a conversation that was
// compacted in between gets a whole one.
func TestContextDeltaBlocksAddUpToTheSnapshot(t *testing.T) {
	partial, whole := 0, 0
	for seed := range uint64(40) {
		r := rand.New(rand.NewPCG(seed, 9078))
		g := newSnapshotGen(r)
		var seen, view map[string]string
		for step := range 60 {
			snapshot := g.next()
			if r.IntN(15) == 0 {
				seen = nil // compacted, or the node restarted
			}
			var block string
			block, seen = contextDelta(seen, snapshot)
			if !strings.Contains(block, "\n- Now: ") {
				t.Fatalf("seed %d step %d: no time:\n%s", seed, step, block)
			}
			partial += strings.Count(block, rowsMark)
			whole += strings.Count(block, tasksHead)
			view = readBlock(t, view, block)
			if t.Failed() {
				t.Fatalf("seed %d step %d", seed, step)
			}
			sameAsSnapshot(t, view, snapshot, fmt.Sprintf("(seed %d step %d)\n%s", seed, step, block))
		}
	}
	if partial < 300 || whole < 100 {
		t.Fatalf("the run told %d sections by their rows and the tasks whole %d times: it does not cover both", partial, whole)
	}
}

// snapshotGen makes snapshots of synthetic sections that drift from one to the next.
type snapshotGen struct {
	r        *rand.Rand
	n        int
	tasks    []string // to-do ids, in order
	rev      map[string]int
	labels   map[string]int // Agents row label → revision, 0 for none
	chats    map[string]int
	misc     int
	gone     map[string]bool // sections the snapshot leaves out now
	omit     bool
	noID     bool
	nextTask int
}

func newSnapshotGen(r *rand.Rand) *snapshotGen {
	return &snapshotGen{r: r, rev: map[string]int{}, labels: map[string]int{"Need you": 1, "Finished unread": 1}, chats: map[string]int{"@me": 1}, gone: map[string]bool{}}
}

var genLabels = []string{"Need you", "Finished unread", "Waiting (the hub wakes them when it is time; ignore them)", "Done (last 24 hours)"}

func (g *snapshotGen) next() string {
	r := g.r
	g.n++
	for range r.IntN(3) { // a few changes each time
		switch r.IntN(12) {
		case 0, 1, 2: // a task's row changes
			if len(g.tasks) > 0 {
				g.rev[g.tasks[r.IntN(len(g.tasks))]]++
			}
		case 3: // a new task, anywhere
			g.nextTask++
			id := fmt.Sprintf("todo_%04x", g.nextTask)
			g.tasks = slices.Insert(g.tasks, r.IntN(len(g.tasks)+1), id)
		case 4: // one goes
			if len(g.tasks) > 0 {
				i := r.IntN(len(g.tasks))
				g.tasks = slices.Delete(g.tasks, i, i+1)
			}
		case 5: // they change places
			r.Shuffle(len(g.tasks), func(i, j int) { g.tasks[i], g.tasks[j] = g.tasks[j], g.tasks[i] })
		case 6, 7: // an Agents row appears, changes or goes
			l := genLabels[r.IntN(len(genLabels))]
			if g.labels[l] > 0 && r.IntN(3) == 0 {
				g.labels[l] = 0
			} else {
				g.labels[l] = g.n
			}
		case 8: // so does a chats row
			l := []string{"@me", "Other"}[r.IntN(2)]
			if g.chats[l] > 0 && r.IntN(2) == 0 {
				g.chats[l] = 0
			} else {
				g.chats[l] = g.n
			}
		case 9:
			g.misc += r.IntN(2)
		case 10: // a section goes or comes back
			k := []string{"tasks", "agents", "chats", "followups", "plans"}[r.IntN(5)]
			g.gone[k] = !g.gone[k]
		case 11:
			g.omit, g.noID = r.IntN(4) == 0, r.IntN(4) == 0
		}
	}
	out := []string{"**This is the main conversation**: the example user usually finds you here.", fmt.Sprintf("- Now: 2026-11-01 Sun 15:%02d (UTC)", g.n%60), "- Computers online: Server (server)"}
	if !g.gone["agents"] {
		sec := fmt.Sprintf("- Agents: %d need you, %d finished unread, %d working (details: agents_list)", g.labels["Need you"]%3, g.labels["Finished unread"]%3, g.n%4)
		for _, l := range genLabels {
			if g.labels[l] > 0 {
				sec += fmt.Sprintf("\n  - %s: Example agent %d (server, session_id=%08x): revision %d; still waiting", l, g.labels[l], g.labels[l], g.labels[l])
			}
		}
		out = append(out, sec)
	}
	if !g.gone["tasks"] {
		switch {
		case g.omit:
			out = append(out, fmt.Sprintf("%s %d in total, too many to fit in this snapshot, use todo_list", tasksHead, len(g.tasks)))
		case len(g.tasks) > 0:
			sec := tasksHead
			for i, id := range g.tasks {
				row := fmt.Sprintf("\n  - Example task: %s (step %d) (%s, link http://hub.example.com/todos?id=%s): working; supervision: normal: revision %d", id[5:], i%3, id, id, g.rev[id])
				if g.noID && i == 0 {
					row = fmt.Sprintf("\n  - A task without a number: revision %d", g.rev[id])
				}
				sec += row
			}
			out = append(out, sec)
		}
	}
	if !g.gone["chats"] {
		if g.chats["@me"]+g.chats["Other"] == 0 {
			out = append(out, "- Feishu: no unread (muted chats are not counted)")
		} else {
			sec := fmt.Sprintf("- Feishu unread chats %d (muted chats are not counted; view them with lark_chats / lark_messages)", 1+(g.chats["@me"]+g.chats["Other"])%5)
			for _, l := range []string{"@me", "Other"} {
				if g.chats[l] > 0 {
					sec += fmt.Sprintf("\n  - %s: Example group (%d): preview %d", l, g.chats[l], g.chats[l])
				}
			}
			out = append(out, sec)
		}
	}
	if !g.gone["plans"] {
		out = append(out, fmt.Sprintf("- Plans waiting for the user's Start (cards in the main conversation, work begins only when the user clicks): 11-01 15:00 Example plan %d", g.misc))
	}
	if !g.gone["followups"] {
		out = append(out, "- Scheduled follow-ups: none")
	}
	return strings.Join(out, "\n")
}

// A conversation that was compacted, and a node that restarted, send the whole snapshot next, rows and all; in
// between, a changed row goes alone.
func TestTurnContextIsWholeAfterCompactionAndRestart(t *testing.T) {
	const id = "11111111-2222-3333-4444-555555555555"
	now := "- Now: 2026-11-01 Sun 15:04 (Asia/Shanghai)"
	first := snapshotOf(now, tasksOf(rowLogin, rowDocs, rowDeploy))
	changed := strings.Replace(rowLogin, "running tests", "tests passed", 1)
	second := snapshotOf(now, tasksOf(changed, rowDocs, rowDeploy))
	send := func(n *Node, snapshot string) string {
		block, commit := n.turnContext(id, contextAnswer{Text: snapshot})
		commit()
		return block
	}
	wholeBlock := func(snapshot string) string {
		return "<persona-context>\n" + fullIntro + "\n" + snapshot + "\n</persona-context>"
	}

	n := assistantNode(t)
	if block := send(n, first); block != wholeBlock(first) {
		t.Fatalf("a new conversation:\n%s", block)
	}
	if block, want := send(n, second), deltaBlock(now, tasksKey+partialNote, changed); block != want {
		t.Fatalf("a changed row:\n%s\nwant\n%s", block, want)
	}
	n.contextCompacted(id)
	if block := send(n, first); block != wholeBlock(first) {
		t.Fatalf("after a compaction:\n%s", block)
	}
	if block := send(assistantNode(t), second); block != wholeBlock(second) {
		t.Fatalf("after a node restart:\n%s", block)
	}
	// A message that did not go out (no commit) leaves the conversation where it was: the row is told again.
	if block, _ := n.turnContext(id, contextAnswer{Text: second}); !strings.Contains(block, changed) {
		t.Fatalf("an unsent block:\n%s", block)
	}
	if block, want := send(n, second), deltaBlock(now, tasksKey+partialNote, changed); block != want {
		t.Fatalf("the row after an unsent block:\n%s\nwant\n%s", block, want)
	}
}

// sectionDeltaBefore is contextDelta as it was before rows were told apart (#9078): a changed section whole, with
// the section keys of then. The measurement below compares against it.
func sectionDeltaBefore(seen map[string]string, snapshot string) (string, map[string]string) {
	key := func(line string) string {
		k, _, _ := strings.Cut(line, ":")
		k, _, _ = strings.Cut(k, " (")
		return strings.TrimSpace(k)
	}
	next := map[string]string{}
	for k, v := range seen {
		next[k] = v
	}
	c := prompts.TurnContext{Full: seen == nil}
	for _, s := range snapshotSections(snapshot) {
		head, _, _ := strings.Cut(s.text, "\n")
		k := key(head)
		if seen == nil || k == timeSection || seen[k] != s.text {
			c.Sections = append(c.Sections, prompts.TurnSection{Text: s.text})
		}
		next[k] = s.text
	}
	return prompts.RenderTurnContext(c), next
}

// shapedSnapshots is a synthetic main conversation's snapshots, rendered by the real templates: tasks to-dos with
// an agent each, of which one says something new before every message, one more now and then; the working agents'
// plans move on every third message; every eighth a task ends and a new one starts, and an agent needs the user.
func shapedSnapshots(messages, tasks int) []string {
	say := []string{"read the code, writing the implementation", "implementation written, running unit tests", "tests passed, tidying the commit message", "opened a PR, waiting for the checks", "checks passed, reading the review comments"}
	type task struct{ id, rev int }
	var open []task
	for i := range tasks {
		open = append(open, task{id: i + 1})
	}
	nextID := tasks + 1
	var out []string
	for m := range messages {
		if m > 0 {
			open[m%len(open)].rev++
			if m%4 == 0 {
				open[(m+3)%len(open)].rev++
			}
			if m%8 == 0 {
				open = append([]task{{id: nextID}}, open[:len(open)-1]...)
				nextID++
			}
		}
		s := prompts.Snapshot{Now: time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).Add(time.Duration(m) * 7 * time.Minute), Zone: "UTC",
			Machines: []prompts.SnapshotMachine{{Name: "Server", ID: "server", Online: true}}, OpenTodosLoaded: true, OpenTodosTotal: len(open)}
		for i, t := range open {
			id := fmt.Sprintf("todo_%016x", t.id)
			session := fmt.Sprintf("%08x", 0xa0000000+t.id)
			title := fmt.Sprintf("Example task %d: tidy the interface and docs of module %d", t.id, t.id)
			s.Tasks = append(s.Tasks, prompts.SnapshotTask{ID: id, Title: title, State: "working", Health: "normal", DoneWhen: 2, Machine: "server", Session: session,
				Link: "http://hub.example.com/todos?id=" + id, Last: say[t.rev%len(say)], Plan: &prompts.SnapshotAgentPlan{Done: t.rev % 6, Total: 6, Current: "step " + fmt.Sprint(t.rev%6+1)}})
			s.OpenTodos = append(s.OpenTodos, prompts.SnapshotOpenTodo{ID: id, Title: title})
			a := prompts.SnapshotAgent{Label: title, Machine: "server", Session: session, Link: "http://hub.example.com/s/server/" + session}
			switch {
			case i == 0 && m%8 >= 6:
				a.Line = "waiting for your confirmation: merge first or add tests first"
				s.Needs = append(s.Needs, a)
			case i%4 == 3:
				a.Line = "waiting for the checks"
				s.Waiting = append(s.Waiting, a)
			default:
				a.Plan = &prompts.SnapshotAgentPlan{Done: m / 3 % 6, Total: 6, Current: "step " + fmt.Sprint(m/3%6+1)}
				s.Working = append(s.Working, a)
			}
		}
		out = append(out, prompts.RenderSnapshot(s))
	}
	return out
}

// Before and after on the same snapshots (#9078): two synthetic conversations with the shape of the two measured
// ones, a short one whose sections repeated about 1.8 KB of rows that had not changed and a long one that repeated
// about 33 KB. Run with -v for the numbers. Told by rows, a block repeats no unchanged row at all.
func TestContextDeltaBytesBeforeAndAfter(t *testing.T) {
	// repeated is how many bytes of block are rows the conversation already had, unchanged.
	repeated := func(seen map[string]string, block string) int {
		had := map[string]bool{}
		for _, text := range seen {
			_, rows := sectionRows(text)
			for _, r := range rows {
				had[r] = true
			}
		}
		n := 0
		for line := range strings.SplitSeq(block, "\n") {
			if had[line] {
				n += len(line) + 1
			}
		}
		return n
	}
	for _, shape := range []struct {
		name            string
		messages, tasks int
	}{{"short", 4, 3}, {"long", 13, 10}} {
		var seenBefore, seenAfter, view map[string]string
		var before, after, repeatedBefore, repeatedAfter, first int
		for i, snapshot := range shapedSnapshots(shape.messages, shape.tasks) {
			old, nextBefore := sectionDeltaBefore(seenBefore, snapshot)
			block, nextAfter := contextDelta(seenAfter, snapshot)
			view = readBlock(t, view, block)
			sameAsSnapshot(t, view, snapshot, block)
			if i == 0 {
				if first = len(block); old != block {
					t.Fatalf("%s: the first, whole block differs:\n%s\nbefore:\n%s", shape.name, block, old)
				}
			} else {
				before, after = before+len(old), after+len(block)
				repeatedBefore, repeatedAfter = repeatedBefore+repeated(seenBefore, old), repeatedAfter+repeated(seenAfter, block)
			}
			seenBefore, seenAfter = nextBefore, nextAfter
		}
		t.Logf("%s (%d messages, %d tasks): first block %d B; the %d blocks after it: by section %d B (%d B of unchanged rows), by row %d B (%d B of unchanged rows), %d%% fewer",
			shape.name, shape.messages, shape.tasks, first, shape.messages-1, before, repeatedBefore, after, repeatedAfter, 100-100*after/before)
		if repeatedAfter != 0 || after >= before {
			t.Errorf("%s: by row %d B with %d B of unchanged rows, by section %d B", shape.name, after, repeatedAfter, before)
		}
	}
}
