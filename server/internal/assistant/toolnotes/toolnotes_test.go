package toolnotes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/approval"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// fake is the assistant as the notes see it: a store in memory, a clock the test moves, the tools, the computers, the
// friction drafts and the to-dos, and cards answered as the test says (answer).
type fake struct {
	mu       sync.Mutex
	vals     map[string][]byte
	now      time.Time
	tools    []proto.Tool
	runtime  string
	machines map[string]string // id → name
	drafts   []Draft
	answer   string // the cards' answer: allow, deny, expire or wait (the card stays up: ops keeps it)
	cards    []Card
	ops      []approval.Operation
	notified int             // Changed(true)
	waiting  int             // Changed(false)
	todos    map[string]bool // id → done
	filed    []filed
	repo     *Repository
}

type filed struct {
	title, notes string
	repo         *Repository
}

func newFake() *fake {
	return &fake{vals: map[string][]byte{}, now: time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC), answer: "allow",
		tools: []proto.Tool{
			{Name: "computer_run", Description: "Runs steps on a Mac."},
			{Name: "agent_send", Description: "Sends a message to an agent."},
			{Name: "todo_create", Description: "Records a to-do."},
		},
		machines: map[string]string{"m-studio": "studio", "m-lab": "lab-mini"}, todos: map[string]bool{}}
}

func (f *fake) host(t *testing.T) Host {
	return Host{
		Load: func(_ context.Context, key string, out any) (bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			b, ok := f.vals[key]
			if !ok {
				return false, nil
			}
			return true, json.Unmarshal(b, out)
		},
		Save: func(_ context.Context, key string, v any) error {
			b, err := json.Marshal(v)
			f.mu.Lock()
			defer f.mu.Unlock()
			f.vals[key] = b
			return err
		},
		Now:  func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now },
		Logf: t.Logf,
		Tools: func(context.Context) []proto.Tool {
			f.mu.Lock()
			defer f.mu.Unlock()
			return slices.Clone(f.tools)
		},
		Runtime: func(context.Context) string { f.mu.Lock(); defer f.mu.Unlock(); return f.runtime },
		Machine: func(_ context.Context, name string) (string, string, bool) {
			for id, n := range f.machines {
				if id == name || strings.EqualFold(n, name) {
					return id, n, true
				}
			}
			return "", "", false
		},
		MachineName: func(id string) string { return f.machines[id] },
		Private: func(context.Context) []string {
			var names []string
			for id, n := range f.machines {
				names = append(names, id, n)
			}
			return names
		},
		Drafts: func(context.Context) ([]Draft, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			return slices.Clone(f.drafts), nil
		},
		UpdateDraft: func(_ context.Context, id string, fn func(*Draft) error) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			i := slices.IndexFunc(f.drafts, func(x Draft) bool { return x.ID == id })
			if i < 0 {
				return ErrNoDraft
			}
			x := f.drafts[i]
			if err := fn(&x); err != nil {
				return err
			}
			f.drafts[i] = x
			return nil
		},
		Confirm: func(ctx context.Context, card Card, op approval.Operation) (approval.Outcome, error) {
			f.mu.Lock()
			f.cards, f.ops = append(f.cards, card), append(f.ops, op)
			answer := f.answer
			f.mu.Unlock()
			switch answer {
			case "allow": // as the coordinator does: a stale proposal is no error of the call
				text, err := op.Apply(ctx, proto.Resolution{})
				if errors.Is(err, approval.ErrStale) {
					text, err = "stale", nil
				}
				if op.Discard != nil {
					op.Discard(ctx, "resolved")
				}
				return approval.Outcome{Text: text}, err
			case "deny", "expire":
				status, text := "denied", op.Denied
				if answer == "expire" {
					status, text = "expired", op.Unanswered
				}
				if op.Discard != nil {
					op.Discard(ctx, status)
				}
				return approval.Outcome{Text: text}, nil
			}
			return approval.Outcome{Clarifying: true, Text: approval.WaitingText}, nil
		},
		Repository: func(context.Context, string) (Repository, bool) {
			if f.repo == nil {
				return Repository{}, false
			}
			return *f.repo, true
		},
		FileTodo: func(_ context.Context, _ Call, title, notes string, repo *Repository) (string, bool, error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			for i, x := range f.filed {
				if x.title == title {
					return fmt.Sprintf("todo_%d", i+1), true, nil
				}
			}
			f.filed = append(f.filed, filed{title, notes, repo})
			return fmt.Sprintf("todo_%d", len(f.filed)), false, nil
		},
		TodoDone: func(id string) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.todos[id] },
		Links: func(_ context.Context, conversations []Evidence) []string {
			var out []string
			for _, c := range conversations {
				out = append(out, "https://persona.example.com/persona/"+c.Session)
			}
			return out
		},
		Changed: func(rendered bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if rendered {
				f.notified++
			} else {
				f.waiting++
			}
		},
	}
}

