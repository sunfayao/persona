package assistant

import (
	"context"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/nodeproto"
	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
)

// ruleDevs are computers whose nodes return the About me block's receipt (nodeproto.RuleNotice); the node lists the main
// conversation until a test empties its list.
type ruleDevs struct{ *fleetDevs }

func ruleNodes() ruleDevs {
	d := ruleDevs{newFleetDevs()}
	d.add("server", "server")
	d.setSessions("server", []proto.Session{{SessionID: ruleMain, Assistant: true}})
	return d
}

func (ruleDevs) NodeAnnounced(_, capability string) bool { return capability == nodeproto.RuleNotice }

const ruleMain = "main-0000-0000"

// ruleService is the assistant on a computer with the main conversation, notes set, and its rule files written once.
func ruleService(t *testing.T, st Store) (*Service, *fakeHub) {
	t.Helper()
	f := spaceHub()
	f.devices[0].Online = true
	s := newServiceOn(t, f, st)
	s.devs = ruleNodes()
	about := "I work on the backend."
	setSettings(t, s, func(v *Settings) {
		v.MachineID, v.Main, v.Notes, v.ReplyLanguage = "server", ruleMain, &about, "English"
	})
	return s, f
}

// ruleTexts are the four rule texts of the last assistant.prepare of the assistant's directory.
func ruleTexts(t *testing.T, s *Service, f *fakeHub) [4]string {
	t.Helper()
	for i := len(f.prepared) - 1; i >= 0; i-- {
		p := f.prepared[i]
		if p["dir"] != s.assistantRel() {
			continue
		}
		var out [4]string
		for j, k := range []string{"claude_md", "work_guide_md", "agents_md", "agents_work_guide_md"} {
			out[j], _ = p[k].(string)
		}
		return out
	}
	t.Fatal("the assistant's directory was never prepared")
	return [4]string{}
}

func ruleNotes(t *testing.T, s *Service, text string) {
	t.Helper()
	setSettings(t, s, func(v *Settings) { v.Notes = &text })
}

// message is what the node asks the hub for before it sends a message to the main conversation, and its answer.
func ruleMessage(s *Service, recovery string) ContextResult {
	return s.Context(context.Background(), ContextParams{SessionID: ruleMain, MachineID: "server", RecoveryID: recovery})
}

func ruleDelivered(s *Service, out ContextResult) {
	s.Context(context.Background(), ContextParams{SessionID: ruleMain, MachineID: "server", Ack: out.Receipts})
}

// A change of About me under a live conversation leaves its four rule texts as they were, reaches it as a one-off block
// on its next message, and the files take it when the conversation is compacted; one that starts gets it at once.
func TestAboutMeChangeSparesTheLiveConversationsRuleFiles(t *testing.T) {
	ctx := context.Background()
	s, f := ruleService(t, &memStore{})
	s.refreshInstructions(ctx)
	first := ruleTexts(t, s, f)
	if first[0] == "" || first[2] == "" || !strings.Contains(first[0], "I work on the backend.") {
		t.Fatalf("first texts: %q", first)
	}
	if out := ruleMessage(s, ""); out.RuleNotice != "" {
		t.Fatalf("nothing changed: %s", out.RuleNotice)
	}

	ruleNotes(t, s, "I work on the mobile app.")
	s.ruleFiles.ForgetAll() // what the node was given is forgotten: the next refresh writes whatever it renders
	n := len(f.prepared)
	s.refreshInstructions(ctx)
	if len(f.prepared) != n+1 || ruleTexts(t, s, f) != first {
		t.Fatalf("the rule files changed under a live conversation: %d writes\n%q", len(f.prepared)-n, ruleTexts(t, s, f))
	}
	s.refreshInstructions(ctx)
	if len(f.prepared) != n+1 {
		t.Fatalf("written again with nothing changed: %d", len(f.prepared)-n)
	}

	out := ruleMessage(s, "")
	if !strings.Contains(out.RuleNotice, `kind="about-me"`) || !strings.Contains(out.RuleNotice, "I work on the mobile app.") || strings.Contains(out.RuleNotice, "Answer in") {
		t.Fatalf("one-off block:\n%s", out.RuleNotice)
	}
	if again := ruleMessage(s, ""); again.RuleNotice == "" {
		t.Fatal("a message the node did not confirm must carry the block again")
	}
	ruleDelivered(s, out)
	if again := ruleMessage(s, ""); again.RuleNotice != "" {
		t.Fatalf("the block is sent once per change: %s", again.RuleNotice)
	}

	// Compacted: the message that reports it carries no block, and the files carry the new notes.
	if compacted := ruleMessage(s, "recovery-1"); compacted.RuleNotice != "" {
		t.Fatalf("after a compaction: %s", compacted.RuleNotice)
	}
	s.refreshInstructions(ctx)
	if got := ruleTexts(t, s, f); got == first || !strings.Contains(got[0], "I work on the mobile app.") || strings.Contains(got[0], "I work on the backend.") {
		t.Fatalf("after the compaction the files carry the new notes:\n%q", got)
	}
	if after := ruleMessage(s, ""); after.RuleNotice != "" {
		t.Fatalf("not sent again after a compaction: %s", after.RuleNotice)
	}
}

