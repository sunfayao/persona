package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/devicewire"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"github.com/sunfayao/persona/server/internal/textfmt"
	"github.com/sunfayao/persona/server/internal/transcript"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

const a2aConfirmFor = 10 * time.Minute

func DeliveryMethod(method string) bool {
	return method == "session.a2a.send" || method == "session.a2a.wake"
}

func (s *Domain) RunDelivery(ctx context.Context) {
	tick, stop := s.clock.Ticker(time.Second)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick:
		case <-s.wake:
		}
		cfg := s.config(ctx)
		if cfg.Machines != nil && len(cfg.Machines) == 0 {
			continue
		}
		st, err := s.store()
		if err != nil {
			continue
		}
		rows, err := st.A2AClaim(ctx, 12)
		if err != nil {
			s.logf("A2A delivery: %v", err)
			continue
		}
		var workers sync.WaitGroup
		for _, r := range rows {
			workers.Go(func() {
				ctx, cancel := s.clock.WithTimeout(ctx, 6*time.Second)
				defer cancel()
				t, err := DecodeTask(&r, nil)
				if err == nil {
					err = s.process(ctx, st, cfg, t)
				}
				if err != nil && !errors.Is(err, errA2AConflict) && ctx.Err() == nil {
					s.logf("A2A %s: %v", r.ID, err)
				}
			})
		}
		workers.Wait()
	}
}

// ProcessTask reconciles a single persisted task against its current endpoints.
func (s *Domain) ProcessTask(ctx context.Context, id string) error {
	st, err := s.store()
	if err != nil {
		return err
	}
	task, err := DecodeTask(st.A2AGet(ctx, id))
	if err != nil {
		return err
	}
	if task == nil {
		return errA2ANotFound
	}
	return s.process(ctx, st, s.config(ctx), task)
}

func (s *Domain) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func a2aDeliveryID(task string, seq int) string {
	return fmt.Sprintf("%s%s-%d", proto.A2ADeliveryPrefix, task, seq)
}

func (s *Domain) a2aItems(ctx context.Context, ref Ref) ([]proto.Item, error) {
	var res struct{ Items []proto.Item }
	err := s.nodes.Call(ctx, ref.Machine, assistantwire.DeviceSessionItems, map[string]any{"session_id": ref.Session, "turns": 4, "lazy_images": true}, &res)
	return res.Items, err
}

type a2aTurnView struct {
	echo, current, closed, interrupted bool
	reply                              string
}

// a2aTurn binds only the exchange after the exact input. A completed older
// exchange can be recovered after restart if the transcript closed that turn.
func a2aTurn(items []proto.Item, id string) a2aTurnView {
	var out a2aTurnView
	start := -1
	for i, item := range items {
		user := item.Role == "user" && item.Kind != proto.KindToolResult
		if user || item.Wake || item.Injected != nil {
			if user && (item.ClientMsgID == id || prompts.RelayMessageID(item.Text) == id) {
				start = i
			} else if start != -1 {
				return a2aReadTurn(items[start:i], false)
			}
		}
	}
	if start != -1 {
		out = a2aReadTurn(items[start:], true)
	}
	return out
}

func a2aReadTurn(items []proto.Item, current bool) a2aTurnView {
	end, closed := transcript.TurnEnd(items)
	out := a2aTurnView{echo: true, current: current, closed: closed}
	for _, item := range items {
		if item.Role == "assistant" && item.Kind == proto.KindText && !item.Narration && item.Text != "" {
			out.reply = a2aCleanText(item.Text)
		}
		if closed && item.ID == end.Key && turnmodel.IsInterruptedNotice(item) {
			out.interrupted = true
		}
	}
	return out
}

