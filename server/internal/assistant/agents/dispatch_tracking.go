package agents

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/proto"
)

// DispatchStart is a recorded launch and the store that accepted it.
type DispatchStart struct {
	store Store
	Task  *Task
}

func (s *Domain) HasDispatchRequest(ctx context.Context, from Ref, messageID string) bool {
	if messageID == "" {
		return false
	}
	st, err := s.store()
	if err != nil {
		return false
	}
	row, err := st.A2ARequest(ctx, SessionCaller(from).Owner, messageID)
	return err == nil && row != nil
}

// DispatchMessage sets the launch's A2A address and returns the first prompt renderer.
// The launcher can change during model selection, so the caller renders after selection.
func (s *Domain) DispatchMessage(ctx context.Context, task *Task, params map[string]any) func(context.Context, string, string) {
	source := task.source()
	first := task.Events[0]
	first.Text = params["prompt"].(string)
	message := a2aTaskMessage(task, &first, source, string(s.language(ctx)))
	id := a2aDeliveryID(task.ID, 1)
	params["session_id"] = task.TargetSession
	params["client_msg_id"] = id
	params["message_source"] = a2aMessageSource(source)
	return func(ctx context.Context, machine, launcher string) {
		s.a2aLaunchEntry(ctx, machine, launcher).lay(&message)
		params["prompt"] = a2aRelay(message, source, id)
	}
}

func (task *Task) Terminal() bool {
	return a2aTerminal(task.State)
}
func (s *Domain) DispatchTasks(ctx context.Context, ref Ref, keep time.Duration) ([]*Task, bool, error) {
	var tasks []*Task
	st, err := s.store()
	if err != nil {
		return tasks, false, nil
	}
	cfg := s.config(ctx)
	if !s.a2aAllowed(ctx, cfg, ref) {
		return tasks, false, nil
	}
	caller := SessionCaller(ref)
	rows, err := st.A2ADispatches(ctx, caller.Owner, ref.Machine, ref.Session, s.clock.Now().Add(-keep))
	if err != nil {
		return nil, false, err
	}
	child := false
	for _, row := range rows {
		task, err := DecodeTask(&row, nil)
		if err != nil {
			return nil, false, err
		}
		if task.Launch == nil {
			continue
		}
		if task.target() == ref {
			child = true
		}
		if task.Owner == caller.Owner && s.a2aTaskVisible(ctx, cfg, caller, task) {
			tasks = append(tasks, task)
		}
	}
	return tasks, child, nil
}

func (s *Domain) RetryDispatch(ctx context.Context, from Ref, target, requestID, prompt string, launch Launch) (*DispatchStart, bool, error) {
	if requestID != "" {
		if st, err := s.store(); err == nil {
			caller := SessionCaller(from)
			row, err := DecodeTask(st.A2ARequest(ctx, caller.Owner, requestID))
			if row != nil || err != nil {
				cfg := s.config(ctx)
				if !s.a2aAllowed(ctx, cfg, from) || row != nil && !s.a2aAllowed(ctx, cfg, row.target()) {
					return nil, false, errA2AUnavailable
				}
				launch.Dispatch = true
				row, err := a2aCheckRetry(row, err, Ref{Machine: target}, "", prompt, true, &launch)
				return &DispatchStart{store: st, Task: row}, true, err
			}
		}
	}
	return nil, false, nil
}

