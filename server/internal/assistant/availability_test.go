package assistant

import (
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/proto"
)

// availEvents are the details of the availability events of kind the service recorded, oldest first.
func availEvents(s *Service, kind string) []map[string]any {
	var out []map[string]any
	for _, e := range testEvents(s) {
		if e.Kind == kind {
			out = append(out, e.Detail)
		}
	}
	return out
}

// availWaitTimes is the timestamps of the wait for conv on mac.
func availWaitTimes(s *Service) (at, ended time.Time) {
	s.avail.mu.Lock()
	defer s.avail.mu.Unlock()
	w := s.avail.waits[[2]string{"mac", "conv"}]
	return w.at, w.ended
}

// availAfter waits until after until, as the old fixed sleep did, without assuming how long scheduling takes.
func availAfter(t *testing.T, until time.Time) {
	t.Helper()
	waitUntil(t, func() bool { return time.Now().After(until) })
}

// The owner's message to an idle conversation: the wait runs to the first change of the conversation's last text,
// and the round it started is the owner's. A supervision round after it is a background round; a message that comes
// during it is counted as having come on a background round, and is answered by the text that follows once nothing
// of the owner's is queued any more.
func TestAvailabilityReplyWaitAndRounds(t *testing.T) {
	ft := newFleetTest(t)
	ft.f.sessions["mac"], ft.f.known["conv"] = nil, true // the fake hub answers the sends to it
	s := ft.s
	m, _ := s.fleetMachine("mac")
	conv := proto.Session{SessionID: "conv", Agent: "claude", Cwd: m.assistantCwd(), Status: proto.StatusIdle, LastText: "earlier"}
	ref := SessionRef{MachineID: "mac", SessionID: "conv"}
	list := func(status, text string, queued ...string) {
		c := conv
		c.Status, c.LastText, c.Queued = status, text, queued
		ft.sessions("mac", c)
	}
	list(proto.StatusIdle, "earlier")

	s.UserSpoke(t.Context(), ref)
	list(proto.StatusWorking, "earlier")
	if got := availEvents(s, replyWaitEvent); len(got) != 0 {
		t.Fatalf("a reply before any text: %v", got)
	}
	list(proto.StatusWorking, "Looking.")
	waits := availEvents(s, replyWaitEvent)
	if len(waits) != 1 || waits[0]["state"] != waitIdle || waits[0]["unanswered"] != nil {
		t.Fatalf("reply waits %v", waits)
	}
	list(proto.StatusWorking, "Done.") // more text of the same reply: not another wait
	list(proto.StatusDone, "Done.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 1 })
	if r := availEvents(s, roundEvent)[0]; r["source"] != roundOwner {
		t.Fatalf("round %v", r)
	}
	if got := availEvents(s, replyWaitEvent); len(got) != 1 {
		t.Fatalf("the round's end counted the answered message again: %v", got)
	}

	// A supervision message starts the next round; the owner writes while it runs.
	if err := s.deliverRelay(t.Context(), "mac", "conv", relayMsg{Tool: superviseTool, Text: "Synthetic supervision."}); err != nil {
		t.Fatal(err)
	}
	list(proto.StatusWorking, "Done.")
	waitUntil(t, func() bool {
		s.avail.mu.Lock()
		defer s.avail.mu.Unlock()
		return s.avail.rounds[[2]string{"mac", "conv"}].source == proto.SourceSupervision
	})
	s.UserSpoke(t.Context(), ref)
	list(proto.StatusWorking, "Checking the PR.", "my question") // the round's own text: the owner's message still waits
	if got := availEvents(s, replyWaitEvent); len(got) != 1 {
		t.Fatalf("the background round's text counted as the reply: %v", got)
	}
	list(proto.StatusWorking, "Here is the answer.")
	waits = availEvents(s, replyWaitEvent)
	if len(waits) != 2 || waits[1]["state"] != waitBackground {
		t.Fatalf("reply waits %v", waits)
	}
	list(proto.StatusDone, "Here is the answer.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 2 })
	if r := availEvents(s, roundEvent)[1]; r["source"] != proto.SourceSupervision {
		t.Fatalf("round %v", r)
	}
}

// A reply whose text the session list never showed changing (the same words as before) is counted when its round
// ends; a message nobody answers is recorded unanswered at the bound instead of waiting for ever.
func TestAvailabilityReplyWaitFallbacks(t *testing.T) {
	ft := newFleetTest(t)
	s := ft.s
	m, _ := s.fleetMachine("mac")
	conv := proto.Session{SessionID: "conv", Agent: "claude", Cwd: m.assistantCwd(), Status: proto.StatusIdle, LastText: "OK"}
	ref := SessionRef{MachineID: "mac", SessionID: "conv"}
	ft.sessions("mac", conv)
	s.UserSpoke(t.Context(), ref)
	conv.Status = proto.StatusWorking
	ft.sessions("mac", conv)
	conv.Status = proto.StatusDone
	ft.sessions("mac", conv)
	waitUntil(t, func() bool { return len(availEvents(s, replyWaitEvent)) == 1 })
	if w := availEvents(s, replyWaitEvent)[0]; w["by"] != "turn_end" || w["state"] != waitIdle {
		t.Fatalf("reply wait %v", w)
	}

	old, oldFor := replyWaitMax, availInputFor
	replyWaitMax = 0
	t.Cleanup(func() { replyWaitMax, availInputFor = old, oldFor })
	s.UserSpoke(t.Context(), ref)
	conv.Status = proto.StatusWorking
	ft.sessions("mac", conv) // the turn started
	at, _ := availWaitTimes(s)
	availAfter(t, at.Add(replyWaitMax))
	ft.sessions("mac", conv) // and wrote nothing
	waits := availEvents(s, replyWaitEvent)
	if len(waits) != 2 || waits[1]["unanswered"] != true || waits[1]["wait"] != 0.0 {
		t.Fatalf("reply waits %v", waits)
	}
	if n := s.avail.waiting.Load(); n != 0 {
		t.Fatalf("%d waits left", n)
	}

	// A message that started no turn (its send was refused) is no wait, not an unanswered one.
	replyWaitMax, availInputFor = old, 0
	conv.Status = proto.StatusInterrupted
	ft.sessions("mac", conv)
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 2 })
	s.UserSpoke(t.Context(), ref)
	at, _ = availWaitTimes(s)
	availAfter(t, at.Add(availInputFor))
	ft.sessions("mac", conv)
	if got := availEvents(s, replyWaitEvent); len(got) != 2 || s.avail.waiting.Load() != 0 {
		t.Fatalf("an undelivered message counted as a wait: %v, %d waiting", got, s.avail.waiting.Load())
	}
}

