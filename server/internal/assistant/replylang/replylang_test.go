package replylang

import "testing"

func TestResolve(t *testing.T) {
	for _, c := range []struct{ setting, locale, want string }{
		{"", "zh-Hans", ""},
		{"", "en", "English"},
		{"", "ja", "日本語"}, // the native name; prompts.AnswerLanguage reads it
		{"", "", ""},
		{"English", "zh-Hans", "English"},
		{"Simplified Chinese", "en", "Simplified Chinese"},
		{"Japanese", "zh-Hans", "Japanese"},
		{"Klingon", "en", "English"}, // an unknown value is not a choice
	} {
		if got := Resolve(c.setting, c.locale); got != c.want {
			t.Errorf("Resolve(%q, %q) = %q, want %q", c.setting, c.locale, got, c.want)
		}
	}
}

func TestSet(t *testing.T) {
	if Set("") || Set("Klingon") || !Set("English") {
		t.Fatal("Set must be true for the choices only")
	}
}
