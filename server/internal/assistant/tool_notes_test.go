package assistant

import (
	"context"
	"encoding/json"
	"github.com/sunfayao/persona/server/internal/assistant/snapshotfit"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/assistant/repositorystore"
	"github.com/sunfayao/persona/server/internal/assistant/toolnotes"
	"github.com/sunfayao/persona/server/internal/assistant/tooluse"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/i18n"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// toolNoteCall starts a tool_note call of the main conversation s1 on mac, as the node sends it; its result comes on
// the channel.
func toolNoteCall(s *Service, args string) <-chan proto.ToolResult {
	done := make(chan proto.ToolResult, 1)
	go func() {
		done <- s.mcpCall(context.Background(), "mac", "s1", "tool_note", json.RawMessage(args), true)
	}()
	return done
}

// storedNotes are the field notes as stored.
func storedNotes(t *testing.T, s *Service) []toolnotes.Note {
	t.Helper()
	var doc struct{ Notes []toolnotes.Note }
	if _, err := s.store().Setting(context.Background(), toolnotes.Key, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Notes
}

// toolNotesFleet is a fleet whose main conversation s1 runs on mac.
func toolNotesFleet(t *testing.T) *fleetTest {
	ft := newFleetTest(t)
	ft.f.hello.Assistant.MachineID = "mac"
	preparedTestConversation(ft)
	return ft
}

// A friction draft proposed as a field note applies directly without a card: the note reaches the assistant
// conversations' tool list at once, tool watches are woken, the list's revision moves, and the draft is marked
// proposed with the note's id. An agent's list gets no notes.
func TestToolNoteApprovedReachesTheToolListAndItsWatch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ft := toolNotesFleet(t)
		s, ctx := ft.s, context.Background()
		if err := s.toolUse.PutDrafts(ctx, []tooluse.Draft{{ID: "fd_1", Kind: tooluse.DraftNote, Tool: "agent_send", Code: "busy",
			Text: "Wait for the agent to finish.", Status: tooluse.DraftPending}}); err != nil {
			t.Fatal(err)
		}
		base, changed := s.mcpChanges.snapshot()
		before := s.toolsCursor(ctx, "mac", "s1", true, base)
		res := s.mcpCall(ctx, "mac", "s1", "tool_note", json.RawMessage(`{"action":"propose","draft_id":"fd_1","text":"Read the agent before sending it more work."}`), true)
		if res.IsError || !strings.HasPrefix(res.Content[0].Text, "Approved: note tn_") || len(s.pendingQuestions()) != 0 {
			t.Fatalf("result = %+v, pending = %+v", res, s.pendingQuestions())
		}
		select {
		case <-changed:
		default:
			t.Fatal("the tool watches were not woken")
		}
		if s.toolsCursor(ctx, "mac", "s1", true, base) == before {
			t.Fatal("the tool-list revision did not move")
		}
		desc := mcpToolList(t, s, "s1", true, false)["agent_send"].Description
		if !strings.HasSuffix(desc, "\n\nField notes:\n- Read the agent before sending it more work.") {
			t.Fatalf("agent_send = %q", desc)
		}
		drafts, _ := s.toolUse.Drafts(ctx)
		if len(drafts) != 1 || drafts[0].Status != tooluse.DraftProposed || !strings.HasPrefix(drafts[0].NoteID, "tn_") {
			t.Fatalf("drafts = %+v", drafts)
		}
		plain := []proto.Tool{{Name: "agent_send", Description: "Sends."}}
		if got := s.withToolNotes(ctx, plain, false); got[0].Description != "Sends." || s.toolNotesRevision(ctx, false) != "" {
			t.Fatalf("an agent's list has notes: %+v", got)
		}
		synctest.Wait()
	})
}

