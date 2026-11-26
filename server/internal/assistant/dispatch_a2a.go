package assistant

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/agents"
)

// The settings book contains dispatches created before A2A. New dispatches
// retain the same start method and task card, and are tracked by the A2A row.
func (s *Service) dispatchTasks(ctx context.Context, ref agents.Ref) ([]dispatchTask, bool, error) {
	tasks := s.dispatched(ctx)
	rows, child, err := s.agents.DispatchTasks(ctx, ref, dispatchKeep)
	if err != nil {
		return nil, false, err
	}
	for _, task := range rows {
		tasks = append(tasks, dispatchTask{FromMachine: ref.Machine, FromSession: ref.Session, Machine: task.TargetMachine,
			Session: task.TargetSession, Cwd: cmp.Or(task.Launch.Worktree, task.Launch.CWD), Title: task.Launch.Title, At: task.Created, a2a: task})
	}
	return tasks, child, nil
}

// admitDispatchA2A answers a retry by requestID with its task, or admits a new dispatch and records it: one within the
// caller's limits, to a computer that is online, which dispatchStartRefusal did not refuse (refused, worked out before
// the lock so that no launcher list is read under it).
func (s *Service) admitDispatchA2A(ctx context.Context, from agents.Ref, target, requestID, prompt string, launch agents.Launch, now time.Time, refused error) (*agents.DispatchStart, bool, error) {
	s.dispatch.admit.Lock()
	defer s.dispatch.admit.Unlock()
	digest := sha256.Sum256([]byte(prompt))
	launch.PromptHash = hex.EncodeToString(digest[:])
	if start, existing, err := s.agents.RetryDispatch(ctx, from, target, requestID, prompt, launch); existing || err != nil {
		return start, existing, err
	}
	if err := s.checkDispatchCapacity(ctx, from, now); err != nil {
		return nil, false, err
	}
	if dev, ok := s.machineByName(ctx, target); !ok || !dev.Online && !dev.Reachable {
		return nil, false, fmt.Errorf("%s is offline; nothing was started", target)
	}
	if refused != nil {
		return nil, false, refused
	}
	return s.agents.PrepareDispatch(ctx, from, target, requestID, prompt, launch)
}

func (s *Service) checkDispatchCapacity(ctx context.Context, from agents.Ref, now time.Time) error {
	tasks, child, err := s.dispatchTasks(ctx, from)
	if err != nil {
		return err
	}
	limit, active := s.hello(ctx).Agents.Dispatch.Limit(), 0
	for _, task := range tasks {
		child = child || task.Machine == from.Machine && task.Session == from.Session
		if task.FromMachine != from.Machine || task.FromSession != from.Session {
			continue
		}
		if task.a2a != nil && !task.a2a.Terminal() || task.a2a == nil && s.dispatchWorking(ctx, task, now) {
			active++
		}
	}
	if child {
		return errors.New("you are a task another agent handed to this computer: do the work here (your own in-session " +
			"subagents are fine), a dispatched task does not dispatch; nothing was started")
	}
	if active >= limit {
		return fmt.Errorf("%d tasks you dispatched are still working (the limit is %d): read them with dispatch_read and "+
			"wait for one to stop; nothing was started", active, limit)
	}
	return nil
}