// availConv is a fleet test with one idle assistant conversation "conv" on "mac": list has its node list it.
func availConv(t *testing.T) (ft *fleetTest, ref SessionRef, list func(status, text string, queued ...string)) {
	ft = newFleetTest(t)
	ft.f.sessions["mac"], ft.f.known["conv"] = nil, true // the fake hub answers the sends to it
	m, _ := ft.s.fleetMachine("mac")
	conv := proto.Session{SessionID: "conv", Agent: "claude", Cwd: m.assistantCwd(), Status: proto.StatusIdle, LastText: "earlier"}
	list = func(status, text string, queued ...string) {
		c := conv
		c.Status, c.LastText, c.Queued = status, text, queued
		ft.sessions("mac", c)
	}
	list(proto.StatusIdle, "earlier")
	return ft, SessionRef{MachineID: "mac", SessionID: "conv"}, list
}

// superviseRound starts a supervision round in the conversation and waits for the book to have it.
func superviseRound(t *testing.T, ft *fleetTest, list func(status, text string, queued ...string), text string) {
	t.Helper()
	if err := ft.s.deliverRelay(t.Context(), "mac", "conv", relayMsg{Tool: superviseTool, Text: "Synthetic supervision."}); err != nil {
		t.Fatal(err)
	}
	list(proto.StatusWorking, text)
	waitUntil(t, func() bool {
		ft.s.avail.mu.Lock()
		defer ft.s.avail.mu.Unlock()
		return ft.s.avail.rounds[[2]string{"mac", "conv"}].source == proto.SourceSupervision
	})
}

