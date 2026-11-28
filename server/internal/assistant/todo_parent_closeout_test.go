package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/parentcloseout"
	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/prompts"
)

func dbTodoDoneEdit(done bool) db.TodoEdit {
	return db.TodoEdit{Done: &done}
}

func pendingParentCloseout(s *Service, parentID string) (superviseSignal, bool) {
	b := &s.supervise
	b.mu.Lock()
	defer b.mu.Unlock()
	g, ok := b.pending[(superviseSignal{Todo: parentID, Kind: sigParentCloseout}).id()]
	return g, ok
}

// closeoutClock gives the closeout rules a clock that stands at a fixed moment plus the returned offset.
func closeoutClock(s *Service) *time.Duration {
	base, off := time.Now(), new(time.Duration)
	s.todos.closeoutNow = func() time.Time { return base.Add(*off) }
	return off
}

// withOwnCriteria gives to-do id a supervision ledger, so that it is no request container.
func withOwnCriteria(s *Service, id string) {
	s.todos.mu.Lock()
	s.todos.todos[id].Supervision = &Supervision{Supervisor: "conv1", DoneWhen: []string{"the whole request works"}}
	s.todos.mu.Unlock()
}

func TestGoldenParentCloseoutWakeLanguages(t *testing.T) {
	children := []prompts.ParentCloseoutChild{
		{ID: "todo_child_1", Title: "Implement API", PRs: []string{"https://github.com/example/app/pull/101"}},
		{ID: "todo_child_2", Title: "Add UI tests", PRs: []string{"https://github.com/example/app/pull/102"}},
	}
	for _, locale := range []string{"zh-Hans", "en", "ja"} {
		container := prompts.RenderParentCloseoutWake(prompts.ParentCloseoutWake{
			Language: answerLanguage(locale),
			Children: children,
			Notes:    "Track API + UI rollout",
		})
		for _, want := range []string{"todo_child_1", "todo_child_2", "pull/101", "pull/102", "todo_update state done"} {
			if !strings.Contains(container, want) {
				t.Fatalf("%s container wake missing %q:\n%s", locale, want, container)
			}
		}
		golden(t, "parent_closeout_wake.container."+locale, container)
	}
}

