package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/autosupervise"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// policyService is a service on a computer whose node delivers one-off blocks, with a main conversation (ruleMain) that
// supervises nothing itself, in the supervisor Agents' mode.
func policyService(t *testing.T) *Service {
	t.Helper()
	s, _ := ruleService(t, &memStore{})
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = autosupervise.Auto })
	return s
}

// superviseHere makes the main conversation the supervisor of an open to-do.
func superviseHere(t *testing.T, s *Service) {
	t.Helper()
	if err := s.ensureTodosLoaded(context.Background()); err != nil {
		t.Fatal(err)
	}
	s.todos.mu.Lock()
	s.todos.todos["todo_own"] = &Todo{ID: "todo_own", Title: "Fix login", Supervision: &Supervision{Supervisor: ruleMain}}
	s.todos.mu.Unlock()
}

// startMain gives the main conversation a start, as the manager does, and returns the policy that start was given.
func startMain(t *testing.T, s *Service) string {
	t.Helper()
	ctx := context.WithValue(context.Background(), supervisionStartup{}, true)
	p := map[string]any{}
	if err := s.putSupervisionInstructions(ctx, "server", "", p); err != nil {
		t.Fatal(err)
	}
	s.noteStartupPolicy(ctx, "server", ruleMain, p, nil)
	return p["supervision_instructions"].(string)
}

func standing(t *testing.T, s *Service) string {
	t.Helper()
	text, err := s.supervisionInstructions(context.Background(), "server", ruleMain)
	if err != nil {
		t.Fatal(err)
	}
	return text
}

// A main conversation that supervises no open to-do itself gets no policy in its standing text when it starts; with the
// setting on explicit, or when it supervises to-dos itself, the policy stays there; a conversation already live keeps
// what it has.
func TestMainWithoutSupervisedTodoGetsNoPolicyFile(t *testing.T) {
	full := prompts.RenderSupervisionInstructions()
	s := policyService(t)
	if got := standing(t, s); got != full {
		t.Fatalf("a conversation that was live before this book keeps its policy: %d bytes", len(got))
	}
	if got := startMain(t, s); got != "" {
		t.Fatalf("a new main conversation that supervises nothing starts with %d bytes of policy", len(got))
	}
	if got := standing(t, s); got != "" {
		t.Fatalf("its standing text carries %d bytes of policy", len(got))
	}

	explicit := policyService(t)
	setSettings(t, explicit, func(v *Settings) { v.TodoSupervisor = autosupervise.Explicit })
	if got := startMain(t, explicit); got != full {
		t.Fatalf("explicit: the start carries %d bytes of policy, want %d", len(got), len(full))
	}
	if got := standing(t, explicit); got != full {
		t.Fatalf("explicit: the standing text carries %d bytes of policy", len(got))
	}

	own := policyService(t)
	setSettings(t, own, func(v *Settings) { yes := true; v.MainSupervisesOwnTasks = &yes })
	if got := startMain(t, own); got != full {
		t.Fatalf("a main conversation that follows tasks up itself starts with %d bytes of policy", len(got))
	}

	plain, _ := ruleService(t, &memStore{})
	plain.devs = newFleetDevs() // its node does not announce the one-off block
	plain.devs.(*fleetDevs).add("server", "server")
	setSettings(t, plain, func(v *Settings) { v.TodoSupervisor = autosupervise.Auto })
	if got := startMain(t, plain); got != full {
		t.Fatalf("a node without the capability starts with %d bytes of policy", len(got))
	}
}

// The standing text never gains or loses the policy under a live conversation: a to-do it supervises later, or a change of
// the setting, change nothing until it is compacted.
func TestStandingPolicyDoesNotChangeUnderALiveConversation(t *testing.T) {
	ctx := context.Background()
	s := policyService(t)
	superviseHere(t, s)                             // a to-do it supervises, since it started
	s.policy.Started(ctx, "server", ruleMain, true) // it started when it supervised nothing
	if got := standing(t, s); got != "" {
		t.Fatalf("a live conversation that started without the policy gained it: %d bytes", len(got))
	}
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = autosupervise.Explicit })
	if got := standing(t, s); got != "" {
		t.Fatalf("a change of the setting gave a live conversation the policy: %d bytes", len(got))
	}
	setSettings(t, s, func(v *Settings) { v.TodoSupervisor = autosupervise.Auto })

	// Compacted: the next start decides again, and it supervises a to-do now.
	s.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server", RecoveryID: "recovery-1"})
	if got := standing(t, s); got != prompts.RenderSupervisionInstructions() {
		t.Fatalf("after a compaction it supervises a to-do: %d bytes of policy", len(got))
	}
}

