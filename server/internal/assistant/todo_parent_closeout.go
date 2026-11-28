package assistant

import (
	"context"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/parentcloseout"
	"github.com/sunfayao/persona/server/internal/prompts"
)

// Parent to-do closeout (#13100). When every part_of piece of an open parent to-do is finished, the hub either closes
// it itself (a request container: no criteria or Agent of its own, every piece a dispatched task; roll-up result, no
// model turn) or raises a durable parent_closeout wake to the owning conversation, raised again while nobody answers.
// The rules, the settle window and the wake markers live in assistant/parentcloseout; this file reads the to-dos for
// them and acts on the verdict.

func isOutsideDecisionTodo(id string) bool {
	return strings.HasPrefix(id, "todo_od_")
}

// closeout is the rules' domain, built on first use. Tests set todoBook.closeoutNow before it.
func (s *Service) closeout() *parentcloseout.Domain {
	b := &s.todos
	b.closeoutOnce.Do(func() {
		b.closeoutDom = parentcloseout.New(parentcloseout.Dependencies{
			Clock: parentcloseout.ClockFunc(func() time.Time {
				if f := b.closeoutNow; f != nil {
					return f()
				}
				return time.Now()
			}),
			Store: func() parentcloseout.Store {
				if st := s.store(); st != nil {
					return st
				}
				return nil
			},
			Logf: s.logf,
		})
	})
	return b.closeoutDom
}

func closeoutPieceOf(t Todo) parentcloseout.Piece {
	at := max(t.CreatedAt.UnixNano(), t.UpdatedAt.UnixNano())
	if t.DoneAt != nil {
		at = max(at, t.DoneAt.UnixNano())
	}
	return parentcloseout.Piece{ID: t.ID, Title: t.Title, Done: t.DoneAt != nil, Cancelled: t.Cancelled(),
		CancelReason: strings.TrimSpace(t.CancelledReason + " " + t.DoneReason), StartFailed: t.StartError != "",
		Dispatched: t.Origin != "" || t.Supervision != nil, Changed: time.Unix(0, at)}
}

func closeoutParentOf(t Todo) parentcloseout.Parent {
	own := t // a supervisor Agent attached to the request is no work of its own: the container still closes itself
	own.Agents = slices.DeleteFunc(slices.Clone(t.Agents), func(a TodoAgent) bool { return a.Source == sourceLeader })
	p := parentcloseout.Parent{Open: t.DoneAt == nil && !t.Cancelled() && t.StartError == "",
		Excluded: t.isConcern() || isAskTodo(t.ID) || isOutsideDecisionTodo(t.ID), HasAgent: todoHasActiveAgent(own),
		OwnAgent: todoHasAgents(own), HasLedger: t.Supervision != nil, Updated: t.UpdatedAt}
	if t.ReopenedAt != nil {
		p.Reopened = *t.ReopenedAt
	}
	if v := t.Supervision; v != nil {
		p.Parked = v.State == reviewParked
		if v.UpdatedAt.After(p.Updated) {
			p.Updated = v.UpdatedAt
		}
	}
	for _, at := range []*time.Time{t.RemindAt, t.DueAt} {
		if at != nil && at.After(p.HoldUntil) {
			p.HoldUntil = *at
		}
	}
	if day, err := time.Parse("2006-01-02", t.DueDate); err == nil && day.Add(24*time.Hour).After(p.HoldUntil) {
		p.HoldUntil = day.Add(24 * time.Hour)
	}
	return p
}

// closeoutPieces are the real part_of pieces of parent id, oldest first, with their closeout values.
func (s *Service) closeoutPieces(id string) ([]Todo, []parentcloseout.Piece) {
	s.todos.mu.RLock()
	var children []Todo
	for _, t := range s.todos.todos {
		if t.PartOf == id && t.ID != id && !isAskTodo(t.ID) && !isOutsideDecisionTodo(t.ID) {
			children = append(children, copyTodoLocked(t))
		}
	}
	s.todos.mu.RUnlock()
	sort.Slice(children, func(i, j int) bool {
		if !children[i].CreatedAt.Equal(children[j].CreatedAt) {
			return children[i].CreatedAt.Before(children[j].CreatedAt)
		}
		return children[i].ID < children[j].ID
	})
	pieces := make([]parentcloseout.Piece, len(children))
	for i, c := range children {
		pieces[i] = closeoutPieceOf(c)
	}
	return children, pieces
}