func (s *Domain) process(ctx context.Context, st Store, cfg assistantwire.A2AConfig, t *Task) error {
	if t.SourceSession != "" && !s.a2aAllowed(ctx, cfg, t.source()) {
		if !a2aTerminal(t.State) && s.a2aEndpointClosed(ctx, cfg, t.source()) {
			s.fail(t, "The sending session was closed.", false)
			return s.a2aSave(ctx, st, t)
		}
		return nil
	}
	if !s.a2aAllowed(ctx, cfg, t.target()) {
		if !t.TargetClosed && s.a2aEndpointClosed(ctx, cfg, t.target()) {
			t.TargetClosed = true
			if !a2aTerminal(t.State) {
				s.fail(t, "The receiving session was closed.", true)
			}
			return s.a2aSave(ctx, st, t)
		}
		if !t.TargetClosed || !s.a2aMachineAllowed(ctx, cfg, t.TargetMachine) {
			return nil
		}
	}
	if t.State == a2aQueued {
		return nil
	}
	machine := s.nodes.ExecutionMachine(ctx, t.target())
	offline := s.machines.Offline(machine)
	if !a2aTerminal(t.State) && offline != t.Disconnected {
		t.Disconnected = offline
		if len(t.Events) < a2aMaxEvents/4 {
			e := Event{ID: s.newID(), Kind: "connection", Text: "The receiving computer reconnected."}
			if offline {
				e.Text = "The receiving computer disconnected. The task will be checked when it returns."
			}
			s.appendEvent(t, e)
		}
		if err := s.a2aSave(ctx, st, t); err != nil {
			return err
		}
	}
	if !offline && t.Launch != nil && t.Launch.Pending && !a2aTerminal(t.State) {
		return s.deliverLaunch(ctx, st, t)
	}
	for i, e := range t.Events {
		if !a2aActiveDelivery(e) {
			continue
		}
		to, from := t.target(), t.source()
		if offline && e.Kind == "message" {
			if changed, err := s.a2aExpireDelivery(ctx, st, t, i); changed || err != nil {
				return err
			}
			continue
		}
		if e.Kind != "message" {
			to, from = from, to
		}
		changed, err := s.a2aDeliver(ctx, st, t, i, to, from)
		if err != nil || changed || e.Kind == "message" {
			return err
		}
	}
	if a2aTerminal(t.State) {
		if !slices.ContainsFunc(t.Events, a2aActiveDelivery) {
			return s.a2aSave(ctx, st, t)
		}
		return nil
	}
	if t.State == a2aInput && t.SourceSession != "" {
		return s.a2aObserveAnswer(ctx, st, t)
	}
	if offline {
		return nil
	}
	return s.a2aObserve(ctx, st, t)
}

func (s *Domain) a2aEndpointClosed(ctx context.Context, cfg assistantwire.A2AConfig, ref Ref) bool {
	if !s.a2aMachineAllowed(ctx, cfg, ref.Machine) {
		return false
	}
	if s.visibility.Deleted(ctx, ref) {
		return true
	}
	if s.visibility.Archived(ctx, ref) {
		return true
	}
	row, ok, err := s.nodes.Session(ctx, ref.Machine, ref.Session)
	return err == nil && ok && (row.Archived || row.IsDeleted() || row.HiddenByDirectory() || s.visibility.HiddenByDirectory(ref.Machine, row))
}

func (s *Domain) a2aExpireDelivery(ctx context.Context, st Store, t *Task, i int) (bool, error) {
	e := &t.Events[i]
	if e.Delivery == a2aQueued || e.Attempted.IsZero() || s.clock.Now().Sub(e.Attempted) < a2aConfirmFor {
		return false, nil
	}
	if e.Kind == "message" && !a2aTerminal(t.State) {
		s.fail(t, "Persona could not confirm delivery of the request.", true)
	} else {
		e.Delivery = a2aExpired
	}
	keep, cancel := s.clock.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return true, s.a2aSave(keep, st, t)
}

func (s *Domain) fail(t *Task, message string, wake bool) {
	for i := range t.Events {
		switch t.Events[i].Delivery {
		case a2aQueued:
			t.Events[i].Delivery = a2aDiscarded
		case a2aSending, a2aUncertain:
			t.Events[i].Delivery = a2aExpired
		}
	}
	if t.Launch != nil {
		t.Launch.Pending = false
	}
	e := Event{ID: s.newID(), Kind: "status", State: a2aFailed, Text: message}
	if wake {
		e = a2aReturn(t, e)
	}
	s.appendEvent(t, e)
}

