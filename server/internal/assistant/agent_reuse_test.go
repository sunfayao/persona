package assistant

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// endedBoxAgent is agent sid of box as a node lists it once it ended without an owner's Stop (told to quit, or on its
// own): a stopped Persona terminal Agent whose node lets only the owner's own message start it again
// (capabilities.send), and that takes the main conversation's resume for the owner when delegated.
func endedBoxAgent(sid string, cause proto.StopCause, delegated bool) proto.Session {
	c := boxAgent(sid)
	c.Status, c.State = proto.StatusOffline, proto.StateOffline
	c.Capabilities = &proto.SessionCapabilities{Read: true, Terminal: true, Send: true, StopCause: cause, DelegatedResume: delegated}
	return c
}

// stoppedWith is stoppedBoxAgent with cause as its stop cause.
func stoppedWith(sid string, cause proto.StopCause) proto.Session {
	c := stoppedBoxAgent(sid)
	c.Capabilities.StopCause = cause
	return c
}

// lineOf is the agents_list line naming session sid, "" when none does.
func lineOf(list, sid string) string {
	for _, l := range strings.Split(list, "\n") {
		if strings.Contains(l, "session_id="+sid+" ") {
			return l
		}
	}
	return ""
}

// The default agents_list (no state, or active) no longer hides the Agents follow-up work could go back to: after
// what goes on, it lists the live ones at rest and the ones that ended in the last 24 hours, newest first, each with
// its model, its to-do and how to go on with it in its own context (reuse=). Muted and archived ones, and ones that
// ended longer ago, stay out.
func TestAgentsListDefaultShowsIdleAndRecentlyEndedAgentsForReuse(t *testing.T) {
	s, f := newQuietService(t)
	now := time.Now()
	working := boxAgent("work0001")
	working.Status, working.State = proto.StatusWorking, proto.StateWorking
	idle := boxAgent("idle0001")
	idle.UpdatedAt, idle.Launcher, idle.Model, idle.Effort = now.Add(-18*time.Minute), "claude", "claude-opus-5-5", "max"
	idle.ContextTokens, idle.ContextWindow = 50_000, 200_000
	idle.LastText = "The login page fix is merged"
	older := boxAgent("idle0002")
	older.UpdatedAt = now.Add(-3 * time.Hour)
	exited := endedBoxAgent("exit0001", proto.StopExit, true)
	exited.UpdatedAt, exited.Launcher = now.Add(-20*time.Minute), "codex"
	requested := "gpt-5.5"
	exited.RequestedModel = &requested
	exited.Work = &proto.WorkState{Phase: proto.WorkDone, Line: "修复登录页：已合并并验证"}
	ownerStopped := stoppedBoxAgent("user0001")
	ownerStopped.Capabilities.StopCause, ownerStopped.UpdatedAt = proto.StopUser, now.Add(-time.Hour)
	idleStopped := stoppedBoxAgent("stop0001")
	idleStopped.UpdatedAt = now.Add(-2 * time.Hour)
	stale := endedBoxAgent("old00001", proto.StopUnknown, true)
	stale.UpdatedAt = now.Add(-30 * time.Hour)
	muted := boxAgent("mute0001")
	archived := endedBoxAgent("arch0001", proto.StopUnknown, true)
	archived.Archived = true
	f.setSessions("box", stale, idleStopped, older, archived, ownerStopped, muted, exited, idle, working)
	f.muted = map[string][]string{"box": {"mute0001"}}
	open := onBoxTask(t, s, "idle0001", "conv1", false, "it is done")
	closed := onBoxTask(t, s, "exit0001", "conv1", true, "it is done")

	for _, state := range []string{"", "active"} {
		out, err := runJSON(t, s, "conv1", "agents_list", map[string]any{"state": state})
		if err != nil {
			t.Fatal(err)
		}
		active, idleAt, endedAt := strings.Index(out, "Active ("), strings.Index(out, "Idle, live and at rest"), strings.Index(out, "Ended in the last 24 hours")
		if active < 0 || idleAt < active || endedAt < idleAt {
			t.Fatalf("state %q: groups missing or out of order:\n%s", state, out)
		}
		for sid, group := range map[string]int{"work0001": active, "idle0001": idleAt, "idle0002": idleAt, "exit0001": endedAt,
			"user0001": endedAt, "stop0001": endedAt} {
			at := strings.Index(out, "session_id="+sid+" ")
			if at < group || (group == active && at > idleAt) || (group == idleAt && at > endedAt) {
				t.Fatalf("state %q: %s not in its group:\n%s", state, sid, out)
			}
		}
		for _, gone := range []string{"old00001", "mute0001", "arch0001"} {
			if strings.Contains(out, gone) {
				t.Fatalf("state %q listed %s:\n%s", state, gone, out)
			}
		}
		// Newest first within a group.
		if strings.Index(out, "idle0001") > strings.Index(out, "idle0002") ||
			strings.Index(out, "exit0001") > strings.Index(out, "user0001") || strings.Index(out, "user0001") > strings.Index(out, "stop0001") {
			t.Fatalf("state %q: not newest first:\n%s", state, out)
		}
		for sid, wants := range map[string][]string{
			"idle0001": {"launcher=claude", "model=claude-opus-5-5 effort=max", "context=25%", "todo=" + open.ID + " ·",
				"reuse=agent_send (live"},
			"exit0001": {"launcher=codex", "model=gpt-5.5", "todo=" + closed.ID + " (closed)", "reuse=agent_send resume=true (stop=exit)"},
			"user0001": {"reuse=the owner's Resume only (stop=user"},
			"stop0001": {"reuse=agent_send resume=true (stop=idle)"},
		} {
			line := lineOf(out, sid)
			for _, want := range wants {
				if !strings.Contains(line, want) {
					t.Fatalf("state %q: %s lacks %q: %s", state, sid, want, line)
				}
			}
		}
		if !strings.Contains(out, "conclusion: 修复登录页：已合并并验证") {
			t.Fatalf("state %q: an ended Agent's conclusion is missing:\n%s", state, out)
		}
	}
	// Another conversation (a lead) is told the main conversation's resume, not its own.
	if line := lineOf(s.describeAgents(context.Background(), "", "", ""), "exit0001"); !strings.Contains(line,
		"reuse=the main conversation's agent_send resume=true, or the owner's Resume (stop=exit)") {
		t.Fatalf("non-main reuse hint: %s", line)
	}
}

