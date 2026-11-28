package assistant

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/assistant/signalrule"
	"github.com/sunfayao/persona/server/internal/proto"
)

// ---- duplicates ----

// duplicateHints are the tasks going on and the agents at work that look like the same work as task t: a shared
// issue / PR number, or the same machine and directory with a similar title, or a very similar title anywhere.
func duplicateHints(t PlanTask, todos []Todo, sessions []proto.Session) []string {
	refs := signalrule.IssueRefs(t.Title + " " + t.Prompt)
	var out []string
	seen := map[string]bool{}
	add := func(key, line string) {
		if !seen[key] && len(out) < 4 {
			seen[key] = true
			out = append(out, line)
		}
	}
	for _, o := range todos {
		if o.DoneAt != nil {
			continue
		}
		sameDir := false
		for _, a := range o.Agents {
			if a.ReleasedAt == nil && a.DismissedAt == nil && a.MachineID == t.Machine && signalrule.DirsOverlap(a.Cwd, t.Cwd) {
				sameDir = true
			}
		}
		if shared := signalrule.SharedRef(refs, signalrule.IssueRefs(o.Title+" "+o.Notes+" "+o.PRURL)); shared != "" {
			add("todo:"+o.ID, fmt.Sprintf("task “%s” (%s) also mentions %s", o.Title, o.ID, shared))
		} else if sim := signalrule.TitleSimilarity(t.Title, o.Title); sim >= 0.7 || sameDir && sim >= 0.4 {
			add("todo:"+o.ID, fmt.Sprintf("task “%s” (%s) has a similar title%s", o.Title, o.ID, map[bool]string{true: ", in the same directory", false: ""}[sameDir]))
		}
	}
	for _, c := range sessions {
		if c.Assistant || c.State != proto.StateWorking && c.State != proto.StateNeedsYou {
			continue
		}
		title := cmp.Or(c.Title, c.LastText)
		where := fmt.Sprintf("the Agent “%s” on %s (session_id=%s)", clip(oneLine(title), 40), c.MachineID, short(c.SessionID))
		sameDir := c.MachineID == t.Machine && signalrule.DirsOverlap(c.Cwd, t.Cwd)
		if shared := signalrule.SharedRef(refs, signalrule.IssueRefs(c.Title+" "+c.LastText)); shared != "" {
			add("agent:"+c.MachineID+"/"+c.SessionID, where+" is also working on "+shared)
		} else if sim := signalrule.TitleSimilarity(t.Title, c.Title); sim >= 0.7 || sameDir && sim >= 0.4 {
			add("agent:"+c.MachineID+"/"+c.SessionID, where+" is working on something similar")
		}
	}
	return out
}

// planDuplicates is what the task_plan tool adds to its answer about the plan's tasks that look like work already
// going on (duplicateHints); "" when none does. A hint, not a refusal: the plan goes on as ever.
func (s *Service) planDuplicates(ctx context.Context, tasks []PlanTask) string {
	var open []Todo
	if s.ensureTodosLoaded(ctx) == nil {
		own := map[string]bool{} // a todo a task runs on is that task, not a copy of it
		for _, t := range tasks {
			if t.ReuseTodo != "" {
				own[t.ReuseTodo] = true
			}
		}
		s.todos.mu.RLock()
		for _, t := range s.todos.todos {
			if t.DoneAt == nil && !own[t.ID] {
				open = append(open, copyTodoLocked(t))
			}
		}
		s.todos.mu.RUnlock()
	}
	var sessions []proto.Session
	for _, d := range s.devices(ctx) {
		if !d.Reachable {
			continue
		}
		list, err := s.sessions(ctx, d.ID)
		if err != nil {
			continue
		}
		for _, c := range list {
			if c.HiddenByDirectory() {
				continue
			}
			if c.MachineID == "" {
				c.MachineID = d.ID
			}
			sessions = append(sessions, c)
		}
	}
	var b strings.Builder
	for _, t := range tasks {
		todos := open
		if t.Continues != "" {
			// The to-do it says it carries on (task_brief_plan.go) is not a copy of it; any other open to-do like it is
			// (#6427: two continuations of one stopped task).
			todos = slices.DeleteFunc(slices.Clone(open), func(o Todo) bool { return o.ID == t.Continues })
		}
		hints := duplicateHints(t, todos, sessions)
		if len(hints) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n- “%s” may duplicate these: %s", t.Title, strings.Join(hints, "; "))
	}
	if b.Len() == 0 {
		return ""
	}
	return "\n\nPossible duplicates (work already going on): look at them (agent_read, the todo) before this goes on; if it is the " +
		"same work, cancel the new task (task_review cancelled, or dismiss the plan) or send what is new to the agent already on it. " +
		"If the new task takes over from one of them (a retry elsewhere, a rework round, a part split off), plan it again with " +
		"continues set to that todo id and continues_why, so it keeps that task's brief (the owner's request and criteria); " +
		"if it is different work, say why in your answer." + b.String()
}