// closeoutStates is the closeout state (parentcloseout.StateClosing, StateAwaiting) of each open parent in list whose
// pieces are all finished, for the to-do view.
func (s *Service) closeoutStates(list []listedTodo) map[string]string {
	pieces := map[string][]parentcloseout.Piece{}
	for _, t := range list {
		if t.PartOf != "" && t.PartOf != t.ID && !isAskTodo(t.ID) && !isOutsideDecisionTodo(t.ID) {
			pieces[t.PartOf] = append(pieces[t.PartOf], closeoutPieceOf(t.Todo))
		}
	}
	out := map[string]string{}
	for _, t := range list {
		if v := s.closeout().Evaluate(closeoutParentOf(t.Todo), pieces[t.ID], nil); v.State != "" {
			out[t.ID] = v.State
		}
	}
	return out
}

// childDeliveryPRs returns the deduplicated merged/delivery PR URLs for a completed child to-do.
func (s *Service) childDeliveryPRs(ctx context.Context, child Todo) ([]string, error) {
	delivery, err := s.todoDeliveryPRs(ctx, child)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	add := func(u string) {
		u = strings.TrimSpace(u)
		k := normalizePRKey(u)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		out = append(out, u)
	}
	for _, u := range todoObservedMergedPRs(child, delivery) {
		add(u)
	}
	for _, u := range delivery {
		add(u)
	}
	return out, nil
}

// closeoutPrepare reads parent's pieces and its wake marker and says what to do with it. A non-nil error is a transient
// failure to read the marker.
func (s *Service) closeoutPrepare(ctx context.Context, parent Todo) ([]Todo, []parentcloseout.Piece, parentcloseout.Verdict, error) {
	children, pieces := s.closeoutPieces(parent.ID)
	m, ok, err := s.closeout().Marker(ctx, parent.ID)
	if err != nil {
		return nil, nil, parentcloseout.Verdict{}, err
	}
	var marker *parentcloseout.Marker
	if ok {
		marker = &m
	}
	return children, pieces, s.closeout().Evaluate(closeoutParentOf(parent), pieces, marker), nil
}

// parentCloseoutSupervisor resolves the owning conversation (the main chat or a side conversation, never the background supervisor)
// for parent's closeout wake. A parent owned by a task leader agent returns "" so leaderRoute delivers to that agent.
func (s *Service) parentCloseoutSupervisor(ctx context.Context, cfg Settings, parent Todo, children []Todo) string {
	isLiveConversation := func(id string) bool {
		if id == "" || id == cfg.Main || id == cfg.BackgroundSupervisor || s.isLeaderRef(id) {
			return false
		}
		if cfg.MachineID != "" {
			if c, ok := s.session(ctx, cfg.MachineID, id); ok && c.Assistant {
				return true
			}
		}
		return false
	}

	if sup := supervisorOf(parent); sup != "" {
		if s.isLeaderRef(sup) {
			return ""
		}
		if isLiveConversation(sup) {
			return sup
		}
		if sup == cfg.Main {
			return cfg.Main
		}
	}
	if orig := parent.Origin; orig != "" {
		if isLiveConversation(orig) {
			return orig
		}
		if orig == cfg.Main {
			return cfg.Main
		}
	}
	return cfg.Main
}

// parentCloseoutTold persists the wake markers of delivered or durably queued sigParentCloseout signals.
func (s *Service) parentCloseoutTold(ctx context.Context, list []superviseSignal) {
	for _, g := range list {
		if g.Kind == sigParentCloseout && g.Todo != "" && g.Key != "" {
			s.closeout().Told(ctx, g.Todo, g.Key, g.To)
		}
	}
}