// Each reuse group is bounded, newest first, and says how to list the rest; with nothing at all the answer says so.
func TestAgentsListDefaultBoundsTheReuseGroups(t *testing.T) {
	s, f := newQuietService(t)
	if out, err := runJSON(t, s, "conv1", "agents_list", map[string]any{}); err != nil || !strings.Contains(out, "No agents match.") {
		t.Fatalf("empty list: %v\n%s", err, out)
	}
	now := time.Now()
	var rows []proto.Session
	for i := range agentsIdleShown + 5 {
		c := boxAgent(fmt.Sprintf("idle%04d", i))
		c.UpdatedAt = now.Add(-time.Duration(i) * time.Minute)
		rows = append(rows, c)
	}
	for i := range agentsEndedShown + 3 {
		c := endedBoxAgent(fmt.Sprintf("ended%03d", i), proto.StopUnknown, true)
		c.UpdatedAt = now.Add(-time.Duration(i+1) * time.Hour / 2)
		rows = append(rows, c)
	}
	f.setSessions("box", rows...)
	out, err := runJSON(t, s, "conv1", "agents_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Active (needs you, working, finished unread):\n- none\n") ||
		!strings.Contains(out, "(5 more idle: agents_list state=idle)") ||
		!strings.Contains(out, "(3 more ended in the last 24 hours: agents_list state=ended)") {
		t.Fatalf("bounds:\n%s", out)
	}
	if !strings.Contains(out, "session_id=idle0000 ") || strings.Contains(out, fmt.Sprintf("session_id=idle%04d ", agentsIdleShown)) ||
		!strings.Contains(out, "session_id=ended000 ") || strings.Contains(out, fmt.Sprintf("session_id=ended%03d ", agentsEndedShown)) {
		t.Fatalf("not the newest ones:\n%s", out)
	}
	ended, err := runJSON(t, s, "conv1", "agents_list", map[string]any{"state": "ended"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ended, "idle0000") || !strings.Contains(ended, fmt.Sprintf("session_id=ended%03d ", agentsEndedShown+2)) ||
		strings.Contains(ended, "Ended in the last") {
		t.Fatalf("state=ended:\n%s", ended)
	}
}

// state=all puts what goes on first (needs you, working, finished unread), then every other row by its latest update:
// an old stopped session no longer ranks with the ones that need the user, nor pushes the recent ones out.
func TestAgentsListAllPutsWhatGoesOnFirstThenTheLatest(t *testing.T) {
	s, f := newQuietService(t)
	now := time.Now()
	var rows []proto.Session
	for i := range agentsListShown {
		c := endedBoxAgent(fmt.Sprintf("hist%04d", i), proto.StopUnknown, true)
		c.UpdatedAt = now.Add(-48*time.Hour - time.Duration(i)*time.Minute)
		rows = append(rows, c)
	}
	needs := boxAgent("need0001")
	needs.Status, needs.State, needs.UpdatedAt = proto.StatusBlocked, proto.StateNeedsYou, now.Add(-5*time.Hour)
	working := boxAgent("work0001")
	working.Status, working.State, working.UpdatedAt = proto.StatusWorking, proto.StateWorking, now.Add(-4*time.Hour)
	recent := endedBoxAgent("rcnt0001", proto.StopExit, true)
	recent.UpdatedAt = now.Add(-10 * time.Minute)
	idle := boxAgent("idle0001")
	idle.UpdatedAt = now.Add(-time.Hour)
	f.setSessions("box", append(rows, idle, recent, working, needs)...)
	out, err := runJSON(t, s, "conv1", "agents_list", map[string]any{"state": "all"})
	if err != nil {
		t.Fatal(err)
	}
	order := []string{"need0001", "work0001", "rcnt0001", "idle0001", "hist0000"}
	last := -1
	for _, sid := range order {
		at := strings.Index(out, "session_id="+sid+" ")
		if at <= last {
			t.Fatalf("%s out of order (want %v):\n%s", sid, order, out)
		}
		last = at
	}
	if strings.Contains(out, fmt.Sprintf("session_id=hist%04d ", agentsListShown-1)) || !strings.Contains(out, "(4 more not shown") {
		t.Fatalf("the oldest were not the ones left out:\n%s", out)
	}
}

// The main conversation acts for the owner: agent_send with resume brings back an Agent that ended without an owner's
// Stop (told to quit, or on its own) in its own context, through the node's guarded resume for the owner
// (resume_delegated). A plain message is refused with how to resume it; an owner's Stop, another conversation and a
// node too old for it are refused with nothing started or sent.
func TestMainConversationResumesAnAgentThatEndedWithoutAnOwnersStop(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  proto.Session
	}{
		{"told to quit", endedBoxAgent("exit0001", proto.StopExit, true)},
		{"ended on its own", endedBoxAgent("exit0001", proto.StopUnknown, true)},
		{"read-only row", func() proto.Session {
			c := stoppedBoxAgent("exit0001")
			c.Capabilities.StopCause, c.Capabilities.DelegatedResume = proto.StopUnknown, true
			return c
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newQuietService(t)
			f.setSessions("box", tc.row)
			resumingNode(f)
			if _, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "exit0001",
				"text": "接着补上登录页的回归测试"}); err == nil || !strings.Contains(err.Error(), "resume true") {
				t.Fatalf("plain message: %v", err)
			}
			res, err := runJSON(t, s, "conv1", "agent_send", map[string]any{"machine": "box", "session_id": "exit0001",
				"text": "接着补上登录页的回归测试", "resume": true, "resume_why": "它刚修完登录页，有现场上下文",
				"task": map[string]any{"title": "补回归测试", "done_when": []string{"回归测试通过"}}})
			if err != nil {
				t.Fatal(err)
			}
			starts := startCalls(f)
			if len(starts) != 1 || starts[0]["resume_session_id"] != "exit0001" || starts[0]["resume_automatic"] != true ||
				starts[0]["resume_delegated"] != true || starts[0]["prompt"] != "接着补上登录页的回归测试" {
				t.Fatalf("session.start: %v", starts)
			}
			if len(sentToAgent(f)) != 0 || !strings.Contains(res, "Resumed exit0001 on box") {
				t.Fatalf("receipt %s, sent %q", res, sentToAgent(f))
			}
		})
	}
	for _, tc := range []struct {
		name, conv string
		row        proto.Session
		want       string
	}{
		// A node lists an owner's Stop and a cause it does not know read-only (no send), as stoppedBoxAgent does.
		{"owner's stop", "conv1", stoppedWith("exit0001", proto.StopUser), "stopped by the owner"},
		{"another conversation", "conv2", endedBoxAgent("exit0001", proto.StopExit, true), "only the main conversation's own agent_send may resume it"},
		{"node too old", "conv1", endedBoxAgent("exit0001", proto.StopExit, false), "node on box is older"},
		{"unknown future cause", "conv1", stoppedWith("exit0001", proto.StopCause("future")), "cannot be resumed by the assistant"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := newQuietService(t)
			f.setSessions("box", tc.row)
			resumingNode(f)
			_, err := runJSON(t, s, tc.conv, "agent_send", map[string]any{"machine": "box", "session_id": "exit0001",
				"text": "go on", "resume": true, "resume_why": "needs its context", "task": map[string]any{"title": "接着做"}})
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Nothing was started or sent") {
				t.Fatalf("refusal: %v", err)
			}
			if len(startCalls(f)) != 0 || len(sentToAgent(f)) != 0 {
				t.Fatalf("started %v, sent %q", startCalls(f), sentToAgent(f))
			}
		})
	}
}

// The automatic flows that resume (rework, routed PR events, inbox forwards) keep to recorded non-user stops even in
// the main conversation: only its own agent_send resumes for the owner.
func TestAutomaticResumeKeepsToRecordedNonUserStops(t *testing.T) {
	s, f := newQuietService(t)
	row := endedBoxAgent("exit0001", proto.StopExit, true)
	f.setSessions("box", row)
	onBoxTask(t, s, "exit0001", "conv1", false, "it is done")
	if err := s.resumeAllowed(t.Context(), "conv1", Device{ID: "box", Name: "box"}, row, "rework", nil); err == nil ||
		!strings.Contains(err.Error(), "only the main conversation's own agent_send") {
		t.Fatalf("automatic resume of an exit: %v", err)
	}
}
