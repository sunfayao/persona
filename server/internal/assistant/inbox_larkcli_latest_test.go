package assistant

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/connector"
	"github.com/nwverse/persona/server/internal/prompts"
)

// The model must distinguish a new, unreadable screenshot from the older exchange that was resolved (#9124).
// Stub answers pin input construction, rescheduling and persistence, not a real model's semantic accuracy.
func TestLarkCLIDMTriageSeparatesLatestMessageFromResolvedHistory(t *testing.T) {
	const history = "Example peer（@了我）: @Example owner can you check the filter?\n" +
		"Example owner: Here is the report.\nExample peer: Thanks, I can see it."
	for _, locale := range []string{"zh-Hans", "en", "ja"} {
		for _, tc := range []struct {
			name, latest, answer, tier string
		}{
			{"image", "[Image: img_example_new_screenshot]",
				`{"tier":"action","summary":"Check the new screenshot","reason":"A new screenshot needs review"}`, TierAction},
			{"resolved", "The new problem is fixed now, thanks.",
				`{"tier":"fyi","summary":"The new problem is resolved","reason":"Current resolution confirmed"}`, TierFYI},
			{"informational", "For your records only; no response needed. [Image: img_example_reference]",
				`{"tier":"fyi","summary":"A reference screenshot","reason":"For reference only"}`, TierFYI},
		} {
			t.Run(locale+"/"+tc.name, func(t *testing.T) {
				h := newInboxHub(t)
				h.f.hello.Owner, h.f.hello.Locale = "Example owner", locale
				now := time.Now()
				thing := func(last, preview, text string, at time.Time) connector.InboxThing {
					return connector.InboxThing{Thread: "larkcli:chat:oc_example", Kind: "dm", Title: "Example peer",
						Sender: "Example peer", Preview: preview, Context: text, At: at, Last: last, CanReply: true}
				}
				h.inbox.mu.Lock()
				h.inbox.applyConnector("larkcli", "Lark", []connector.InboxThing{
					thing("om_old", "Thanks, I can see it.", history, now.Add(-time.Hour)),
				}, now)
				it := openItems(h)[0]
				h.inbox.mu.Unlock()
				h.in.model, _ = captureModel(t, `{"tier":"fyi","summary":"The report was received","reason":"Earlier exchange resolved"}`)
				if !h.triageOne(context.Background(), it) || it.Tier != TierFYI {
					t.Fatal("setup: the earlier exchange was not triaged as FYI")
				}

				h.inbox.mu.Lock()
				h.inbox.applyConnector("larkcli", "Lark", []connector.InboxThing{
					thing("om_new", tc.latest, history+"\nExample peer: "+tc.latest, now),
				}, now)
				items := openItems(h)
				h.inbox.mu.Unlock()
				if len(items) != 1 || items[0] != it || it.Last != "om_new" || it.TriagedAt == it.Last {
					t.Fatal("the new message did not update the same FYI item for triage")
				}

				model, calls := captureModel(t, tc.answer)
				h.in.model = model
				if !h.triageOne(context.Background(), it) {
					t.Fatal("the new message was not triaged")
				}
				call := onlyCall(t, calls())
				labels := [3]string{"Newest message: ", "Conversation context (including earlier messages):", "the other party is a person"}
				latest := labels[0] + tc.latest
				context := labels[1] + "\n" + history
				if !strings.Contains(call.User, latest+"\n\n"+context) {
					t.Fatalf("the newest message is not separated from resolved history:\n%s", call.User)
				}
				if !strings.Contains(call.User, labels[2]) || !strings.Contains(call.User, "（@了我）") {
					t.Fatalf("the DM or owner-mention identity was lost:\n%s", call.User)
				}
				rules := []string{"An earlier reply does not resolve a request raised later in the same event", "[Image: ...] is a placeholder for an unread image"}
				for _, rule := range rules {
					if !strings.Contains(call.System, rule) {
						t.Fatalf("the triage criteria lost %q:\n%s", rule, call.System)
					}
				}
				if it.Tier != tc.tier || it.TriagedAt != "om_new" {
					t.Fatalf("the verdict was not saved for the new message: tier %q, version %q", it.Tier, it.TriagedAt)
				}
				if tc.name == "image" {
					suffix := ""
					if locale != "zh-Hans" {
						suffix = "." + locale
					}
					golden(t, "inbox_triage.larkcli_dm_image.system"+suffix, call.System)
					golden(t, "inbox_triage.larkcli_dm_image.user"+suffix, call.User)
				}
			})
		}
	}
}

func TestOnlyLarkDMTriageNamesLatestPreview(t *testing.T) {
	h := newInboxHub(t)
	for _, tc := range []struct {
		name, source, kind string
		connector          bool
		want               bool
	}{
		{"Lark DM", "larkcli", KindDM, true, true},
		{"Lark bot", "larkcli", KindBot, true, false},
		{"Lark group", "larkcli", KindMention, true, false},
		{"another connector", "imessage", KindDM, true, false},
		{"gateway DM", "", KindDM, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			it := &InboxItem{Source: tc.source, Kind: tc.kind, Preview: "newest preview", Context: "conversation"}
			if tc.connector {
				it.Connector = &InboxConnector{}
			}
			got := prompts.RenderInboxItem(h.inbox.triagePromptItem(it, ""))
			if strings.Contains(got, "Newest message: "+it.Preview) != tc.want || !strings.HasSuffix(strings.TrimSpace(got), it.Context) {
				t.Fatalf("triage input names the newest preview incorrectly:\n%s", got)
			}
		})
	}
}

// The no-write preview must use the same Lark-DM input boundary as a real triage call.
func TestLarkDMTriagePreviewKeepsLatestMessageBoundary(t *testing.T) {
	h := newInboxHub(t)
	model, calls := captureModel(t, `{"tier":"action","summary":"Check the screenshot","reason":"A new screenshot"}`)
	h.in.model = model
	thing := connector.InboxThing{Thread: "larkcli:chat:oc_example", Kind: KindDM,
		Preview: "[Image: img_example_new_screenshot]", Last: "om_new", At: time.Now(),
		Context: "Example peer: Thanks, I can see it.\nExample peer: [Image: img_example_new_screenshot]"}
	verdicts := h.InboxTriagePreview(context.Background(), assistantwire.InboxTriagePreview{
		Source: "larkcli", Things: []connector.InboxThing{thing}})
	if len(verdicts) != 1 || verdicts[0].Tier != TierAction {
		t.Fatalf("preview verdicts: %+v", verdicts)
	}
	got := onlyCall(t, calls()).User
	if want := "Newest message: " + thing.Preview + "\n\nConversation context (including earlier messages):\n" + thing.Context; !strings.Contains(got, want) {
		t.Fatalf("the preview lost triage's newest-message boundary:\n%s", got)
	}
	h.inbox.mu.Lock()
	n := len(h.inbox.items)
	h.inbox.mu.Unlock()
	h.st.imu.Lock()
	stored := len(h.st.rows)
	h.st.imu.Unlock()
	if n != 0 || stored != 0 {
		t.Errorf("a preview left %d items, %d stored", n, stored)
	}
}