func (f *fake) set(fn func(*fake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fake) notifications() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.notified
}

var call = Call{Machine: "m-studio", Session: "main", Lang: i18n.En, Zone: time.UTC}

// run is one tool_note call; args is JSON.
func run(t *testing.T, d *Domain, args string) (string, error) {
	t.Helper()
	return d.Tool(context.Background(), call, json.RawMessage(args))
}

func mustRun(t *testing.T, d *Domain, args string) string {
	t.Helper()
	out, err := run(t, d, args)
	if err != nil {
		t.Fatalf("%s: %v", args, err)
	}
	return out
}

// descriptions are the tools' descriptions, with notes, as a conversation's list has them.
func descriptions(d *Domain, f *fake) map[string]string {
	f.mu.Lock()
	tools := slices.Clone(f.tools)
	f.mu.Unlock()
	out := map[string]string{}
	for _, t := range d.Annotate(context.Background(), tools) {
		out[t.Name] = t.Description
	}
	return out
}

func held(d *Domain) []Note {
	return d.held(context.Background()).Notes
}

// A proposal waits for the user's tap: approved, it is at the end of its tool's description in a "Field notes:" block,
// the tool-list revision moves and the tool watches are told once; the stored note says who proposed it, its tool's
// digest and when it is due for review.
func TestApprovedNoteIsRenderedAndChangesTheRevision(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	if rev := d.Revision(context.Background()); rev != "" {
		t.Fatalf("revision with no notes = %q", rev)
	}
	out := mustRun(t, d, `{"action":"propose","tool":"computer_run","text":"Open apps with a script (await cua.launch), never a launch_app step."}`)
	if !strings.HasPrefix(out, "Approved: note tn_") || !strings.HasSuffix(out, " now ends computer_run's description, until 2027-02-26.") {
		t.Fatalf("propose = %q", out)
	}
	if len(f.cards) != 1 || f.cards[0].Machine != "m-studio" || f.cards[0].Session != "main" ||
		f.cards[0].Summary != "Add a field note to computer_run: “Open apps with a script (await cua.launch), never a launch_app step.”" {
		t.Fatalf("cards = %+v", f.cards)
	}
	got := descriptions(d, f)["computer_run"]
	want := "Runs steps on a Mac.\n\nField notes:\n- Open apps with a script (await cua.launch), never a launch_app step."
	if got != want {
		t.Fatalf("description = %q\nwant %q", got, want)
	}
	if descriptions(d, f)["agent_send"] != "Sends a message to an agent." {
		t.Fatal("another tool's description changed")
	}
	if rev := d.Revision(context.Background()); !strings.HasPrefix(rev, ":notes=") {
		t.Fatalf("revision = %q", rev)
	}
	if f.notifications() != 1 {
		t.Fatalf("watches told %d times, want once", f.notifications())
	}
	notes := held(d)
	if len(notes) != 1 || notes[0].Status != StatusApproved || notes[0].By != "main" || notes[0].PromptDigest != Digest(f.tools[0]) ||
		!notes[0].ReviewAt.Equal(f.now.Add(ReviewDays*day)) || notes[0].ApprovedAt.IsZero() {
		t.Fatalf("stored = %+v", notes)
	}
	// What is stored is what a new Domain reads: the note is shown after a restart.
	if again := New(f.host(t)); descriptions(again, f)["computer_run"] != want {
		t.Fatal("a restarted Domain does not show the stored note")
	}
}