// A text-only parent can answer the latest question in the turn that received
// it. The event ID makes recovery idempotent across Manager restarts.
func (s *Domain) a2aObserveAnswer(ctx context.Context, st Store, t *Task) error {
	idx := len(t.Events) - 1
	for idx >= 0 && t.Events[idx].State != a2aInput {
		idx--
	}
	if idx < 0 || t.Events[idx].Delivery != a2aDelivered {
		return nil
	}
	sess, ok, err := s.nodes.Session(ctx, t.SourceMachine, t.SourceSession)
	if err != nil || !ok || !a2aIdle(sess) {
		return nil
	}
	question := t.Events[idx]
	items, err := s.a2aItems(ctx, t.source())
	if err != nil {
		return nil
	}
	turn := a2aTurn(items, a2aDeliveryID(t.ID, question.Seq))
	if !turn.echo || turn.interrupted || !turn.current && !turn.closed {
		return nil
	}
	answer, ok := a2aReplyBody(turn.reply, "A2A "+t.ID+" answer")
	if !ok || s.outbound.AllowSend(ctx, t.source(), t.target(), answer) != nil {
		return nil
	}
	changed, err := s.a2aContinue(t, fmt.Sprintf("persona-answer-%d", question.Seq), answer)
	if err != nil || !changed {
		return err
	}
	return s.a2aSave(ctx, st, t)
}

func (s *Domain) a2aDeliver(ctx context.Context, st Store, t *Task, i int, to, from Ref) (bool, error) {
	e := &t.Events[i]
	id := a2aDeliveryID(t.ID, e.Seq)
	if e.Delivery != a2aQueued {
		if items, err := s.a2aItems(ctx, to); err == nil && a2aTurn(items, id).echo {
			return true, s.a2aReceipt(ctx, st, t.ID, e.Seq, a2aDelivered)
		}
		var status proto.SessionEchoStatus
		if err := s.nodes.Call(ctx, to.Machine, proto.MethodSessionEchoStatus, map[string]string{"session_id": to.Session, "client_msg_id": id}, &status); err == nil {
			if status.State == proto.EchoSeen {
				return true, s.a2aReceipt(ctx, st, t.ID, e.Seq, a2aDelivered)
			}
		}
		if changed, err := s.a2aExpireDelivery(ctx, st, t, i); changed || err != nil {
			return changed, err
		}
		if e.Delivery == a2aSending && s.clock.Now().Sub(e.Attempted) > 15*time.Second {
			return true, s.a2aReceipt(ctx, st, t.ID, e.Seq, a2aUncertain)
		}
		return false, nil
	}
	sess, ok, err := s.nodes.Session(ctx, to.Machine, to.Session)
	if err != nil || !ok {
		return false, nil
	}
	resume := e.Kind != "message" && s.a2aResumeAllowed(sess)
	if !resume && (!s.a2aSendable(sess) || !a2aIdle(sess)) {
		return false, nil
	}
	if device, remote := strings.CutPrefix(t.Owner, "device:"); remote && e.Kind == "message" {
		revoked, err := s.outbound.CredentialRevoked(ctx, device)
		if err != nil {
			return false, err
		}
		if revoked {
			e.Delivery = a2aDiscarded
			s.appendEvent(t, Event{ID: s.newID(), Kind: "status", State: a2aCanceled})
			return true, s.a2aSave(ctx, st, t)
		}
	}
	entry := s.a2aSessionEntry(ctx, to.Machine, sess) // asked before the send is marked as under way
	e.Delivery, e.Attempted = a2aSending, s.clock.Now().UTC()
	if err := s.a2aSave(ctx, st, t); err != nil {
		return false, err
	}
	text := a2aPrompt(t, e, from, string(s.language(ctx)), entry)
	params := map[string]any{
		"session_id": to.Session, "text": text, "client_msg_id": id, "message_source": a2aMessageSource(from),
	}
	if resume {
		if sess.Capabilities.A2AWakeOnSend {
			params["a2a_wake_on_send"] = true
			return true, s.a2aSendNode(ctx, st, t.ID, e.Seq, to, "session.a2a.send", params)
		}
		taskID, seq := t.ID, e.Seq
		go func() {
			ctx, cancel := s.clock.WithTimeout(s.runContext(), s.startTimeout())
			defer cancel()
			if err := s.a2aSendNode(ctx, st, taskID, seq, to, "session.a2a.wake", params); err != nil {
				s.logf("A2A wake %s: %v", taskID, err)
			}
			s.Wake()
		}()
		return true, nil
	}
	return true, s.a2aSendNode(ctx, st, t.ID, e.Seq, to, "session.a2a.send", params)
}

func (s *Domain) a2aResumeAllowed(sess proto.Session) bool {
	if sess.Status != proto.StatusOffline || !s.a2aVisible(sess) || sess.Capabilities == nil {
		return false
	}
	if a2aStopped(sess) {
		return false
	}
	return sess.Capabilities.Send && sess.Capabilities.A2AWakeOnSend ||
		sess.Capabilities.StopCause == proto.StopIdle || sess.Capabilities.StopCause == proto.StopRestart
}