// The user's computers' names, ids and ssh hosts, as the hub lists them, are refused in a note before any card.
func TestToolNoteRefusesTheUsersComputerNames(t *testing.T) {
	f := newFakeHub().withMachine()
	f.devices = []Device{{ID: "studio-7", Name: "Design Studio", SSHHost: "render-box.example.com"}}
	s := newTestService(t, f)
	ctx := context.Background()
	for text, scrubbed := range map[string]string{
		"Only Design Studio has the fonts.":        "Only <machine> has the fonts.",
		"Copy them to render-box first.":           "Copy them to <machine> first.",
		"studio-7 runs the renders; ask it first.": "<machine> runs the renders; ask it first.",
	} {
		args, _ := json.Marshal(map[string]string{"action": "propose", "tool": "agent_send", "text": text})
		_, err := s.toolNote(ctx, "server", "c1", args)
		if err == nil || !strings.Contains(err.Error(), "not proposed") || !strings.Contains(err.Error(), scrubbed) {
			t.Errorf("%q: %v, want a refusal showing %q", text, err, scrubbed)
		}
	}
	if len(s.pendingQuestions()) != 0 {
		t.Fatal("a card was shown")
	}
}

// file_fix makes a fix draft a to-do the way todo_create does, titled "Fix <tool>: <code>" in the user's language,
// bound to sunfayao/persona when it is saved in Persona, with the draft's evidence as conversation links; the draft is
// filed with it. Once the to-do is done, the next sweep retires the notes of its tool and code, and they leave the
// tool list.
func TestToolNoteFileFixMakesTheTodoAndItsDoneRetiresTheNote(t *testing.T) {
	st := repositorystore.New(todoTestDB(t))
	s := newServiceOn(t, newFakeHub().withMachine(), st)
	ctx := context.Background()
	catalog := repositoryDocument{Revision: 3, Repositories: []RepositoryBinding{{ID: "repo_persona", VerifiedAt: time.Now().UTC(),
		Identity: repository.Identity{Provider: "github", InstanceURL: "https://github.com", Path: "sunfayao/persona",
			CanonicalURL: "https://github.com/sunfayao/persona"}}}}
	if err := st.PutSetting(ctx, repositoriesKey, catalog); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Add(-time.Hour)
	if err := s.toolUse.PutDrafts(ctx, []tooluse.Draft{{ID: "fd_1", Kind: tooluse.DraftFix, Tool: "todo_create", Code: "store_unavailable",
		Count: 2, FirstAt: at, LastAt: at, Skeleton: "{title:str}", Text: "todo_create fails while the store restarts.",
		Evidence: []tooluse.Evidence{{Machine: "server", Session: "c1", At: at}}, Status: tooluse.DraftPending}}); err != nil {
		t.Fatal(err)
	}
	tool := toolsNamed([]string{"todo_create"})[0]
	note := toolnotes.Note{ID: "tn_1", Tool: "todo_create", Text: "Retry once after a minute.", Code: "store_unavailable", By: "c1",
		Status: toolnotes.StatusApproved, PromptDigest: toolnotes.Digest(tool), CreatedAt: at, ApprovedAt: at, ReviewAt: at.Add(90 * 24 * time.Hour)}
	if err := st.PutSetting(ctx, toolnotes.Key, map[string]any{"notes": []toolnotes.Note{note}}); err != nil {
		t.Fatal(err)
	}
	described := func() string {
		for _, t := range s.withToolNotes(ctx, s.mcpTools(ctx, true), true) {
			if t.Name == "todo_create" {
				return t.Description
			}
		}
		return ""
	}
	if !strings.HasSuffix(described(), "Field notes:\n- Retry once after a minute.") {
		t.Fatalf("todo_create = %q", described())
	}

	res := s.Tool(ctx, ToolCall{Name: "tool_note", MachineID: "server", SessionID: "c1", Arguments: json.RawMessage(`{"action":"file_fix","draft_id":"fd_1"}`)})
	if res.IsError || !strings.Contains(res.Content[0].Text, ", bound to sunfayao/persona") {
		t.Fatalf("file_fix = %+v", res)
	}
	drafts, _ := s.toolUse.Drafts(ctx)
	if len(drafts) != 1 || drafts[0].Status != tooluse.DraftFiled || drafts[0].TodoID == "" {
		t.Fatalf("drafts = %+v", drafts)
	}
	id := drafts[0].TodoID
	todo := todoNow(s, id)
	if want := i18n.T(s.lang(ctx), "assistant-tool_notes.fix_title", i18n.Args{"tool": "todo_create", "code": "store_unavailable"}); todo.Title != want {
		t.Fatalf("title = %q, want %q", todo.Title, want)
	}
	for _, want := range []string{"todo_create fails while the store restarts.", "{title:str}", "/persona/c1"} {
		if !strings.Contains(todo.Notes, want) {
			t.Errorf("notes lack %q:\n%s", want, todo.Notes)
		}
	}
	set, err := s.savedTodoRepositories(ctx, id)
	if err != nil || len(set.Repositories) != 1 || set.Repositories[0].RepoID != "repo_persona" {
		t.Fatalf("repositories = %+v, %v", set, err)
	}

	s.toolNotes.Sweep(ctx)
	if !strings.Contains(described(), "Retry once after a minute.") {
		t.Fatal("the note retired while its fix is open")
	}
	done := true
	if _, err := s.editTodo(ctx, id, db.TodoEdit{Done: &done}); err != nil {
		t.Fatal(err)
	}
	s.toolNotes.Sweep(ctx)
	if strings.Contains(described(), "Field notes") {
		t.Fatalf("a covered note is shown: %q", described())
	}
	if notes := storedNotes(t, s); len(notes) != 1 || notes[0].RetiredReason != "covered: its fix, to-do "+id+", is done" {
		t.Fatalf("notes = %+v", notes)
	}
}