// Declined or unanswered, a proposal never reaches a description: it retires saying how, and the call says nothing
// was added. While its card waits it is listed as waiting and shown nowhere.
func TestDeclinedAndUnansweredProposalsStayOut(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	f.set(func(f *fake) { f.answer = "deny" })
	if out := mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read the agent first."}`); out != "The user declined the field note: it was not added." {
		t.Fatalf("declined = %q", out)
	}
	f.set(func(f *fake) { f.answer = "expire" })
	if out := mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Send one clear message."}`); !strings.Contains(out, "it was not added") {
		t.Fatalf("unanswered = %q", out)
	}
	f.set(func(f *fake) { f.answer = "wait" })
	if out := mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Use task for new work."}`); out != approval.WaitingText {
		t.Fatalf("waiting = %q", out)
	}
	notes := held(d)
	if len(notes) != 3 || notes[0].RetiredReason != reasonDeclined || notes[1].RetiredReason != "not approved: its card ended (expired)" ||
		notes[2].Status != StatusProposed {
		t.Fatalf("notes = %+v", notes)
	}
	if desc := descriptions(d, f)["agent_send"]; strings.Contains(desc, "Field notes") {
		t.Fatalf("an unapproved note is shown: %q", desc)
	}
	if list := mustRun(t, d, `{"action":"list"}`); !strings.Contains(list, "agent_send, waiting for the user's tap: “Use task for new work.”") ||
		strings.Contains(list, "Read the agent first.") {
		t.Fatalf("list = %q", list)
	}
	// The waiting card ends unanswered later: the note retires; a late Allow of an ended card applies nothing.
	f.ops[2].Discard(context.Background(), "expired")
	if n := held(d)[2]; n.Status != StatusRetired {
		t.Fatalf("after its card expired: %+v", n)
	}
	if _, err := f.ops[2].Apply(context.Background(), proto.Resolution{}); !errors.Is(err, approval.ErrStale) {
		t.Fatalf("a retired proposal was approved: %v", err)
	}
	if f.notifications() != 0 {
		t.Fatalf("watches told %d times about notes never shown", f.notifications())
	}
}

// Nothing that carries a secret, a path, an address or one of the user's computers' names is shown on a card or kept:
// the call is refused with the text as the scrubber leaves it. A draft's evidence is checked too.
func TestNotesCarryingPrivateValuesAreRefused(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.drafts = []Draft{{ID: "fd_1", Kind: DraftNote, Tool: "agent_send", Code: "studio_offline", Count: 2, Status: DraftPending}}
	d := New(f.host(t))
	for _, tc := range []struct{ args, scrubbed string }{
		{`{"action":"propose","tool":"agent_send","text":"Pass token=abcdef123456 in the header."}`, "token=<redacted>"},
		{`{"action":"propose","tool":"agent_send","text":"The log is in /home/alex/work/run.log."}`, "~/work/run.log"},
		{`{"action":"propose","tool":"agent_send","text":"It only works on studio, the big Mac."}`, "<machine>"},
		{`{"action":"propose","tool":"agent_send","text":"Ask ops@example.com first."}`, "<email>"},
		{`{"action":"propose","tool":"agent_send","text":"Call https://hub.example.com/api directly."}`, "https://<host>/api"},
		{`{"action":"propose","draft_id":"fd_1","text":"Wait until it is back."}`, "with <machine>_offline"},
	} {
		_, err := run(t, d, tc.args)
		if err == nil || !strings.Contains(err.Error(), "not proposed") || !strings.Contains(err.Error(), tc.scrubbed) {
			t.Errorf("%s: %v, want a refusal showing %q", tc.args, err, tc.scrubbed)
		}
	}
	if len(f.cards) != 0 || len(held(d)) != 0 {
		t.Fatalf("cards %+v, notes %+v", f.cards, held(d))
	}
}