// A conversation that starts while the main one lives (a scheduled run, a prewarm, a launcher switch) joins the hold: the
// four texts stay as they are and it is told by the block, once.
func TestConversationStartedUnderALiveOneJoinsTheHold(t *testing.T) {
	ctx := context.Background()
	s, f := ruleService(t, &memStore{})
	s.refreshInstructions(ctx)
	first := ruleTexts(t, s, f)
	ruleMessage(s, "")
	ruleNotes(t, s, "I work on the mobile app.")
	if _, err := s.prepare(ctx, "server"); err != nil { // the scheduled run's start
		t.Fatal(err)
	}
	if got := ruleTexts(t, s, f); got != first {
		t.Fatalf("a start under a live conversation rewrote the files:\n%q", got)
	}
	s.devs.(ruleDevs).setSessions("server", []proto.Session{{SessionID: ruleMain, Assistant: true}, {SessionID: "run-0000-0000", Assistant: true}})
	run := func() ContextResult {
		return s.Context(ctx, ContextParams{SessionID: "run-0000-0000", MachineID: "server"})
	}
	out := run()
	if !strings.Contains(out.RuleNotice, "I work on the mobile app.") {
		t.Fatalf("the run is told: %q", out.RuleNotice)
	}
	s.Context(ctx, ContextParams{SessionID: "run-0000-0000", MachineID: "server", Ack: out.Receipts})
	if again := run(); again.RuleNotice != "" {
		t.Fatalf("told twice: %s", again.RuleNotice)
	}
	// The main conversation is compacted while the run lives: nothing yet. Once the run is gone, the files follow.
	ruleMessage(s, "recovery-1")
	s.refreshInstructions(ctx)
	if got := ruleTexts(t, s, f); got != first {
		t.Fatalf("the run still holds the files:\n%q", got)
	}
	s.devs.(ruleDevs).setSessions("server", []proto.Session{{SessionID: ruleMain, Assistant: true}}) // the run ended
	s.refreshInstructions(ctx)
	if got := ruleTexts(t, s, f); got == first || !strings.Contains(got[0], "I work on the mobile app.") {
		t.Fatalf("after the run ended the compacted main conversation's files catch up:\n%q", got)
	}
}