// A round that stops without a turn's end (its process went away, the owner pressed stop) is over and recorded as
// stopped: the owner's message hours later comes to an idle conversation and starts a round of their own, not a
// continuation of the old one.
func TestAvailabilityStoppedRound(t *testing.T) {
	ft, ref, list := availConv(t)
	s := ft.s
	superviseRound(t, ft, list, "earlier")
	list(proto.StatusOffline, "earlier")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 1 })
	if r := availEvents(s, roundEvent)[0]; r["source"] != proto.SourceSupervision || r["stopped"] != true {
		t.Fatalf("round %v", r)
	}

	s.UserSpoke(t.Context(), ref)
	list(proto.StatusWorking, "Here.") // resumed: offline to working
	waits := availEvents(s, replyWaitEvent)
	if len(waits) != 1 || waits[0]["state"] != waitIdle {
		t.Fatalf("reply waits %v", waits)
	}
	list(proto.StatusDone, "Here.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 2 })
	if r := availEvents(s, roundEvent)[1]; r["source"] != roundOwner || r["stopped"] != nil || r["seconds"].(float64) > 60 {
		t.Fatalf("round %v", r)
	}

	// A round left in the book whose stop was never seen (the list was lost with its node) does not make the
	// conversation busy, and the round the owner's message starts replaces it.
	s.avail.mu.Lock()
	s.avail.rounds[[2]string{"mac", "conv"}] = availRound{source: proto.SourceSupervision, started: time.Now().Add(-5 * time.Hour)}
	s.avail.mu.Unlock()
	list(proto.StatusOffline, "Here.")
	s.UserSpoke(t.Context(), ref)
	list(proto.StatusWorking, "Again.")
	if waits = availEvents(s, replyWaitEvent); len(waits) != 2 || waits[1]["state"] != waitIdle {
		t.Fatalf("reply waits %v", waits)
	}
	list(proto.StatusDone, "Again.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 3 })
	if r := availEvents(s, roundEvent)[2]; r["source"] != roundOwner || r["seconds"].(float64) > 60 {
		t.Fatalf("round %v", r)
	}
}

// A conversation whose node lists no queue (Codex, a runtime, an older Claude Code): the text a background round
// writes after the owner's message is that round's own, not the reply. A round following at once is the owner's and
// its first text the reply, however long before the round's end the message came; with no such round the message
// was taken into the running one, and that round's end stands in for the reply.
func TestAvailabilityReplyWaitWithoutQueue(t *testing.T) {
	ft, ref, list := availConv(t)
	s := ft.s
	oldFor := availInputFor
	t.Cleanup(func() { availInputFor = oldFor })
	superviseRound(t, ft, list, "earlier")
	availInputFor = time.Millisecond // only the owner's input is rushed; expiring supervision's input would misattribute its round
	s.UserSpoke(t.Context(), ref)
	availInputFor = oldFor
	at, _ := availWaitTimes(s)
	availAfter(t, at.Add(5*time.Millisecond))
	list(proto.StatusWorking, "Checking the PR.")
	list(proto.StatusWorking, "Merged it.")
	if got := availEvents(s, replyWaitEvent); len(got) != 0 {
		t.Fatalf("the background round's text counted as the reply: %v", got)
	}
	list(proto.StatusDone, "Merged it.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 1 })
	list(proto.StatusWorking, "Merged it.")
	list(proto.StatusWorking, "Here is the answer.")
	waits := availEvents(s, replyWaitEvent)
	if len(waits) != 1 || waits[0]["state"] != waitBackground || waits[0]["by"] != nil {
		t.Fatalf("reply waits %v", waits)
	}
	list(proto.StatusDone, "Here is the answer.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 2 })
	if r := availEvents(s, roundEvent)[1]; r["source"] != roundOwner {
		t.Fatalf("the owner's queued message started a round of %v", r["source"])
	}

	// Taken into the running round: no round follows.
	oldGrace := availQueuedGrace
	availQueuedGrace = 0
	t.Cleanup(func() { availQueuedGrace = oldGrace })
	superviseRound(t, ft, list, "Here is the answer.")
	s.UserSpoke(t.Context(), ref)
	list(proto.StatusWorking, "Both done.")
	list(proto.StatusDone, "Both done.")
	waitUntil(t, func() bool {
		s.avail.mu.Lock()
		defer s.avail.mu.Unlock()
		return !s.avail.waits[[2]string{"mac", "conv"}].ended.IsZero()
	})
	_, ended := availWaitTimes(s)
	availAfter(t, ended.Add(availQueuedGrace))
	list(proto.StatusDone, "Both done.")
	waits = availEvents(s, replyWaitEvent)
	if len(waits) != 2 || waits[1]["state"] != waitBackground || waits[1]["by"] != "turn_end" {
		t.Fatalf("reply waits %v", waits)
	}
}

// The hub says the owner spoke before it knows a message for a replay of one it already delivered: the replay opens
// no wait, whichever of its two signals comes first. A conversation that leaves its node's list ends its wait.
func TestAvailabilityReplayAndGone(t *testing.T) {
	ft, ref, list := availConv(t)
	s := ft.s
	send := func(id string) {
		_, _ = s.SendParams(t.Context(), SendParams{MachineID: "mac", SessionID: "conv", Params: map[string]any{"text": "Hello", "client_msg_id": id}})
	}
	s.UserSpoke(t.Context(), ref)
	send("m1")
	list(proto.StatusWorking, "Hi.")
	list(proto.StatusDone, "Hi.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 1 })
	if got := availEvents(s, replyWaitEvent); len(got) != 1 {
		t.Fatalf("reply waits %v", got)
	}
	s.UserSpoke(t.Context(), ref) // the replay, after a silent reconnect
	send("m1")
	if n := s.avail.waiting.Load(); n != 0 {
		t.Fatalf("a replay opened a wait (%d)", n)
	}
	send("m1")
	s.UserSpoke(t.Context(), ref)
	if n := s.avail.waiting.Load(); n != 0 {
		t.Fatalf("a replay whose send came first opened a wait (%d)", n)
	}
	// A message the hub could not send is taken back; one that failed after an earlier message opened the wait is not.
	s.UserSpoke(t.Context(), ref)
	s.ConversationSent(t.Context(), ConversationSent{MachineID: "mac", SessionID: "conv", Outcome: assistantwire.ConversationFailed})
	s.waitWorkEvents()
	if n := s.avail.waiting.Load(); n != 0 {
		t.Fatalf("a failed send opened a wait (%d)", n)
	}
	superviseRound(t, ft, list, "Hi.") // the replay's input is not the next round's
	list(proto.StatusDone, "Hi.")
	waitUntil(t, func() bool { return len(availEvents(s, roundEvent)) == 2 })

	// A new message during a round, and the conversation is gone before a reply.
	superviseRound(t, ft, list, "Hi.")
	s.UserSpoke(t.Context(), ref)
	send("m2")
	if n := s.avail.waiting.Load(); n != 1 {
		t.Fatalf("%d waits", n)
	}
	old := replyWaitMax
	replyWaitMax = 0
	t.Cleanup(func() { replyWaitMax = old })
	at, _ := availWaitTimes(s)
	availAfter(t, at.Add(replyWaitMax))
	ft.sessions("mac")
	waits := availEvents(s, replyWaitEvent)
	if len(waits) != 2 || waits[1]["unanswered"] != true || s.avail.waiting.Load() != 0 {
		t.Fatalf("reply waits %v, %d waiting", waits, s.avail.waiting.Load())
	}
}

// A queued message records, once delivered, how long it waited and how long the queue was when it was added; one
// that takes a queued message's place has waited since the first.
func TestAvailabilityQueueWait(t *testing.T) {
	f := newFakeHub().withMachine()
	f.known["receiver"] = true
	s := newTestService(t, f)
	first := relayMsg{From: "c1", FromTitle: "Research", Tool: "conversation_send", Text: "Read the result.", Hop: 1, At: time.Now().UTC()}
	if n := s.queueRelay("receiver", first); n != 1 {
		t.Fatalf("queued %d", n)
	}
	if n := s.queueRelay("receiver", relayMsg{Tool: superviseTool, Text: "Synthetic supervision."}); n != 2 {
		t.Fatalf("queued %d", n)
	}
	queued := s.relays.st.Queue["receiver"][1]
	if queued.Depth != 2 || queued.QueuedAt.IsZero() {
		t.Fatalf("queued %+v", queued)
	}
	if !s.replaceQueuedSupervise("receiver", relayMsg{Tool: superviseTool, Text: "A later one."}) {
		t.Fatal("not replaced")
	}
	if got := s.relays.st.Queue["receiver"][1]; got.Text != "A later one." || !got.QueuedAt.Equal(queued.QueuedAt) || got.Depth != 2 {
		t.Fatalf("the replacement lost when the first was queued: %+v", got)
	}
	idle := []proto.Session{{SessionID: "receiver", Status: proto.StatusIdle}}
	s.drainRelays("server", idle)
	waitUntil(t, func() bool { return len(availEvents(s, queueWaitEvent)) == 1 })
	if e := availEvents(s, queueWaitEvent)[0]; e["kind"] != proto.SourceConversation || e["depth"] != 1 {
		t.Fatalf("queue wait %v", e)
	}
	s.drainRelays("server", idle)
	waitUntil(t, func() bool { return len(availEvents(s, queueWaitEvent)) == 2 })
	if e := availEvents(s, queueWaitEvent)[1]; e["kind"] != proto.SourceSupervision || e["depth"] != 2 {
		t.Fatalf("queue wait %v", e)
	}
}

// Only a resolved or dismissed card produces an outcome event.
func TestAvailabilityAskOutcome(t *testing.T) {
	s := newStorelessService(t, newFakeHub().withMachine())
	now := time.Now()
	card := proto.Interaction{ID: "q1", MachineID: "mac", SessionID: "s1", Kind: proto.IxQuestion, CreatedAt: now.Add(-time.Minute)}
	s.availAskEnded(InteractionResolved{Interaction: card, Status: "failed"}, now)
	s.availAskEnded(InteractionResolved{Interaction: card, Status: "resolved"}, now)
	card.ID = "a1"
	card.ToolName = "ask_user"
	s.availAskEnded(InteractionResolved{Interaction: card, Resolution: proto.Resolution{Dismiss: true, Reason: proto.DismissedExpired}, Status: "dismissed"}, now)

	events := availEvents(s, askOutcomeEvent)
	if len(events) != 2 || events[0]["interaction"] != "q1" || events[0]["by"] != askByOwner || events[0]["after"] != 60.0 ||
		events[1]["interaction"] != "a1" || events[1]["kind"] != "ask_user" || events[1]["expired"] != true {
		t.Fatalf("ask outcomes %v", events)
	}
}