// A note is for a tool in the list, on a known computer and runtime, in one line of at most 200 characters; anything
// else is refused before a card, and so is the same note twice.
func TestProposeChecksToolMachineRuntimeAndText(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	for args, want := range map[string]string{
		`{"action":"propose","tool":"no_such_tool","text":"x"}`:                              `no tool "no_such_tool"`,
		`{"action":"propose","text":"x"}`:                                                    "tool is required",
		`{"action":"propose","tool":"agent_send","text":"x","machine":"nowhere"}`:            `no computer "nowhere"`,
		`{"action":"propose","tool":"agent_send","text":"x","runtime":"gemini"}`:             "runtime must be claude, codex or agy",
		`{"action":"propose","tool":"agent_send","text":"one\ntwo"}`:                         "one line",
		`{"action":"propose","tool":"agent_send","text":" "}`:                                "text is required",
		`{"action":"propose","tool":"agent_send","text":"` + strings.Repeat("a", 201) + `"}`: "201 characters",
		`{"action":"propose","tool":"agent_send","text":"x","review_days":30}`:               "unknown field",
		`{"action":"renew","id":"tn_1"}`:                                                     "action must be propose, list, retire or file_fix",
	} {
		if _, err := run(t, d, args); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", args, err, want)
		}
	}
	if len(f.cards) != 0 {
		t.Fatalf("cards shown: %+v", f.cards)
	}
	mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read first."}`)
	if _, err := run(t, d, `{"action":"propose","tool":"agent_send","text":"read first."}`); err == nil || !strings.Contains(err.Error(), "already there") {
		t.Fatalf("a duplicate: %v", err)
	}
}

// A runtime-scoped note is shown only to conversations on that runtime; a machine-scoped one is shown everywhere and
// says which computer it holds on, by its name.
func TestRuntimeAndMachineScoping(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	f.set(func(f *fake) { f.runtime = "claude" })
	mustRun(t, d, `{"action":"propose","tool":"computer_run","text":"Call persona_call with describe first.","runtime":"agy"}`)
	mustRun(t, d, `{"action":"propose","tool":"computer_run","text":"Its screen is small; read before clicking.","machine":"lab-mini"}`)
	if len(f.cards) != 2 || f.cards[0].Summary != "Add a field note to computer_run (agy): “Call persona_call with describe first.”" ||
		f.cards[1].Summary != "Add a field note to computer_run (lab-mini): “Its screen is small; read before clicking.”" ||
		string(f.cards[1].Shown) != `{"text":"Its screen is small; read before clicking.","tool":"computer_run","where":"lab-mini"}` {
		t.Fatalf("cards = %+v", f.cards)
	}
	claude := descriptions(d, f)["computer_run"]
	if strings.Contains(claude, "persona_call") || !strings.Contains(claude, "- On lab-mini: Its screen is small; read before clicking.") {
		t.Fatalf("on claude: %q", claude)
	}
	before := d.Revision(context.Background())
	f.set(func(f *fake) { f.runtime = "agy" })
	if agy := descriptions(d, f)["computer_run"]; !strings.Contains(agy, "- Call persona_call with describe first.") {
		t.Fatalf("on agy: %q", agy)
	}
	if d.Revision(context.Background()) == before {
		t.Fatal("the revision did not change with the runtime")
	}
	if n := held(d)[1]; n.Machine != "m-lab" {
		t.Fatalf("machine kept as %q, want its id", n.Machine)
	}
	if list := mustRun(t, d, `{"action":"list","tool":"computer_run"}`); !strings.Contains(list, " computer_run (on lab-mini), until 2027-02-26: ") ||
		!strings.Contains(list, " computer_run (agy only), until ") {
		t.Fatalf("list = %q", list)
	}
}