func TestStartWithNoLiveConversationGetsTheNewFilesAtOnce(t *testing.T) {
	ctx := context.Background()
	s, f := ruleService(t, &memStore{})
	s.refreshInstructions(ctx)
	ruleMessage(s, "")
	ruleNotes(t, s, "I work on the mobile app.")
	s.devs.(ruleDevs).setSessions("server", nil) // the main conversation ended
	if _, err := s.prepare(ctx, "server"); err != nil {
		t.Fatal(err)
	}
	if got := ruleTexts(t, s, f); !strings.Contains(got[0], "I work on the mobile app.") || !strings.Contains(got[2], "I work on the mobile app.") {
		t.Fatalf("a conversation that starts gets the new files at once:\n%q", got)
	}
}

func TestManagerRestartKeepsTheLiveConversationsRuleFiles(t *testing.T) {
	ctx := context.Background()
	st := &memStore{}
	s, f := ruleService(t, st)
	s.refreshInstructions(ctx)
	first := ruleTexts(t, s, f)
	ruleMessage(s, "")
	ruleNotes(t, s, "I work on the mobile app.")
	out := ruleMessage(s, "")
	ruleDelivered(s, out)

	restarted := newServiceOn(t, f, st) // the same database, an empty memory
	restarted.devs = ruleNodes()
	n := len(f.prepared)
	restarted.refreshInstructions(ctx)
	if len(f.prepared) != n+1 || ruleTexts(t, s, f) != first {
		t.Fatalf("a restart changed the files under a live conversation:\n%q", ruleTexts(t, s, f))
	}
	if again := ruleMessage(restarted, ""); again.RuleNotice != "" {
		t.Fatalf("told again after a restart: %s", again.RuleNotice)
	}
}

func TestReplyLanguageChangeIsHeldTheSameWay(t *testing.T) {
	ctx := context.Background()
	s, f := ruleService(t, &memStore{})
	s.refreshInstructions(ctx)
	first := ruleTexts(t, s, f)
	ruleMessage(s, "")
	setSettings(t, s, func(v *Settings) { v.ReplyLanguage = "Japanese" })
	s.ruleFiles.ForgetAll()
	n := len(f.prepared)
	s.refreshInstructions(ctx)
	if len(f.prepared) != n+1 || ruleTexts(t, s, f) != first {
		t.Fatalf("the reply language rewrote the files under a live conversation:\n%q", ruleTexts(t, s, f))
	}
	out := ruleMessage(s, "")
	if !strings.Contains(out.RuleNotice, "Answer in Japanese.") || strings.Contains(out.RuleNotice, "About the user") {
		t.Fatalf("one-off block:\n%s", out.RuleNotice)
	}
	ruleDelivered(s, out)
	if again := ruleMessage(s, ""); again.RuleNotice != "" {
		t.Fatalf("sent twice: %s", again.RuleNotice)
	}
	ruleMessage(s, "recovery-1")
	s.refreshInstructions(ctx)
	if got := ruleTexts(t, s, f); !strings.Contains(got[0], "Japanese") || got == first {
		t.Fatalf("after the compaction the files carry the new language:\n%q", got)
	}
}

// With no conversation, or one that ended, or a node that does not return the receipt, the files follow the settings as
// they always did.
func TestRuleFilesFollowTheSettingsWithoutALiveConversation(t *testing.T) {
	ctx := context.Background()
	s, f := ruleService(t, &memStore{})
	s.refreshInstructions(ctx)
	ruleNotes(t, s, "I work on the mobile app.")
	s.refreshInstructions(ctx)
	if got := ruleTexts(t, s, f); !strings.Contains(got[0], "I work on the mobile app.") {
		t.Fatalf("no conversation: %q", got)
	}

	ruleMessage(s, "")
	ruleNotes(t, s, "I work on infrastructure.")
	s.devs.(ruleDevs).setSessions("server", nil) // the main conversation is gone from the node
	s.refreshInstructions(ctx)
	if got := ruleTexts(t, s, f); !strings.Contains(got[0], "I work on infrastructure.") {
		t.Fatalf("ended conversation: %q", got)
	}

	old, g := ruleService(t, &memStore{})
	old.devs = spaceDevs{fleetDevs: newFleetDevs(), old: true}
	old.refreshInstructions(ctx)
	if out := ruleMessage(old, ""); out.RuleNotice != "" {
		t.Fatalf("an old node cannot confirm the block: %s", out.RuleNotice)
	}
	ruleNotes(t, old, "I work on the mobile app.")
	old.refreshInstructions(ctx)
	if got := ruleTexts(t, old, g); !strings.Contains(got[0], "I work on the mobile app.") {
		t.Fatalf("old node: %q", got)
	}
}