// The main conversation's snapshot says what waits for review in tool_note list, only while something does; another
// conversation's and the background supervisor's do not.
func TestMainSnapshotSaysWhatWaitsForToolReview(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	ctx := context.Background()
	snapshot := func() prompts.Snapshot {
		done := make(chan struct{})
		s.takeSnapshot(done)
		<-done
		return *s.live.snap
	}
	if snap := snapshot(); snap.ToolReview != nil {
		t.Fatalf("nothing waits, yet %+v", snap.ToolReview)
	}
	if err := s.toolUse.PutDrafts(ctx, []tooluse.Draft{{ID: "fd_1", Status: tooluse.DraftPending}, {ID: "fd_2", Status: tooluse.DraftDismissed},
		{ID: "fd_3", Status: tooluse.DraftPending}}); err != nil {
		t.Fatal(err)
	}
	snap := snapshot()
	if snap.ToolReview == nil || *snap.ToolReview != (prompts.SnapshotToolReview{Drafts: 2}) {
		t.Fatalf("tool review = %+v", snap.ToolReview)
	}
	const line = "- 2 friction drafts and 0 field notes due wait for review (tool_note list)"
	if main := snapshotfit.Fit(snapshotfit.Scope(snap, "")); !strings.Contains(main, line) {
		t.Fatalf("main snapshot lacks %q:\n%s", line, main)
	}
	for name, other := range map[string]prompts.Snapshot{"another conversation": snapshotfit.Scope(snap, "c2"),
		"the background supervisor": snapshotfit.Background(snapshotfit.Scope(snap, ""))} {
		if text := snapshotfit.Fit(other); strings.Contains(text, "tool_note list") {
			t.Errorf("%s's snapshot has the line:\n%s", name, text)
		}
	}
}

// The field notes read the friction drafts' kinds and states by the values the tool record stores.
func TestToolNotesReadTheToolRecordsDraftStates(t *testing.T) {
	for _, pair := range [][2]string{{toolnotes.DraftNote, tooluse.DraftNote}, {toolnotes.DraftFix, tooluse.DraftFix},
		{toolnotes.DraftPending, tooluse.DraftPending}, {toolnotes.DraftProposed, tooluse.DraftProposed},
		{toolnotes.DraftDismissed, tooluse.DraftDismissed}, {toolnotes.DraftFiled, tooluse.DraftFiled}} {
		if pair[0] != pair[1] {
			t.Errorf("toolnotes has %q where tooluse stores %q", pair[0], pair[1])
		}
	}
}

func TestGoldenToolFieldNotes(t *testing.T) {
	golden(t, "tool_field_notes", prompts.RenderToolFieldNotes([]prompts.FieldNote{
		{Text: "Open apps with a script that calls await cua.launch, never with a launch_app step."},
		{Machine: "box-1", Text: "Its screen is small: read the window before clicking."},
	}))
}

func TestGoldenSnapshotToolReview(t *testing.T) {
	golden(t, "assistant_snapshot.tool_review", prompts.RenderSnapshot(prompts.Snapshot{Now: time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC),
		Zone: "UTC", ToolReview: &prompts.SnapshotToolReview{Drafts: 2, NotesDue: 1}}))
}