// One tool's description gains at most 3 notes, the newest, in at most 600 bytes: a note that does not fit what is
// left is skipped for an older one that does.
func TestBlockKeepsTheNewestWithinItsCaps(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	for i := range 4 {
		mustRun(t, d, fmt.Sprintf(`{"action":"propose","tool":"todo_create","text":"Short tip number %d."}`, i))
		f.set(func(f *fake) { f.now = f.now.Add(time.Minute) })
	}
	desc := descriptions(d, f)["todo_create"]
	if strings.Count(desc, "\n- ") != MaxPerTool || strings.Contains(desc, "number 0") || !strings.Contains(desc, "Field notes:\n- Short tip number 3.") {
		t.Fatalf("description = %q", desc)
	}
	long := strings.Repeat("word ", 38) + "end." // 194 characters, 196 with its number
	for i := range 3 {
		mustRun(t, d, fmt.Sprintf(`{"action":"propose","tool":"agent_send","text":"%d %s"}`, i, long))
		f.set(func(f *fake) { f.now = f.now.Add(time.Minute) })
	}
	block, _ := strings.CutPrefix(descriptions(d, f)["agent_send"], "Sends a message to an agent.\n\n")
	if len(block) > MaxBlock || strings.Count(block, "\n- ") != 2 || !strings.Contains(block, "- 2 word") || strings.Contains(block, "- 0 word") {
		t.Fatalf("block of %d bytes = %q", len(block), block)
	}
}

// Past its review date a note leaves the descriptions and is listed as due (and counted for the snapshot); Sweep tells
// the watches once. Proposed again and approved, it is back and replaces the due one.
func TestDueNoteLeavesAndIsProposedAgain(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read the agent first."}`)
	old := held(d)[0].ID
	ctx := context.Background()
	d.Sweep(ctx)
	told := f.notifications()
	f.set(func(f *fake) { f.now = f.now.Add((ReviewDays + 1) * day) })
	if desc := descriptions(d, f)["agent_send"]; strings.Contains(desc, "Field notes") {
		t.Fatalf("a note past its review date is shown: %q", desc)
	}
	if d.Revision(ctx) != "" {
		t.Fatal("the revision still carries the note")
	}
	if _, due := d.Waiting(ctx); due != 1 {
		t.Fatalf("due = %d", due)
	}
	if list := mustRun(t, d, `{"action":"list"}`); !strings.Contains(list, "Due for review") || !strings.Contains(list, old+" agent_send, due since 2027-02-26") {
		t.Fatalf("list = %q", list)
	}
	d.Sweep(ctx)
	d.Sweep(ctx)
	if f.notifications() != told+1 {
		t.Fatalf("Sweep told the watches %d times, want once", f.notifications()-told)
	}
	mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read the agent first."}`)
	notes := held(d)
	if len(notes) != 2 || notes[0].Status != StatusRetired || notes[0].RetiredReason != "proposed again as "+notes[1].ID || notes[1].Status != StatusApproved {
		t.Fatalf("notes = %+v", notes)
	}
	if desc := descriptions(d, f)["agent_send"]; !strings.Contains(desc, "Read the agent first.") {
		t.Fatalf("the note proposed again is not shown: %q", desc)
	}
	if _, due := d.Waiting(ctx); due != 0 {
		t.Fatalf("due = %d", due)
	}
}

// A release that changes a tool's description retires its notes: they leave the descriptions at once, and Sweep
// stores the retirement and tells the watches once. A tool absent from the list (its extension off) keeps its notes.
func TestNoteRetiresWhenItsToolsDescriptionChanges(t *testing.T) {
	t.Parallel()
	f := newFake()
	d := New(f.host(t))
	mustRun(t, d, `{"action":"propose","tool":"computer_run","text":"Use a script."}`)
	mustRun(t, d, `{"action":"propose","tool":"todo_create","text":"Short titles."}`)
	ctx := context.Background()
	d.Sweep(ctx)
	told := f.notifications()
	f.set(func(f *fake) {
		f.tools[0].Description = "Runs steps on a Mac; launch_app works in a step now."
		f.tools = slices.DeleteFunc(f.tools, func(t proto.Tool) bool { return t.Name == "todo_create" })
	})
	if desc := descriptions(d, f)["computer_run"]; strings.Contains(desc, "Field notes") {
		t.Fatalf("a stale note is shown: %q", desc)
	}
	d.Sweep(ctx)
	d.Sweep(ctx)
	notes := held(d)
	if notes[0].Status != StatusRetired || notes[0].RetiredReason != reasonChanged || notes[1].Status != StatusApproved {
		t.Fatalf("notes = %+v", notes)
	}
	if f.notifications() != told+1 {
		t.Fatalf("Sweep told the watches %d times, want once", f.notifications()-told)
	}
}