// pruneParentCloseoutWakes removes wake markers for closed or deleted parents.
func (s *Service) pruneParentCloseoutWakes(ctx context.Context) {
	s.closeout().Forget(ctx, "", func(id string) bool {
		t, ok := s.todoNow(id)
		return !ok || t.DoneAt != nil
	})
}

// clearSentParentCloseout removes in-memory sent markers for sigParentCloseout signals dropped before delivery.
func (s *Service) clearSentParentCloseout(list []superviseSignal) {
	b := &s.supervise
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, g := range list {
		if g.Kind == sigParentCloseout && b.sent[g.id()] == g.Key {
			delete(b.sent, g.id())
		}
	}
}

// resetParentCloseoutWake clears pending/sent in-memory parent_closeout signals, the durable marker, and the
// task-bound supervision dedup fingerprint when parent todoID is reopened.
func (s *Service) resetParentCloseoutWake(ctx context.Context, todoID string) {
	todoID = strings.TrimSpace(todoID)
	if todoID == "" {
		return
	}
	sigID := superviseSignal{Todo: todoID, Kind: sigParentCloseout}.id()
	b := &s.supervise
	b.mu.Lock()
	delete(b.pending, sigID)
	delete(b.sent, sigID)
	b.mu.Unlock()
	s.closeout().Forget(ctx, todoID, nil)
	s.clearParentCloseoutDedup(ctx, todoID)
}

// maybeEmitParentCloseout evaluates parent: a container whose pieces have stayed settled is closed with a roll-up; any
// other finished parent gets one sigParentCloseout signal for its owning conversation (again after the back-off while
// nobody answers).
func (s *Service) maybeEmitParentCloseout(ctx context.Context, parent Todo) {
	children, pieces, v, err := s.closeoutPrepare(ctx, parent)
	if err != nil {
		s.logf("parent_closeout: check wake marker for %s: %v", parent.ID, err)
		return
	}
	switch v.Action {
	case parentcloseout.ActionClose:
		s.closeContainer(ctx, parent, children, pieces)
	case parentcloseout.ActionWake:
		s.signal(superviseSignal{Todo: parent.ID, Kind: sigParentCloseout, Key: v.Key,
			To: s.parentCloseoutSupervisor(ctx, s.settings(ctx), parent, children)})
	}
}

// closeContainer closes request container parent with the roll-up of its pieces as its result (done by the hub, no model
// turn) and puts the closed to-do in the owning conversation's feed.
func (s *Service) closeContainer(ctx context.Context, parent Todo, children []Todo, pieces []parentcloseout.Piece) {
	if s.outsideBackingPending(parent.ID) {
		return
	}
	var last time.Time
	for i, c := range children {
		if pieces[i].Changed.After(last) {
			last = pieces[i].Changed
		}
		if !pieces[i].Accepted() {
			continue
		}
		prs, err := s.childDeliveryPRs(ctx, c)
		if err != nil {
			s.logf("parent_closeout: delivery PRs of %s: %v", c.ID, err)
			return
		}
		pieces[i].PRs = prs
		for _, k := range s.briefCriteriaNow(c) {
			if k.State != criterionDropped {
				pieces[i].Criteria++
			}
			if k.State == criterionMet {
				pieces[i].Met++
			}
		}
	}
	text := parentcloseout.RollUp(s.lang(ctx), pieces)
	if done, err := s.autoDoneTodo(ctx, parent.ID, text, last); err != nil || !done {
		if err != nil {
			s.logf("parent_closeout: close %s: %v", parent.ID, err)
		}
		return
	}
	if to := s.parentCloseoutSupervisor(ctx, s.settings(ctx), parent, children); to != "" {
		if _, err := s.appendFeed(ctx, to, feedReceipt, parent.ID, receiptBody{Title: parent.Title, State: "done", Summary: text}); err != nil {
			s.logf("parent_closeout: feed for %s: %v", parent.ID, err)
		}
	}
}