func (s *Domain) a2aSendNode(ctx context.Context, st Store, taskID string, seq int, to Ref, method string, params map[string]any) error {
	var out struct {
		Queued  bool
		Session proto.Session
	}
	err := s.nodes.Call(ctx, to.Machine, method, params, &out)
	receipt := a2aDelivered
	switch {
	case err != nil && s.a2aNoDelivery(err):
		receipt = a2aQueued
	case err != nil:
		receipt = a2aUncertain
	case out.Queued:
		receipt = a2aSending
	case method == "session.a2a.wake" && (out.Session.SessionID != to.Session || out.Session.StartError != ""):
		receipt = a2aUncertain
	}
	keep, cancel := s.clock.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	return s.a2aReceipt(keep, st, taskID, seq, receipt)
}

func a2aMessageSource(from Ref) *proto.MessageSource {
	fromName := from.Machine + "/" + from.Session
	if from.Session == "" {
		fromName = "A2A client"
	}
	return &proto.MessageSource{Kind: proto.SourceConversation, ConversationID: from.Session, Title: fromName}
}

// a2aPrompt is event e of task t from from as its recipient receives it, in language, saying how a recipient with
// entry reaches the A2A tools (a2a_entry.go).
func a2aPrompt(t *Task, e *Event, from Ref, language string, entry a2aEntry) string {
	m := a2aTaskMessage(t, e, from, language)
	entry.lay(&m)
	return a2aRelay(m, from, a2aDeliveryID(t.ID, e.Seq))
}

// Reserve a launch before handing it to a node. An ambiguous startup stays
// discoverable by its reserved session ID and is never started a second time.
func (s *Domain) deliverLaunch(ctx context.Context, st Store, t *Task) error {
	e := &t.Events[0]
	if e.Delivery != a2aQueued {
		if e.Delivery == a2aSending && s.clock.Now().Sub(e.Attempted) <= s.startTimeout() {
			return nil
		}
		if sess, ok, _ := s.nodes.Session(ctx, t.TargetMachine, t.TargetSession); ok {
			return s.a2aLaunched(ctx, st, t, sess, sess.StartError != "", sess.StartError)
		}
		if s.clock.Now().Sub(e.Attempted) > s.startTimeout()+time.Minute {
			return s.a2aLaunched(ctx, st, t, proto.Session{}, true, "") // no answer and no session: unknown
		}
		return nil
	}
	dev, ok := s.machines.Resolve(ctx, t.TargetMachine)
	if !ok || !dev.Online && !dev.Reachable {
		return nil
	}
	e.Delivery, e.Attempted = a2aSending, s.clock.Now().UTC()
	if err := s.a2aSave(ctx, st, t); err != nil {
		return err
	}
	taskID, mid, sid, source := t.ID, t.TargetMachine, t.TargetSession, t.source()
	// The first message is written once the launcher is known (a2aLaunchEntry), from a copy taken here: the task
	// itself is not read again until the start's outcome is recorded.
	message := a2aTaskMessage(t, e, source, string(s.language(ctx)))
	request := map[string]any{"cwd": t.Launch.CWD, "launcher": t.Launch.Launcher, "session_id": sid,
		"client_msg_id": a2aDeliveryID(taskID, 1), "message_source": a2aMessageSource(source), "permission_mode": "plan"}
	t.Launch.Run.Apply(request) // a tier's launcher, model, effort and speed; never its permission mode (agent_presets.go)
	go func() {
		ctx, cancel := s.clock.WithTimeout(s.runContext(), s.startTimeout())
		defer cancel()
		var out struct{ Session proto.Session }
		err := s.launches.ApplyModel(ctx, request)
		companion := false
		if err == nil {
			// Where it starts is decided as it goes out, with what the computer runs now (start_host.go): an agy
			// launcher on its companion, and a read-only start a pane cannot hold refused here, not by the node.
			companion, err = s.launches.SelectHost(ctx, dev, request)
		}
		definite := err != nil
		if err == nil {
			launcher, _ := request["launcher"].(string)
			s.a2aLaunchEntry(ctx, mid, launcher).lay(&message)
			request["prompt"] = a2aRelay(message, source, a2aDeliveryID(taskID, 1))
			start := func() error { return s.nodes.Call(ctx, mid, assistantwire.DeviceSessionStart, request, &out) }
			err = start()
			if s.launches.CompanionAway(err, companion) && request["permission_mode"] != "plan" {
				// The companion went away before the start reached it: nothing ran, and a writable start opens in the
				// default host. A read-only one fails with the node's reason: no pane holds it read-only.
				delete(request, "host")
				s.logf("A2A launch %s: the Antigravity companion is not connected on %s; starting in its default host", taskID, mid)
				err = start()
			}
			definite = err != nil && !s.launches.StartUnknown(err)
		}
		keep, stop := s.clock.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		if out.Session.SessionID == "" && !definite {
			_ = s.a2aReceipt(keep, st, taskID, 1, a2aUncertain)
			return
		}
		why := ""
		if definite {
			why = err.Error()
		}
		t, readErr := DecodeTask(st.A2AGet(keep, taskID))
		if readErr == nil && t != nil && t.Launch.Pending && !a2aTerminal(t.State) {
			if err := s.a2aLaunched(keep, st, t, out.Session, definite, why); err != nil {
				s.logf("A2A launch %s: %v", taskID, err)
			}
		}
		s.Wake()
	}()
	return nil
}