func (s *Domain) PrepareDispatch(ctx context.Context, from Ref, target, requestID, prompt string, launch Launch) (*DispatchStart, bool, error) {
	st, err := s.store()
	if err != nil {
		// Managers without the A2A store retain the legacy dispatch path.
		return nil, false, nil
	}
	caller := SessionCaller(from)
	to := Ref{target, s.newSessionID()}
	cfg := s.config(ctx)
	if !s.a2aAllowed(ctx, cfg, from) || !s.a2aAllowed(ctx, cfg, to) {
		return nil, false, errA2AUnavailable
	}
	if requestID == "" {
		requestID = s.newID()
	}
	if !a2aValidID(requestID) {
		return nil, false, errA2AInvalid
	}
	launch.Dispatch, launch.Pending = true, true
	now := s.clock.Now().UTC()
	task := &Task{
		A2ARow: db.A2ARow{ID: s.newID(), Owner: caller.Owner, Request: requestID, Context: s.newID(),
			TargetMachine: to.Machine, TargetSession: to.Session, SourceMachine: from.Machine, SourceSession: from.Session},
		taskBody: taskBody{Wake: true, Launch: &launch},
	}
	s.appendEvent(task, Event{ID: requestID, Kind: "message", Text: a2aCleanText(prompt), State: a2aSubmitted, Delivery: a2aSending, Attempted: now})
	task.Body, err = task.MarshalBody()
	if err != nil {
		return nil, false, err
	}
	row, err := DecodeTask(st.A2ACreate(ctx, task.A2ARow))
	row, err = a2aCheckRetry(row, err, to, "", prompt, true, &launch)
	if err != nil {
		return nil, false, err
	}
	return &DispatchStart{store: st, Task: row}, row.ID != task.ID, nil
}

func (s *Domain) FinishDispatch(ctx context.Context, start *DispatchStart, session proto.Session, attempted bool, launchErr error) {
	if start == nil {
		return
	}
	defer s.Wake()
	ctx, cancel := s.clock.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if attempted && (s.launches.StartUnknown(launchErr) || launchErr == nil && session.SessionID == "") {
		if err := s.a2aReceipt(ctx, start.store, start.Task.ID, 1, a2aUncertain); err != nil {
			s.logf("dispatch A2A %s: %v", start.Task.ID, err)
		}
		return
	}
	why := session.StartError // a definite failure's reason, on the A2A task as the caller was told it
	if launchErr != nil {
		why = launchErr.Error()
	}
	for range 5 {
		task, err := DecodeTask(start.store.A2AGet(ctx, start.Task.ID))
		if err == nil && task != nil && task.Launch.Pending {
			err = s.a2aLaunched(ctx, start.store, task, session, launchErr != nil || session.StartError != "", why)
		}
		if errors.Is(err, errA2AConflict) {
			continue
		}
		if err != nil {
			s.logf("dispatch A2A %s: %v", start.Task.ID, err)
		}
		return
	}
}

func (t *Task) DispatchStatus() string {
	if t.Disconnected && !a2aTerminal(t.State) {
		return "computer offline"
	}
	switch t.State {
	case a2aSubmitted, a2aWorking:
		return "working"
	case a2aInput:
		return "waiting for your input"
	case a2aAuth:
		return "waiting for the user"
	case a2aComplete:
		return "stopped"
	default:
		return t.State
	}
}

func (task *Task) DispatchResult(head string) string {
	if task.State == a2aSubmitted && task.Events[0].Delivery == a2aUncertain {
		head += " The start or first message is awaiting confirmation."
	}
	for i := len(task.Events) - 1; i >= 0; i-- {
		e := task.Events[i]
		if e.Kind == "status" && e.State == task.State && e.Text != "" {
			head += "\n\n" + e.Text
			break
		}
	}
	return fmt.Sprintf("%s\n\nA2A task %s.", head, task.ID)
}

func (s *Domain) ReadDispatch(ctx context.Context, from Ref, task *Task, wait, poll time.Duration) (string, error) {
	st, err := s.store()
	if err != nil {
		return "", err
	}
	c := SessionCaller(from)
	cfg := s.config(ctx)
	deadline := s.clock.Now().Add(wait)
	for task.DispatchStatus() == "working" && s.clock.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-s.clock.After(min(poll, deadline.Sub(s.clock.Now()))):
		}
		task, err = s.a2aGet(ctx, st, cfg, c, task.ID)
		if err != nil {
			return "", err
		}
	}
	name := task.TargetMachine
	if m, ok := s.machines.Resolve(ctx, name); ok {
		name = m.Name
	}
	return task.DispatchResult(fmt.Sprintf("Task %s on %s (%s): %s.", task.TargetSession,
		name, cmp.Or(task.Launch.Worktree, task.Launch.CWD), task.DispatchStatus())), nil
}
