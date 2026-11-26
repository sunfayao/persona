package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/proto"
)

// registryTool runs persona_settings the way the main chat does and answers each approval card with allow.
type registryTool struct {
	t     *testing.T
	s     *Service
	cards []string
}

// call runs one call; given allow, it waits for the one approval card and answers it that way (no card is a failure).
func (r *registryTool) call(args string, allow ...bool) (string, bool) {
	r.t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if len(allow) == 0 {
			return
		}
		q, ok := awaitCard(r.s)
		if !ok {
			r.t.Errorf("no approval card for %s", args)
			return
		}
		r.cards = append(r.cards, q.Summary)
		answer, _ := json.Marshal(proto.Resolution{Allow: &allow[0]})
		if _, err := r.s.Action(context.Background(), Action{Op: "answer", InteractionID: q.ID, Answer: answer}); err != nil {
			r.t.Errorf("answer: %v", err)
		}
	}()
	res := r.s.Tool(context.Background(), ToolCall{Name: "persona_settings", Arguments: json.RawMessage(args), MachineID: "server", SessionID: "c1"})
	<-done
	return res.Content[0].Text, res.IsError
}

func (r *registryTool) get(key string) string {
	r.t.Helper()
	out, isErr := r.call(fmt.Sprintf(`{"action":"get","key":%q}`, key))
	if isErr {
		r.t.Fatalf("get %s: %s", key, out)
	}
	return out
}

func TestRegistrySettingsListNamesEveryKeyWithItsValue(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	r := &registryTool{t: t, s: s}
	out, isErr := r.call(`{"action":"list"}`)
	if isErr {
		t.Fatal(out)
	}
	for _, f := range append(s.profileFields(), s.triageFields()...) {
		line := ""
		for _, l := range strings.Split(out, "\n") {
			if strings.HasPrefix(l, "- "+f.key+" (") {
				line = l
			}
		}
		if line == "" || !strings.Contains(line, f.about) {
			t.Errorf("list has no line with the description of %s", f.key)
		}
		if !f.slow && !strings.Contains(line, " Now: ") {
			t.Errorf("list shows no current value for %s: %s", f.key, line)
		}
	}
	for _, key := range []string{"assistant.saved_prompts", "assistant.delivery_gate"} {
		if !strings.Contains(out, "- "+key+" (") {
			t.Errorf("list lacks %s", key)
		}
	}
}

func TestRegistrySettingsSet(t *testing.T) {
	cases := []struct {
		key     string
		value   string // a valid change
		want    string // get after it
		before  string // get before it
		invalid []string
	}{
		{"assistant.cache_keepalive", "false", "false", "true", []string{"1", `"off"`, "null"}},
		{"assistant.cache_keepalive_minutes", "30", "30", "55", []string{"0", "56", "2.5", `"30"`}},
		{"assistant.cache_keepalive_max", "48", "48", "24", []string{"0", "145"}},
		{"assistant.idle_close_hours", "0", "0", "12", []string{"-1", "8761"}},
		{"assistant.archive_closed_days", "7", "7", "3", []string{"3651", "true"}},
		{"assistant.agent_auto_naming", "false", "false", "true", []string{`"no"`}},
		{"assistant.agent_auto_naming_max", "3", "3", "1", []string{"0", "1001"}},
		{"assistant.agent_auto_naming_interval_hours", "24", "24", "0", []string{"-1"}},
		{"assistant.next_suggestions", "true", "true", "false", []string{"2"}},
		{"assistant.inbox_post", "false", "false", "true", []string{"null"}},
		{"assistant.owner_reply_approval", "true", "true", "false", []string{`"yes"`}},
		{"assistant.persistent_owner_action_cards", "true", "true", "false", []string{"1"}},
		{"assistant.owner_reply_approval_phrases", `["ok","go"]`, `["ok","go"]`, "", []string{`["ok","ok"]`, `[""]`, `"ok"`, `["a","b","c","d","e","f","g","h","i"]`}},
		{"agents.triage.closeout", "true", "true", "false", []string{`"on"`}},
		{"agents.triage.transient_retry", "false", "false", "true", nil},
		{"agents.triage.reply_reminder_minutes", "45", "45", "30", []string{"-1", "10081"}},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			s := newTestService(t, newFakeHub().withMachine())
			r := &registryTool{t: t, s: s}
			if c.before != "" {
				if got := r.get(c.key); got != c.before && !(c.before == "（无）" && got == "（无）") {
					t.Fatalf("before: %q want %q", got, c.before)
				}
			}
			for _, bad := range c.invalid {
				if out, isErr := r.call(fmt.Sprintf(`{"action":"set","key":%q,"value":%s}`, c.key, bad)); !isErr {
					t.Fatalf("invalid value %s accepted: %q", bad, out)
				}
			}
			if len(r.cards) != 0 || len(s.pendingQuestions()) != 0 {
				t.Fatal("an invalid value made a card")
			}
			if out, isErr := r.call(fmt.Sprintf(`{"action":"set","key":%q,"value":%s}`, c.key, c.value), false); isErr || !strings.Contains(out, "declined") {
				t.Fatalf("declined card: %q error=%v", out, isErr)
			}
			if got := r.get(c.key); c.before != "" && got != c.before {
				t.Fatalf("a declined change changed %s to %q", c.key, got)
			}
			if out, isErr := r.call(fmt.Sprintf(`{"action":"set","key":%q,"value":%s}`, c.key, c.value), true); isErr {
				t.Fatalf("approved change: %q", out)
			}
			if got := r.get(c.key); got != c.want {
				t.Fatalf("after: %q want %q", got, c.want)
			}
			if len(r.cards) != 2 || !strings.Contains(r.cards[0], i18n.T(i18n.Default, "assistant-settings_registry.title_"+settingSlug(c.key))) {
				t.Fatalf("approval cards: %q", r.cards)
			}
			// the value in effect again is no change and needs no card
			if out, isErr := r.call(fmt.Sprintf(`{"action":"set","key":%q,"value":%s}`, c.key, c.value)); isErr || !strings.Contains(out, "unchanged") {
				t.Fatalf("same value: %q error=%v", out, isErr)
			}
		})
	}
}