// a2aLaunchCheck refuses a new a2a_send launch on computer to.Machine before anything is asked or recorded when it
// would start read-only where its launcher cannot be held read-only (agentStartedHost, on the start as Launch will
// send it). A launch a2aStart refuses anyway is not looked at, and neither is one whose launcher Model configuration
// cannot give now: Launch records why that one did not start. A retry by message_id is not checked again:
// a2aStart answers it.
func (s *Domain) a2aLaunchCheck(ctx context.Context, cfg assistantwire.A2AConfig, to Ref, contextID string, launch *Launch) error {
	dev, ok := s.machines.Resolve(ctx, to.Machine)
	if to.Session != "" || contextID != "" || !ok || dev.ID != to.Machine || !s.a2aMachineAllowed(ctx, cfg, to.Machine) ||
		!s.a2aLaunchOn(ctx, cfg) {
		return nil // a2aStart refuses it
	}
	probe := map[string]any{"launcher": launch.Launcher, "permission_mode": "plan"}
	launch.Run.Apply(probe) // its tier's launcher, as Launch lays it
	if s.launches.ApplyModel(ctx, probe) != nil {
		return nil
	}
	_, err := s.launches.SelectHost(ctx, dev, probe)
	return err
}

// a2aNotStarted is the failed status of a launch that did not start: why, when that is known (the node's or the
// manager's refusal, or the error its launcher stopped with), so the calling Agent can start it another way. Only a
// start whose outcome is unknown says that Persona could not confirm it.
func a2aNotStarted(why string) string {
	if why = textfmt.OneLine(why); why == "" {
		return "Persona could not confirm the Agent started."
	}
	return "The Agent did not start: " + textfmt.Clip(why, 1000)
}

// a2aLaunched records how t's launch ended: session started, or the launch failed. why is what a definite failure
// gives as its reason (a2aNotStarted), "" for a start whose outcome is unknown.
func (s *Domain) a2aLaunched(ctx context.Context, st Store, t *Task, session proto.Session, failed bool, why string) error {
	t.Launch.Pending = false
	if failed {
		t.Events[0].Delivery = a2aDiscarded
		s.appendEvent(t, a2aReturn(t, Event{ID: s.newID(), Kind: "status", State: a2aFailed, Text: a2aNotStarted(why)}))
	} else {
		t.TargetSession = session.SessionID
		if t.Launch.Branch != "" {
			t.Launch.Worktree = session.Cwd
		}
		// The first prompt may still be queued. Observe its exact message
		// before treating the child as running.
		if t.Events[0].Delivery != a2aDelivered {
			t.Events[0].Delivery = a2aUncertain
		}
	}
	return s.a2aSave(ctx, st, t)
}

func (s *Domain) a2aNoDelivery(err error) bool {
	if errors.Is(err, apierr.ErrOffline) || errors.Is(err, assistantwire.ErrNoDevice) || errors.Is(err, rpc.ErrNotSent) {
		return true
	}
	var re *rpc.Error
	if !errors.As(err, &re) {
		return false
	}
	return s.nodes.Delivered(re) == "none" || re.Code == devicewire.CodeMachineOffline || re.Code == devicewire.CodeNoDevice ||
		re.Code == "a2a_session_busy" || re.Code == proto.CodeAgentStopped
}