// A conversation with no policy in its standing text gets it once, with the first signal it is asked to judge, as a
// one-off block; it is repeated while the node did not confirm it, and after a compaction; not for a message that asks
// nothing; and a supervision message carries no snapshot while an ordinary one does.
func TestPolicyTravelsOnceWithTheFirstJudgedSignal(t *testing.T) {
	ctx := context.Background()
	s := policyService(t)
	startMain(t, s)
	ask := func(recovery string) ContextResult {
		return s.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server", RecoveryID: recovery})
	}
	if out := ask(""); out.Text == "" || out.RuleNotice != "" {
		t.Fatalf("an ordinary message: text %q, notice %q", out.Text, out.RuleNotice)
	}

	s.digests.Sending(ruleMain, "a routine summary", false)
	if out := ask(""); out.Text != "" || out.RuleNotice != "" {
		t.Fatalf("a message that judges nothing: text %q, notice %d bytes", out.Text, len(out.RuleNotice))
	}
	s.digests.Done(ruleMain, "a routine summary", true)

	s.digests.Sending(ruleMain, "the first judged signal", true)
	out := ask("")
	if out.Text != "" {
		t.Fatalf("a supervision message carries the snapshot: %q", out.Text)
	}
	if !strings.Contains(out.RuleNotice, `<persona-context kind="supervision-policy">`) || !strings.Contains(out.RuleNotice, prompts.RenderSupervisionInstructions()) ||
		len(out.Receipts) != 1 || out.Receipts[0].ItemID != "supervision_policy" {
		t.Fatalf("the policy block:\n%s\nreceipts %v", out.RuleNotice, out.Receipts)
	}
	if again := ask(""); !strings.Contains(again.RuleNotice, `kind="supervision-policy"`) {
		t.Fatal("a message the node did not confirm must carry the block again")
	}
	s.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server", Ack: out.Receipts})
	if again := ask(""); strings.Contains(again.RuleNotice, "supervision-policy") || again.Text != "" {
		t.Fatalf("told twice: %q", again.RuleNotice)
	}
	s.digests.Done(ruleMain, "the first judged signal", true)

	// Compacted: the next judged signal brings it again, once.
	s.digests.Sending(ruleMain, "the next judged signal", true)
	if compacted := ask("recovery-1"); !strings.Contains(compacted.RuleNotice, "supervision-policy") {
		t.Fatalf("after a compaction the policy comes again: %q", compacted.RuleNotice)
	}
	s.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server", Ack: []assistantwire.ViewErrorReceipt{{ItemID: "supervision_policy", Digest: "1"}}})
	if again := ask(""); strings.Contains(again.RuleNotice, "supervision-policy") {
		t.Fatal("told twice after the compaction")
	}
	s.digests.Done(ruleMain, "the next judged signal", true)

	// A conversation that carries the policy in its standing text is never sent it.
	own := policyService(t)
	setSettings(t, own, func(v *Settings) { v.TodoSupervisor = autosupervise.Explicit })
	startMain(t, own)
	own.digests.Sending(ruleMain, "a judged signal", true)
	if got := own.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server"}); got.RuleNotice != "" {
		t.Fatalf("a conversation with the policy in its standing text was sent it again: %d bytes", len(got.RuleNotice))
	}
}