func TestParentCloseoutHooksAndPartialCompletion(t *testing.T) {
	for _, hook := range []string{"todo_complete", "task_review_accepted", "auto_done"} {
		t.Run(hook, func(t *testing.T) {
			st := &memStore{}
			s, f := newTodoReadyService(t, st)
			s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
			ctx := context.Background()

			off := closeoutClock(s)
			parent := createRequestAndPiece(t, s, "conv1", "Ship auth refresh", "Parent container", "", nil)
			withOwnCriteria(s, parent.ID)
			child1 := createRequestAndPiece(t, s, "conv1", "Backend token refresh", "", parent.ID, nil)
			child2 := createRequestAndPiece(t, s, "conv1", "Client retry wiring", "", parent.ID, nil)
			const pr1 = "https://github.com/example/app/pull/201"
			const pr2 = "https://github.com/example/app/pull/202"
			s.setTaskStage(ctx, child1.ID, "merge", pr1)
			s.setTaskStage(ctx, child2.ID, "merge", pr2)
			if err := s.applyPRStatus(ctx, todoNow(s, child1.ID), TodoPRStatus{URL: pr1, State: "merged", CheckedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}
			if err := s.applyPRStatus(ctx, todoNow(s, child2.ID), TodoPRStatus{URL: pr2, State: "merged", CheckedAt: time.Now()}); err != nil {
				t.Fatal(err)
			}

			complete := func(id string) {
				t.Helper()
				switch hook {
				case "todo_complete":
					if _, err := s.editTodo(ctx, id, dbTodoDoneEdit(true)); err != nil {
						t.Fatal(err)
					}
				case "task_review_accepted":
					if closed, err := s.reviewDoneTodo(ctx, id, "Verified", true); err != nil || !closed {
						t.Fatalf("reviewDoneTodo(%s): closed=%v err=%v", id, closed, err)
					}
				case "auto_done":
					if ok, err := s.autoDoneTodo(ctx, id, "Done", time.Now().Add(-time.Minute)); err != nil || !ok {
						t.Fatalf("autoDoneTodo(%s): ok=%v err=%v", id, ok, err)
					}
				}
			}

			// Completing only child 1 (partial completion) must NOT wake the parent.
			complete(child1.ID)
			if _, ok := pendingParentCloseout(s, parent.ID); ok {
				t.Fatal("partial completion woke the parent prematurely")
			}

			// Completing child 2 completes all part_of children -> queues exactly one parent_closeout wake.
			complete(child2.ID)
			if _, ok := pendingParentCloseout(s, parent.ID); ok {
				t.Fatal("the wake went out inside the settle window")
			}
			*off = 2 * time.Minute
			s.sweepParentCloseout(ctx)
			g, ok := pendingParentCloseout(s, parent.ID)
			if !ok {
				t.Fatal("completing last part_of child did not queue sigParentCloseout")
			}
			if !g.urgent() || !supervisionBatchImmediate(g) {
				t.Fatalf("sigParentCloseout should be urgent and batch-immediate: %+v", g)
			}

			// Parent itself remains open (never silently auto-closed).
			if got := todoNow(s, parent.ID); got.DoneAt != nil {
				t.Fatal("parent to-do was silently auto-completed")
			}

			sendPendingPRConflicts(s)
			notices := sendsToConversation(f, "conv1")
			if len(notices) != 1 {
				t.Fatalf("got %d notices to main, want 1: %q", len(notices), notices)
			}
			for _, want := range []string{"signal=" + sigParentCloseout, parent.ID, child1.Title, child2.Title, pr1, pr2, "todo_update state done"} {
				if !strings.Contains(notices[0], want) {
					t.Errorf("parent closeout notice missing %q:\n%s", want, notices[0])
				}
			}
		})
	}
}

func TestParentCloseoutSkipsActiveAgentAndCancelledOrFailedChildren(t *testing.T) {
	t.Run("parent with active agent is skipped", func(t *testing.T) {
		s, _ := newTodoReadyService(t, &memStore{})
		s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
		ctx := context.Background()

		*closeoutClock(s) = time.Hour
		parent := createRequestAndPiece(t, s, "conv1", "Parent with active agent", "", "", nil)
		child := createRequestAndPiece(t, s, "conv1", "Subtask", "", parent.ID, nil)
		s.todos.mu.Lock()
		s.todos.todos[parent.ID].Agents = []TodoAgent{{MachineID: "box", SessionID: "sess-1", LinkedAt: time.Now()}}
		s.todos.mu.Unlock()

		if _, err := s.editTodo(ctx, child.ID, dbTodoDoneEdit(true)); err != nil {
			t.Fatal(err)
		}
		if _, ok := pendingParentCloseout(s, parent.ID); ok {
			t.Fatal("parent with an active Agent unexpectedly queued sigParentCloseout")
		}
	})

	t.Run("failed-start child does not trigger parent closeout", func(t *testing.T) {
		for _, mode := range []string{"failed_start"} {
			t.Run(mode, func(t *testing.T) {
				s, _ := newTodoReadyService(t, &memStore{})
				s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
				*closeoutClock(s) = time.Hour
				ctx := context.Background()

				parent := createRequestAndPiece(t, s, "conv1", "Parent", "", "", nil)
				c1 := createRequestAndPiece(t, s, "conv1", "Done child", "", parent.ID, nil)
				c2 := createRequestAndPiece(t, s, "conv1", "Bad child", "", parent.ID, nil)

				if _, err := s.editTodo(ctx, c1.ID, dbTodoDoneEdit(true)); err != nil {
					t.Fatal(err)
				}
				now := time.Now().UTC()
				s.todos.mu.Lock()
				s.todos.todos[c2.ID].DoneAt = &now
				s.todos.todos[c2.ID].StartError = "machine offline"
				s.todos.mu.Unlock()
				s.sweepParentCloseout(ctx)
				if _, ok := pendingParentCloseout(s, parent.ID); ok {
					t.Fatalf("%s child unexpectedly triggered sigParentCloseout", mode)
				}
			})
		}
	})

	t.Run("synthetic ask or outside-decision rows do not block or trigger parent closeout", func(t *testing.T) {
		s, _ := newTodoReadyService(t, &memStore{})
		s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
		ctx := context.Background()

		*closeoutClock(s) = time.Hour
		parent := createRequestAndPiece(t, s, "conv1", "Parent with ask child", "", "", nil)
		withOwnCriteria(s, parent.ID)
		child := createRequestAndPiece(t, s, "conv1", "Real child", "", parent.ID, nil)
		s.todos.mu.Lock()
		s.todos.todos[askTodoPrefix+"1"] = &Todo{ID: askTodoPrefix + "1", Title: "Pending question", PartOf: parent.ID}
		s.todos.todos["todo_od_1"] = &Todo{ID: "todo_od_1", Title: "Outside decision", PartOf: parent.ID}
		s.todos.mu.Unlock()

		if _, err := s.editTodo(ctx, child.ID, dbTodoDoneEdit(true)); err != nil {
			t.Fatal(err)
		}
		s.sweepParentCloseout(ctx)
		if _, ok := pendingParentCloseout(s, parent.ID); !ok {
			t.Fatal("synthetic ask/outside-decision rows blocked parent closeout wake")
		}
	})
}

func TestParentCloseoutIdempotentAcrossSweepsAndRestart(t *testing.T) {
	st := &memStore{}
	s1, f1 := newTodoReadyService(t, st)
	s1.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	ctx := context.Background()

	*closeoutClock(s1) = time.Hour
	parent := createRequestAndPiece(t, s1, "conv1", "Container parent", "", "", nil)
	withOwnCriteria(s1, parent.ID)
	c1 := createRequestAndPiece(t, s1, "conv1", "Child 1", "", parent.ID, nil)
	if _, err := s1.editTodo(ctx, c1.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	s1.sweepParentCloseout(ctx)
	sendPendingPRConflicts(s1)
	if got := sendsToConversation(f1, "conv1"); len(got) != 1 || !hasSignal(got[0], sigParentCloseout) {
		t.Fatalf("expected 1 parent_closeout wake, got %q", got)
	}

	// Verify durable wake marker was persisted to the store.
	markers := map[string]parentcloseout.Marker{}
	if found, err := st.Setting(ctx, parentcloseout.SettingKey, &markers); err != nil || !found || markers[parent.ID].Key == "" {
		t.Fatalf("expected persisted parent_closeout_wakes marker: found=%v err=%v markers=%+v", found, err, markers)
	}

	// Repeated sweep on the same service must not duplicate.
	s1.sweepParentCloseout(ctx)
	sendPendingPRConflicts(s1)
	if got := sendsToConversation(f1, "conv1"); len(got) != 1 {
		t.Fatalf("repeated sweep duplicated parent_closeout wake: %d", len(got))
	}

	// Restarted service sharing the same store and reloaded todos must not re-emit for the same child set.
	s2, f2 := newTodoReadyService(t, st)
	s2.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	*closeoutClock(s2) = time.Hour
	parentCopy, c1Copy := todoNow(s1, parent.ID), todoNow(s1, c1.ID)
	s2.todos.mu.Lock()
	s2.todos.loaded = true
	s2.todos.todos = map[string]*Todo{
		parent.ID: &parentCopy,
		c1.ID:     &c1Copy,
	}
	s2.todos.mu.Unlock()
	s2.sweepParentCloseout(ctx)
	sendPendingPRConflicts(s2)
	if got := sendsToConversation(f2, "conv1"); len(got) != 0 {
		t.Fatalf("restarted service re-emitted duplicate parent_closeout wake: %q", got)
	}

	// Adding a new child c2 and completing it produces a new canonical child-set key and wakes once more.
	c2 := createRequestAndPiece(t, s2, "conv1", "Child 2", "", parent.ID, nil)
	if _, err := s2.editTodo(ctx, c2.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	s2.sweepParentCloseout(ctx)
	sendPendingPRConflicts(s2)
	if got := sendsToConversation(f2, "conv1"); len(got) != 1 || !strings.Contains(got[0], c2.Title) {
		t.Fatalf("new completed child set did not trigger fresh parent_closeout wake: %q", got)
	}

	// Closing and reopening parent resets the persisted marker so a subsequent sweep can wake if needed.
	if _, err := s2.editTodo(ctx, parent.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	s2.pruneParentCloseoutWakes(ctx)
	markers = map[string]parentcloseout.Marker{}
	if _, err := st.Setting(ctx, parentcloseout.SettingKey, &markers); err != nil {
		t.Fatal(err)
	}
	if _, ok := markers[parent.ID]; ok {
		t.Fatalf("closed parent marker was not pruned: %+v", markers)
	}
}

func closeoutFeed(t *testing.T, s *Service, conversation string) []FeedItem {
	t.Helper()
	items, err := s.rawFeed(context.Background(), conversation, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestParentCloseoutContainerClosesItselfWithRollUp(t *testing.T) {
	s, f := newTodoReadyService(t, planUpdateStore{&planMemStore{memStore: &memStore{}, plans: map[string]db.TaskPlanRow{}}})
	f.hello.Locale = "en"
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	ctx := context.Background()
	off := closeoutClock(s)

	parent := createRequestAndPiece(t, s, "conv1", "Ship the release", "", "", nil)
	var pieces []Todo
	for _, title := range []string{"Backend", "Client", "Docs", "Rollback plan"} {
		pieces = append(pieces, createRequestAndPiece(t, s, "conv1", title, "", parent.ID, nil))
	}
	for _, c := range pieces[:3] {
		if _, err := s.editTodo(ctx, c.ID, dbTodoDoneEdit(true)); err != nil {
			t.Fatal(err)
		}
	}
	// the 4/4 case: the last piece was cancelled, which no longer blocks the parent
	if closed, err := s.reviewDoneTodo(ctx, pieces[3].ID, "not needed any more", false); err != nil || !closed {
		t.Fatalf("cancel piece: closed=%v err=%v", closed, err)
	}

	s.sweepParentCloseout(ctx)
	if got := todoNow(s, parent.ID); got.DoneAt != nil {
		t.Fatal("the container was closed inside the settle window")
	}
	*off = 2 * time.Minute
	s.sweepParentCloseout(ctx)
	s.sweepParentCloseout(ctx)
	got := todoNow(s, parent.ID)
	if got.DoneAt == nil || got.DoneBy != db.DoneByAuto {
		t.Fatalf("container not closed by the hub: %+v", got)
	}
	for _, want := range []string{"Backend: accepted", "Docs: accepted", "Rollback plan: cancelled (not needed any more)", "3 accepted, 1 cancelled"} {
		if !strings.Contains(got.DoneReason, want) {
			t.Errorf("roll-up lacks %q:\n%s", want, got.DoneReason)
		}
	}
	if _, ok := pendingParentCloseout(s, parent.ID); ok {
		t.Fatal("a container raised a parent_closeout wake")
	}
	var receipts int
	for _, it := range closeoutFeed(t, s, "conv1") {
		if it.Kind == feedReceipt && it.Ref == parent.ID && strings.Contains(string(it.Body), "Ship the release") {
			receipts++
		}
	}
	if receipts != 1 {
		t.Fatalf("owner conversation got %d closed-to-do receipts, want 1", receipts)
	}
	sendPendingPRConflicts(s)
	if got := sendsToConversation(f, "conv1"); len(got) != 0 {
		t.Fatalf("closing a container started a model turn: %q", got)
	}
}

func TestParentCloseoutSecondPieceInsideSettleWindow(t *testing.T) {
	s, _ := newTodoReadyService(t, &memStore{})
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	ctx := context.Background()
	off := closeoutClock(s)

	parent := createRequestAndPiece(t, s, "conv1", "Request", "", "", nil)
	p1 := createRequestAndPiece(t, s, "conv1", "First piece", "", parent.ID, nil)
	if _, err := s.editTodo(ctx, p1.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	s.sweepParentCloseout(ctx) // "All 1 child to-dos are done" used to fire here
	p2 := createRequestAndPiece(t, s, "conv1", "Second piece", "", parent.ID, nil)
	*off = 10 * time.Minute
	s.sweepParentCloseout(ctx)
	if got := todoNow(s, parent.ID); got.DoneAt != nil {
		t.Fatal("closed with a piece still open")
	}
	if _, ok := pendingParentCloseout(s, parent.ID); ok {
		t.Fatal("a wake fired with a piece still open")
	}
	*off = 0
	if _, err := s.editTodo(ctx, p2.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	s.sweepParentCloseout(ctx)
	if got := todoNow(s, parent.ID); got.DoneAt != nil {
		t.Fatal("closed inside the settle window")
	}
	*off = 2 * time.Minute
	s.sweepParentCloseout(ctx)
	if got := todoNow(s, parent.ID); got.DoneAt == nil || !strings.Contains(got.DoneReason, "Second piece") {
		t.Fatalf("both pieces done: parent not closed with both in its roll-up: %+v", got)
	}
}

func TestParentCloseoutNonContainerWakeIsRaisedAgain(t *testing.T) {
	s, _ := newTodoReadyService(t, &memStore{})
	s.superviseTimes = superviseTimings{gather: time.Hour, every: time.Hour, urgent: time.Hour}
	ctx := context.Background()
	off := closeoutClock(s)

	parent := createRequestAndPiece(t, s, "conv1", "Request with its own criteria", "", "", nil)
	withOwnCriteria(s, parent.ID)
	p := createRequestAndPiece(t, s, "conv1", "Only piece", "", parent.ID, nil)
	if _, err := s.editTodo(ctx, p.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	delivered := func(want string) {
		t.Helper()
		g, ok := pendingParentCloseout(s, parent.ID)
		if !ok || !strings.HasSuffix(g.Key, want) {
			t.Fatalf("pending wake = %+v (%v), want a key ending %q", g, ok, want)
		}
		s.supervise.mu.Lock()
		delete(s.supervise.pending, g.id())
		s.supervise.mu.Unlock()
		s.parentCloseoutTold(ctx, []superviseSignal{g})
	}
	*off = 2 * time.Minute
	s.sweepParentCloseout(ctx)
	if got := todoNow(s, parent.ID); got.DoneAt != nil {
		t.Fatal("a parent with criteria of its own was closed by the hub")
	}
	delivered(p.ID)

	*off = 20 * time.Minute // inside the back-off
	s.sweepParentCloseout(ctx)
	if _, ok := pendingParentCloseout(s, parent.ID); ok {
		t.Fatal("raised again inside the back-off")
	}
	*off = 40 * time.Minute
	s.sweepParentCloseout(ctx)
	delivered("#2")
	*off = 80 * time.Minute
	s.sweepParentCloseout(ctx)
	delivered("#3")
	*off = 200 * time.Minute
	s.sweepParentCloseout(ctx)
	if _, ok := pendingParentCloseout(s, parent.ID); ok {
		t.Fatal("raised a fourth time")
	}
}

func TestCloseoutStatesForTheView(t *testing.T) {
	s, _ := newTodoReadyService(t, &memStore{})
	ctx := context.Background()
	container := createRequestAndPiece(t, s, "conv1", "Container", "", "", nil)
	own := createRequestAndPiece(t, s, "conv1", "Own criteria", "", "", nil)
	withOwnCriteria(s, own.ID)
	open := createRequestAndPiece(t, s, "conv1", "Still open", "", "", nil)
	for _, p := range []Todo{container, own, open} {
		piece := createRequestAndPiece(t, s, "conv1", "Piece of "+p.Title, "", p.ID, nil)
		if p.ID != open.ID {
			if _, err := s.editTodo(ctx, piece.ID, dbTodoDoneEdit(true)); err != nil {
				t.Fatal(err)
			}
		}
	}
	var list []listedTodo
	for _, td := range s.todosOf(ctx, "*") {
		list = append(list, listedTodo{Todo: td})
	}
	got := s.closeoutStates(ctx, list)
	if got[container.ID] != parentcloseout.StateClosing || got[own.ID] != parentcloseout.StateAwaiting || got[open.ID] != "" {
		t.Fatalf("states %v", got)
	}
	// Once the wakes are used up nobody is told again: the parent is unattended, no longer awaiting.
	*closeoutClock(s) = 10 * time.Minute // the pieces have settled
	_, ps := s.closeoutPieces(own.ID)
	key := parentcloseout.Key(ps, time.Time{})
	s.closeout().Told(ctx, own.ID, key, "conv1")
	if got := s.closeoutStates(ctx, list); got[own.ID] != parentcloseout.StateAwaiting {
		t.Fatalf("one wake delivered, more may follow: %v", got)
	}
	s.closeout().Told(ctx, own.ID, key+"#2", "conv1")
	s.closeout().Told(ctx, own.ID, key+"#3", "conv1")
	if got := s.closeoutStates(ctx, list); got[own.ID] != "" {
		t.Fatalf("every wake used up, yet still awaiting: %v", got)
	}
}

// A piece reopened between the decision to close a container and the close leaves the parent open.
func TestCloseContainerAbortsWhenAPieceChangedAfterTheDecision(t *testing.T) {
	s, _ := newTodoReadyService(t, &memStore{})
	ctx := context.Background()
	parent := createRequestAndPiece(t, s, "conv1", "Container", "", "", nil)
	piece := createRequestAndPiece(t, s, "conv1", "Piece", "", parent.ID, nil)
	if _, err := s.editTodo(ctx, piece.ID, dbTodoDoneEdit(true)); err != nil {
		t.Fatal(err)
	}
	children, pieces := s.closeoutPieces(parent.ID) // the decision is taken on these
	if _, err := s.editTodo(ctx, piece.ID, dbTodoDoneEdit(false)); err != nil {
		t.Fatal(err)
	}
	s.closeContainer(ctx, todoNow(s, parent.ID), children, pieces)
	if todoNow(s, parent.ID).DoneAt != nil {
		t.Fatal("the container was closed with a piece reopened")
	}
}