// The digest is of the description as Persona wrote it: the same in every language a built-in tool is shown in, and
// another for another description.
func TestDigestIsTheSameInEveryLanguage(t *testing.T) {
	t.Parallel()
	zh, en := prompts.Tool("task_plan"), prompts.Tool("task_plan", "en")
	if zh.Description == en.Description {
		t.Fatal("task_plan reads the same in both languages: pick a tool that does not")
	}
	tool := func(m prompts.MCPTool) proto.Tool {
		return proto.Tool{Name: m.Name, Description: m.Description, InputSchema: m.InputSchema}
	}
	if Digest(tool(zh)) != Digest(tool(en)) {
		t.Fatal("the digest moves with the language")
	}
	changed := tool(zh)
	changed.Description += " More."
	if Digest(changed) == Digest(tool(zh)) {
		t.Fatal("the digest does not move with the description")
	}
}

// A note draft is proposed by its id: the draft gives the tool, the code and the evidence (the call may replace the
// text), and once approved the draft is marked proposed with the note's id. A draft that is not pending is refused.
func TestProposeFromADraft(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.drafts = []Draft{{ID: "fd_1", Kind: DraftNote, Tool: "computer_run", Code: "invalid_batch", Runtime: "agy", Count: 4,
		FirstAt: f.now.Add(-6 * day), LastAt: f.now.Add(-day), Skeleton: "{steps:[{launch_app:{name:str}}]}",
		RetrySkeleton: "{script:str}", Text: "Launch apps from a script.", Status: DraftPending}}
	d := New(f.host(t))
	if list := mustRun(t, d, `{"action":"list","tool":"computer_run"}`); !strings.Contains(list, "- fd_1 note draft for computer_run: invalid_batch 4 times") ||
		!strings.Contains(list, "the retry that worked had {script:str}") {
		t.Fatalf("list = %q", list)
	}
	mustRun(t, d, `{"action":"propose","draft_id":"fd_1","text":"Launch apps with await cua.launch in a script."}`)
	n := held(d)[0]
	if n.Tool != "computer_run" || n.Code != "invalid_batch" || n.DraftID != "fd_1" || n.Status != StatusApproved ||
		n.Text != "Launch apps with await cua.launch in a script." ||
		n.Evidence != "4 failed calls with invalid_batch from 2026-11-22 to 2026-11-27; a retry with other arguments worked" {
		t.Fatalf("note = %+v", n)
	}
	if x := f.drafts[0]; x.Status != DraftProposed || x.NoteID != n.ID {
		t.Fatalf("draft = %+v", x)
	}
	if _, err := run(t, d, `{"action":"propose","draft_id":"fd_1"}`); err == nil || !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("a proposed draft again: %v", err)
	}
	if _, err := run(t, d, `{"action":"propose","draft_id":"fd_9"}`); err == nil || !strings.Contains(err.Error(), `no friction draft "fd_9"`) {
		t.Fatalf("an unknown draft: %v", err)
	}
}