// A conversation is told only what changed of a to-do once a message went in, and told it whole again after a compaction.
func TestSupervisionMessagesTellOnlyWhatChanged(t *testing.T) {
	ctx := context.Background()
	s := policyService(t)
	entry := func(last string) []prompts.SuperviseEntry {
		return []prompts.SuperviseEntry{{ID: "todo_1", Title: "Fix login", Kind: "review", Last: last,
			Criteria: []prompts.SuperviseCriterion{{Text: "the PR is merged", State: "pending"}, {Text: "a screenshot is attached", State: "pending"}}}}
	}
	first := s.renderQueuedDigest(ctx, ruleMain, entry("first words"))
	if !strings.Contains(first, "the PR is merged") || strings.Contains(first, "unchanged") {
		t.Fatalf("the first message:\n%s", first)
	}
	if again := s.renderQueuedDigest(ctx, ruleMain, entry("first words")); again != first {
		t.Fatalf("a message that was only rendered counts as told:\n%s", again)
	}
	s.digests.Sending(ruleMain, first, true)
	s.digests.Done(ruleMain, first, true)

	next := s.renderQueuedDigest(ctx, ruleMain, entry("new words"))
	if strings.Contains(next, "the PR is merged") || !strings.Contains(next, "new words") || !strings.Contains(next, "unchanged since the previous message") {
		t.Fatalf("the follow-up:\n%s", next)
	}
	s.digests.Sending(ruleMain, next, true)
	out := s.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server", RecoveryID: "recovery-1"}) // compacted meanwhile
	if !strings.Contains(out.RuleNotice, `kind="supervision-digest"`) || !strings.Contains(out.RuleNotice, "the PR is merged") {
		t.Fatalf("a follow-up for a compacted conversation does not bring the whole block:\n%s", out.RuleNotice)
	}
	s.digests.Done(ruleMain, next, true)
	if whole := s.renderQueuedDigest(ctx, ruleMain, entry("new words")); !strings.Contains(whole, "unchanged") {
		t.Fatalf("the message that carried the whole block counts as told:\n%s", whole)
	}
	s.Context(ctx, ContextParams{SessionID: ruleMain, MachineID: "server", RecoveryID: "recovery-2"}) // compacted, no message on its way
	if whole := s.renderQueuedDigest(ctx, ruleMain, entry("new words")); !strings.Contains(whole, "the PR is merged") {
		t.Fatalf("after a compaction the block is whole again:\n%s", whole)
	}
}

// A to-do its supervisor Agent runs is one line of the snapshot: its id, title, state and supervisor, with none of its
// Agents' steps or words; one an Agent of the conversation's own works on keeps its row.
func TestSnapshotLineOfAToDoItsSupervisorAgentRuns(t *testing.T) {
	now := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	led := Todo{ID: "todo_led", Title: "Rebuild the report page", Agents: []TodoAgent{{MachineID: "box", SessionID: "lead0001-aaaa", Source: sourceLeader}},
		Supervision: &Supervision{Supervisor: leaderRef("box", "lead0001-aaaa"), DoneWhen: []string{"the page renders"}}}
	own := Todo{ID: "todo_own", Title: "Fix the login crash", Agents: []TodoAgent{{MachineID: "box", SessionID: "work0001-bbbb"}},
		Supervision: &Supervision{Supervisor: "conv1", DoneWhen: []string{"the PR is merged"}}}
	worker := proto.Session{SessionID: "work0001-bbbb", MachineID: "box", State: proto.StateWorking, LastText: strings.Repeat("running the tests and reading the output of every one of them ", 4),
		Activity: "go test ./...", Subagents: []proto.Subagent{{Running: true, Description: "read the diff"}}}
	tasks := []prompts.SnapshotTask{snapshotTask(led, "working", []proto.Session{worker}, now), snapshotTask(own, "working", []proto.Session{worker}, now)}
	if tasks[0].Led != "lead0001" || tasks[0].Session != "" || tasks[0].Last != "" || tasks[0].Doing != "" || len(tasks[0].Subagents) != 0 {
		t.Fatalf("a to-do its supervisor Agent runs is not one line: %+v", tasks[0])
	}
	if tasks[1].Led != "" || tasks[1].Session != "work0001" || len(tasks[1].Last) > 64 {
		t.Fatalf("a to-do of the conversation's own: %+v", tasks[1])
	}
	golden(t, "assistant_snapshot.led", prompts.RenderSnapshot(prompts.Snapshot{Now: now, Zone: "UTC", Tasks: tasks}))
}