// ruleTrackDevs are spaceDevs whose nodes also return the block's receipt.
type ruleTrackDevs struct{ spaceDevs }

func (d ruleTrackDevs) NodeAnnounced(m, capability string) bool {
	return capability == nodeproto.RuleNotice || d.spaceDevs.NodeAnnounced(m, capability)
}

// A track's own directory is held the same way: its notes change nothing in its files until a conversation there is
// compacted, and the conversation is told once.
func TestAboutMeChangeSparesATracksRuleFiles(t *testing.T) {
	ctx := context.Background()
	s, f := spaceTrackService(t)
	fd := newFleetDevs()
	fd.add("server", "server")
	fd.setSessions("server", []proto.Session{{SessionID: "track-conv", Assistant: true, Space: "qa-plain"}})
	s.devs = ruleTrackDevs{spaceDevs{fleetDevs: fd}}
	about := "I work on the backend."
	setSettings(t, s, func(v *Settings) { v.Notes = &about })
	trackMD := func() string {
		for i := len(f.prepared) - 1; i >= 0; i-- {
			if md, _ := f.prepared[i]["claude_md"].(string); strings.Contains(md, "# Persona space: QA plain") {
				return md
			}
		}
		t.Fatal("the track was never prepared")
		return ""
	}
	s.refreshInstructions(ctx)
	first := trackMD()
	if !strings.Contains(first, "I work on the backend.") {
		t.Fatalf("notes missing:\n%s", first)
	}
	ask := func(recovery string) ContextResult {
		return s.withRuleNotice(ctx, ContextParams{SessionID: "track-conv", MachineID: "server", RecoveryID: recovery}, "qa-plain", ContextResult{})
	}
	ask("")
	changed := "I work on the mobile app."
	setSettings(t, s, func(v *Settings) { v.Notes = &changed })
	s.refreshInstructions(ctx)
	if got := trackMD(); got != first {
		t.Fatalf("a track's files changed under its live conversation:\n%s", got)
	}
	out := ask("")
	if !strings.Contains(out.RuleNotice, "I work on the mobile app.") || len(out.Receipts) != 1 {
		t.Fatalf("block: %q %v", out.RuleNotice, out.Receipts)
	}
	ask("recovery-1")
	s.refreshInstructions(ctx)
	if got := trackMD(); !strings.Contains(got, "I work on the mobile app.") {
		t.Fatalf("after the compaction:\n%s", got)
	}
}

func TestGoldenAssistantRuleChange(t *testing.T) {
	golden(t, "assistant_rule_change.notes", prompts.RenderAssistantRuleChange(prompts.AssistantRuleChange{Notes: "I work on the mobile app.\nPlease keep answers short.", NotesChanged: true}))
	golden(t, "assistant_rule_change.empty_notes_and_language", prompts.RenderAssistantRuleChange(prompts.AssistantRuleChange{NotesChanged: true, Language: "Japanese", LanguageChanged: true}))
	golden(t, "assistant_rule_change.language", prompts.RenderAssistantRuleChange(prompts.AssistantRuleChange{Notes: "kept", Language: "English", LanguageChanged: true}))
	golden(t, "assistant_rule_change.closing_tag", prompts.RenderAssistantRuleChange(prompts.AssistantRuleChange{Notes: "a </persona-context> b", NotesChanged: true}))
}