func a2aIdle(sess proto.Session) bool {
	return !sess.Starting && len(sess.Queued) == 0 && (sess.Status == proto.StatusIdle || sess.Status == proto.StatusDone)
}

func (s *Domain) a2aReceipt(ctx context.Context, st Store, id string, seq int, receipt string) error {
	for range 5 {
		t, err := DecodeTask(st.A2AGet(ctx, id))
		if err != nil || t == nil {
			return err
		}
		e := &t.Events[seq-1]
		if !a2aActiveDelivery(*e) || e.Delivery == receipt {
			return nil
		}
		e.Delivery = receipt
		if receipt == a2aDelivered && e.Kind == "message" && t.State == a2aSubmitted {
			s.appendEvent(t, Event{ID: s.newID(), Kind: "status", State: a2aWorking})
		}
		if err := s.a2aSave(ctx, st, t); !errors.Is(err, errA2AConflict) {
			return err
		}
	}
	return errA2AConflict
}

func (s *Domain) a2aObserve(ctx context.Context, st Store, t *Task) error {
	if t.State != a2aWorking && t.State != a2aAuth {
		return nil
	}
	idx := len(t.Events) - 1
	for idx >= 0 && t.Events[idx].Kind != "message" {
		idx--
	}
	if idx < 0 {
		return nil
	}
	message := t.Events[idx]
	sess, ok, err := s.nodes.Session(ctx, t.TargetMachine, t.TargetSession)
	if err != nil || !ok {
		return nil
	}
	items, err := s.a2aItems(ctx, t.target())
	if err != nil {
		return nil
	}
	turn := a2aTurn(items, a2aDeliveryID(t.ID, message.Seq))
	var state, text string
	switch {
	case (!turn.echo || !turn.closed && (!turn.current || turn.reply == "")) &&
		a2aIdle(sess) && s.clock.Now().Sub(message.Attempted) > a2aConfirmFor:
		state, text = a2aFailed, "Persona could not find the receiving session's finished task turn."
	case !turn.echo:
		return nil
	case len(t.Events) >= a2aMaxEvents-1:
		state, text = a2aFailed, "This task reached its event limit."
	case turn.interrupted:
		state, text = a2aFailed, "The receiving session was interrupted."
	case turn.closed && !turn.current && turn.reply != "":
		state, text = a2aFinalReply(t.ID, turn.reply)
	case !turn.current:
		return nil
	case sess.Status == proto.StatusBlocked && t.State == a2aWorking:
		state, text = a2aAuth, "The receiving session is waiting for an interaction in Persona."
	case sess.Status == proto.StatusInterrupted:
		state, text = a2aFailed, "The receiving session was interrupted."
	case sess.Status == proto.StatusOffline && a2aStopped(sess):
		state, text = a2aFailed, "The receiving session stopped before finishing the task."
	case a2aIdle(sess) && sess.FailureReason != "":
		state, text = a2aFailed, "The receiving session reported an execution failure."
	case a2aIdle(sess) && turn.reply != "":
		state, text = a2aFinalReply(t.ID, turn.reply)
	case a2aIdle(sess) && turn.closed:
		state, text = a2aFailed, "The receiving session ended its turn without a result."
	case sess.Status == proto.StatusWorking && t.State == a2aAuth:
		state, text = a2aWorking, "The receiving session resumed work."
	default:
		return nil
	}
	s.appendEvent(t, a2aReturn(t, Event{ID: s.newID(), Kind: "status", State: state, Text: text}))
	return s.a2aSave(ctx, st, t)
}

// Text-only harnesses can return a question or failure in the exact turn
// that received the task. A marker from any other turn is ordinary text.
func a2aFinalReply(taskID, reply string) (string, string) {
	for _, state := range []string{a2aInput, a2aFailed} {
		if body, ok := a2aReplyBody(reply, "A2A "+taskID+" "+state); ok {
			return state, body
		}
	}
	return a2aComplete, reply
}

func a2aReplyBody(reply, marker string) (string, bool) {
	head, body, hasBody := strings.Cut(reply, "\n")
	body = strings.TrimSpace(body)
	return body, hasBody && head == marker && body != ""
}

func a2aStopped(sess proto.Session) bool {
	if sess.Capabilities == nil {
		return false
	}
	switch sess.Capabilities.StopCause {
	case proto.StopUser, proto.StopExit, proto.StopResumeFailed, proto.StopCompactFailed:
		return true
	}
	return false
}