// file_fix makes a fix draft a to-do titled "Fix <tool>: <code>" whose body carries the draft's text, its count and
// dates, the skeletons and links to the conversations, bound to Persona's repository when it is saved (else naming
// it); the draft is marked filed with the to-do. Done, the to-do retires the notes of the same tool and code.
func TestFileFixAndTheNotesItCovers(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.drafts = []Draft{
		{ID: "fd_1", Kind: DraftFix, Tool: "todo_create", Code: "store_unavailable", Count: 3, FirstAt: f.now.Add(-2 * day),
			LastAt: f.now.Add(-day), Skeleton: "{title:str}", RetrySkeleton: "{notes:str,title:str}", Text: "todo_create fails while the store restarts.",
			Evidence: []Evidence{{Machine: "m-studio", Session: "conv-a"}, {Machine: "m-studio", Session: "conv-b"}}, Status: DraftPending},
		{ID: "fd_2", Kind: DraftNote, Tool: "todo_create", Code: "store_unavailable", Count: 3, Text: "Retry once after a minute.", Status: DraftPending},
		{ID: "fd_3", Kind: DraftFix, Tool: "agent_send", Code: "busy", Count: 1, Status: DraftPending},
	}
	d := New(f.host(t))
	mustRun(t, d, `{"action":"propose","draft_id":"fd_2"}`)
	mustRun(t, d, `{"action":"propose","tool":"todo_create","text":"Keep titles short."}`)
	if _, err := run(t, d, `{"action":"file_fix","draft_id":"fd_2"}`); err == nil || !strings.Contains(err.Error(), "not a fix") {
		t.Fatalf("a note draft filed: %v", err)
	}
	out := mustRun(t, d, `{"action":"file_fix","draft_id":"fd_1"}`)
	if out != "Filed as to-do todo_1 “Fix todo_create: store_unavailable” (sunfayao/persona is not saved in Persona: the to-do names it). "+
		"Once it is done, the notes on todo_create for store_unavailable retire." {
		t.Fatalf("file_fix = %q", out)
	}
	todo := f.filed[0]
	for _, want := range []string{"todo_create fails while the store restarts.", "Failed 3 times, first on 2026-11-26, last on 2026-11-27.",
		"Arguments that failed: {title:str}", "Arguments of the retry that worked: {notes:str,title:str}",
		"Conversations where it happened: https://persona.example.com/persona/conv-a https://persona.example.com/persona/conv-b",
		"Repository: sunfayao/persona"} {
		if !strings.Contains(todo.notes, want) {
			t.Errorf("to-do body lacks %q:\n%s", want, todo.notes)
		}
	}
	if todo.title != "Fix todo_create: store_unavailable" || todo.repo != nil {
		t.Fatalf("to-do = %+v", todo)
	}
	if x := f.drafts[0]; x.Status != DraftFiled || x.TodoID != "todo_1" {
		t.Fatalf("draft = %+v", x)
	}
	if out := mustRun(t, d, `{"action":"file_fix","draft_id":"fd_1"}`); out != "Draft fd_1 is filed already, as to-do todo_1." {
		t.Fatalf("again = %q", out)
	}
	// Saved in Persona, the repository is bound instead of named.
	f.set(func(f *fake) { f.repo = &Repository{ID: "repo_1", Revision: 7} })
	if out := mustRun(t, d, `{"action":"file_fix","draft_id":"fd_3"}`); !strings.Contains(out, ", bound to sunfayao/persona.") {
		t.Fatalf("bound = %q", out)
	}
	if got := f.filed[1]; got.repo == nil || got.repo.ID != "repo_1" || strings.Contains(got.notes, "Repository:") {
		t.Fatalf("bound to-do = %+v", got)
	}

	ctx := context.Background()
	d.Sweep(ctx)
	if notes := held(d); notes[0].Status != StatusApproved {
		t.Fatalf("covered while the to-do is open: %+v", notes[0])
	}
	told := f.notifications()
	f.set(func(f *fake) { f.todos["todo_1"] = true })
	d.Sweep(ctx)
	d.Sweep(ctx)
	notes := held(d)
	if notes[0].Status != StatusRetired || notes[0].RetiredReason != "covered: its fix, to-do todo_1, is done" || notes[1].Status != StatusApproved {
		t.Fatalf("notes = %+v", notes)
	}
	if f.notifications() != told+1 {
		t.Fatalf("Sweep told the watches %d times, want once", f.notifications()-told)
	}
	if desc := descriptions(d, f)["todo_create"]; strings.Contains(desc, "Retry once") || !strings.Contains(desc, "Keep titles short.") {
		t.Fatalf("description = %q", desc)
	}
}

