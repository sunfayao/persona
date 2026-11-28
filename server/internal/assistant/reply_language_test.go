package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/prompts"
)

// setReplyLanguage saves the reply-language setting through the registry's own field, as persona_settings does.
func setReplyLanguage(t *testing.T, s *Service, value string) {
	t.Helper()
	ctx := context.Background()
	for _, f := range s.profileFields() {
		if f.key != "assistant.reply_language" {
			continue
		}
		raw, _ := json.Marshal(value)
		val, err := f.decodeValue(raw)
		if err != nil {
			t.Fatalf("%q: %v", value, err)
		}
		if err := f.write(ctx, val); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("assistant.reply_language is not in the registry")
}

// With the setting English and the interface Simplified Chinese, the main rules (answers and compaction), the worker
// rules and the closeout all say English; with it empty they say what they said before.
func TestReplyLanguageSettingReachesEveryRuleText(t *testing.T) {
	ctx := context.Background()
	s, f := newTaskService(t)
	f.hello.Locale = "zh-Hans"
	closeoutOn(t, s)
	texts := func() (main, worker, brief, compact string) {
		return s.claudeMD(ctx, "box", false), prompts.RenderTaskHandoff(s.taskEntryFor(ctx, "box", "claude", true, false)), s.closeoutBriefFor(ctx, "box", false, false),
			s.contextResult(ctx, ContextParams{}, "", "", true).CompactInstructions
	}
	main0, worker0, brief0, compact0 := texts()
	for name, got := range map[string]string{"main": main0, "worker": worker0, "closeout": brief0, "compaction": compact0} {
		if !strings.Contains(got, "Simplified Chinese") {
			t.Errorf("default %s rules: %q", name, got)
		}
	}
	if !strings.Contains(main0, "Answer in Simplified Chinese (the language of the user's interface") {
		t.Error("the default main rules lost the interface parenthesis")
	}

	setReplyLanguage(t, s, "English")
	main1, worker1, brief1, compact1 := texts()
	for name, got := range map[string]string{"main": main1, "worker": worker1, "closeout": brief1, "compaction": compact1} {
		if strings.Contains(got, "Simplified Chinese") {
			t.Errorf("%s rules still name Simplified Chinese: %q", name, got)
		}
	}
	for _, want := range []string{"Answer in English.", "Write every compaction summary in English"} {
		if !strings.Contains(main1, want) {
			t.Errorf("main rules lack %q", want)
		}
	}
	if strings.Contains(main1, "the language of the user's interface") {
		t.Error("with the setting set, the main rules still point at the interface language")
	}
	if !strings.Contains(worker1, "Reply in English.") || !strings.Contains(brief1, "in English.") {
		t.Errorf("worker rules: %q / %q", worker1, brief1)
	}
	if len(main1) >= len(main0) {
		t.Error("the main rules did not get shorter with the setting set")
	}

	setReplyLanguage(t, s, "")
	if m, w, b, c := texts(); m != main0 || w != worker0 || b != brief0 || c != compact0 {
		t.Error("emptying the setting did not restore the rules")
	}
}

func TestReplyLanguageSettingRefusesOtherValues(t *testing.T) {
	s, _ := newTaskService(t)
	for _, f := range s.profileFields() {
		if f.key != "assistant.reply_language" {
			continue
		}
		for _, bad := range []string{`"Klingon"`, `null`, `3`} {
			if _, err := f.decodeValue(json.RawMessage(bad)); err == nil {
				t.Errorf("%s was accepted", bad)
			}
		}
		return
	}
	t.Fatal("assistant.reply_language is not in the registry")
}