func TestRegistrySettingsMachineRefusesUnknown(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	r := &registryTool{t: t, s: s}
	if out, isErr := r.call(`{"action":"set","key":"assistant.machine","value":"nowhere"}`); !isErr {
		t.Fatalf("an unknown machine was accepted: %q", out)
	}
}

func TestRegistrySavedPrompts(t *testing.T) {
	s := newTestService(t, newFakeHub().withMachine())
	r := &registryTool{t: t, s: s}
	if got := r.get("assistant.saved_prompts"); got != "（无）" {
		t.Fatalf("empty list: %q", got)
	}
	for _, bad := range []string{`{"name":"two words","text":"x"}`, `{"name":"ok","text":""}`, `{"text":"x"}`, `"x"`} {
		if out, isErr := r.call(`{"action":"add","key":"assistant.saved_prompts","value":` + bad + `}`); !isErr {
			t.Fatalf("invalid prompt %s accepted: %q", bad, out)
		}
	}
	if out, isErr := r.call(`{"action":"add","key":"assistant.saved_prompts","value":{"name":"/review","text":"Review my diff"}}`, true); isErr {
		t.Fatalf("add: %q", out)
	}
	got := r.get("assistant.saved_prompts")
	if !strings.Contains(got, "/review: Review my diff") {
		t.Fatalf("list: %q", got)
	}
	if out, isErr := r.call(`{"action":"add","key":"assistant.saved_prompts","value":{"name":"REVIEW","text":"again"}}`); !isErr {
		t.Fatalf("a duplicate name was accepted: %q", out)
	}
	id := strings.Fields(strings.TrimPrefix(got, "- "))[0]
	if out, isErr := r.call(fmt.Sprintf(`{"action":"set","key":"assistant.saved_prompts","value":{"id":%q,"name":"review","text":"Review the diff and the tests"}}`, id), true); isErr {
		t.Fatalf("set: %q", out)
	}
	if got := r.get("assistant.saved_prompts"); !strings.Contains(got, "Review the diff and the tests") {
		t.Fatalf("after set: %q", got)
	}
	if out, isErr := r.call(fmt.Sprintf(`{"action":"remove","key":"assistant.saved_prompts","id":%q}`, id), true); isErr {
		t.Fatalf("remove: %q", out)
	}
	if got := r.get("assistant.saved_prompts"); got != "（无）" {
		t.Fatalf("after remove: %q", got)
	}
}