// retire takes a note out at once by its id, or sets a pending friction draft aside by its draft_id; it refuses what
// it does not take.
func TestRetireTakesANoteOrADraft(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.drafts = []Draft{{ID: "fd_1", Kind: DraftNote, Tool: "agent_send", Status: DraftPending}}
	d := New(f.host(t))
	if out := mustRun(t, d, `{"action":"retire","draft_id":"fd_1"}`); out != "Set draft fd_1 aside." || f.drafts[0].Status != DraftDismissed {
		t.Fatalf("retire a draft = %q, %+v", out, f.drafts[0])
	}
	if _, err := run(t, d, `{"action":"retire","draft_id":"fd_1"}`); err == nil || !strings.Contains(err.Error(), "not pending") {
		t.Fatalf("set aside twice: %v", err)
	}
	if _, err := run(t, d, `{"action":"retire","draft_id":"fd_7"}`); err == nil || !strings.Contains(err.Error(), "no friction draft") {
		t.Fatalf("unknown draft: %v", err)
	}
	mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read first."}`)
	id := held(d)[0].ID
	told := f.notifications()
	if out := mustRun(t, d, `{"action":"retire","id":"`+id+`"}`); out != "Retired note "+id+": it is out of agent_send's description." {
		t.Fatalf("retire = %q", out)
	}
	if n := held(d)[0]; n.Status != StatusRetired || n.RetiredReason != "retired on request" || f.notifications() != told+1 {
		t.Fatalf("retired = %+v, told %d", n, f.notifications()-told)
	}
	for _, args := range []string{`{"action":"retire"}`, `{"action":"retire","id":"` + id + `","draft_id":"fd_1"}`} {
		if _, err := run(t, d, args); err == nil || !strings.Contains(err.Error(), "retire takes id") {
			t.Errorf("%s: %v", args, err)
		}
	}
	if _, err := run(t, d, `{"action":"retire","id":"`+id+`"}`); err == nil || !strings.Contains(err.Error(), "retired already") {
		t.Fatalf("retired twice: %v", err)
	}
}

// A proposal whose card is gone with the process that showed it (a restart) retires at the next Sweep; one whose card
// is still up does not.
func TestSweepRetiresProposalsWhoseCardIsGone(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.answer = "wait"
	d := New(f.host(t))
	mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read first."}`)
	d.Sweep(context.Background())
	if n := held(d)[0]; n.Status != StatusProposed {
		t.Fatalf("a live proposal retired: %+v", n)
	}
	restarted := New(f.host(t))
	restarted.Sweep(context.Background())
	if n := held(restarted)[0]; n.Status != StatusRetired || n.RetiredReason != reasonRestart {
		t.Fatalf("after a restart: %+v", n)
	}
}

// Waiting counts what tool_note list asks to review: the pending drafts and the notes past their review date.
func TestWaitingCountsDraftsAndDueNotes(t *testing.T) {
	t.Parallel()
	f := newFake()
	f.drafts = []Draft{{ID: "fd_1", Status: DraftPending}, {ID: "fd_2", Status: DraftDismissed}, {ID: "fd_3", Status: DraftPending}}
	d := New(f.host(t))
	if drafts, due := d.Waiting(context.Background()); drafts != 2 || due != 0 {
		t.Fatalf("waiting = %d, %d", drafts, due)
	}
	mustRun(t, d, `{"action":"propose","tool":"agent_send","text":"Read first."}`)
	f.set(func(f *fake) { f.now = f.now.Add((ReviewDays + 1) * day) })
	if drafts, due := d.Waiting(context.Background()); drafts != 2 || due != 1 {
		t.Fatalf("waiting = %d, %d", drafts, due)
	}
}

// Retired notes are kept a while, then dropped: past keepRetired, or the oldest past maxRetired.
func TestPruneDropsOldRetiredNotes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)
	var doc doc
	doc.Notes = append(doc.Notes, Note{ID: "old", Status: StatusRetired, RetiredAt: now.Add(-keepRetired - day)},
		Note{ID: "kept", Status: StatusApproved})
	for i := range maxRetired + 2 {
		doc.Notes = append(doc.Notes, Note{ID: fmt.Sprintf("r%03d", i), Status: StatusRetired, RetiredAt: now.Add(-time.Duration(i) * time.Hour)})
	}
	if !prune(&doc, now) {
		t.Fatal("nothing dropped")
	}
	ids := map[string]bool{}
	for _, n := range doc.Notes {
		ids[n.ID] = true
	}
	if len(doc.Notes) != maxRetired+1 || ids["old"] || !ids["kept"] || ids[fmt.Sprintf("r%03d", maxRetired+1)] || ids[fmt.Sprintf("r%03d", maxRetired)] || !ids["r000"] {
		t.Fatalf("kept %d: %v", len(doc.Notes), ids)
	}
	if prune(&doc, now) {
		t.Fatal("a second prune dropped more")
	}
}