// evaluateParentCloseoutForChild checks the part_of parent of childID, whose state or place just changed.
func (s *Service) evaluateParentCloseoutForChild(ctx context.Context, childID string) {
	childID = strings.TrimSpace(childID)
	if childID == "" || isAskTodo(childID) || isOutsideDecisionTodo(childID) {
		return
	}
	if child, ok := s.todoNow(childID); ok && child.PartOf != child.ID {
		s.evaluateParentCloseout(ctx, child.PartOf)
	}
}

// evaluateParentCloseout checks parent id (a no-op for "" or a missing to-do).
func (s *Service) evaluateParentCloseout(ctx context.Context, id string) {
	if parent, ok := s.todoNow(id); ok && id != "" {
		s.maybeEmitParentCloseout(ctx, parent)
	}
}

// sweepParentCloseout reconciles open parent to-dos on startup and every supervision sweep: it closes containers once
// their pieces have settled, recovers wakes lost before delivery and raises unanswered ones again. A delivered wake
// stays suppressed by its persisted marker until the back-off ends.
func (s *Service) sweepParentCloseout(ctx context.Context) {
	if s.ensureTodosLoaded(ctx) != nil {
		return
	}
	s.pruneParentCloseoutWakes(ctx)
	s.todos.mu.RLock()
	parentIDs := map[string]bool{}
	for _, t := range s.todos.todos {
		if t.PartOf != "" && t.PartOf != t.ID && !isAskTodo(t.ID) && !isOutsideDecisionTodo(t.ID) {
			parentIDs[t.PartOf] = true
		}
	}
	var parents []Todo
	for id := range parentIDs {
		if p := s.todos.todos[id]; p != nil && p.DoneAt == nil {
			parents = append(parents, copyTodoLocked(p))
		}
	}
	s.todos.mu.RUnlock()
	sort.Slice(parents, func(i, j int) bool { return parents[i].ID < parents[j].ID })
	for _, p := range parents {
		s.maybeEmitParentCloseout(ctx, p)
	}
}

// parentCloseoutDigestEntry builds and revalidates a sigParentCloseout supervision entry at delivery time.
func (s *Service) parentCloseoutDigestEntry(ctx context.Context, g superviseSignal, web string) (prompts.SuperviseEntry, bool, bool) {
	t, ok := s.todoNow(g.Todo)
	if !ok || t.DoneAt != nil {
		return prompts.SuperviseEntry{}, false, false
	}
	children, pieces, v, err := s.closeoutPrepare(ctx, t)
	if err != nil {
		return prompts.SuperviseEntry{}, false, true
	}
	if v.Container || !parentcloseout.SameSet(v.Key, g.Key) {
		return prompts.SuperviseEntry{}, false, false
	}
	var promptChildren []prompts.ParentCloseoutChild
	for i, c := range children {
		if !pieces[i].Accepted() {
			continue
		}
		prs, err := s.childDeliveryPRs(ctx, c)
		if err != nil {
			return prompts.SuperviseEntry{}, false, true
		}
		promptChildren = append(promptChildren, prompts.ParentCloseoutChild{ID: c.ID, Title: c.Title, PRs: prs})
	}
	var doneWhen []string
	if t.Supervision != nil {
		doneWhen = t.Supervision.DoneWhen
	}
	lang := s.replyLang(ctx, string(s.botLang(ctx)))
	if g.To == "" && t.Supervision != nil && s.isLeaderRef(t.Supervision.Supervisor) {
		lang = "English"
	}
	wakeText := prompts.RenderParentCloseoutWake(prompts.ParentCloseoutWake{
		Language: lang,
		Children: promptChildren,
		Notes:    clip(strings.TrimSpace(t.Notes), 1000),
	})
	return prompts.SuperviseEntry{
		ID:             t.ID,
		Title:          t.Title,
		Kind:           sigParentCloseout,
		DoneWhen:       doneWhen,
		Link:           todoLink(web, t.ID),
		Quiet:          t.Quiet,
		ParentCloseout: wakeText,
	}, true, false
}
