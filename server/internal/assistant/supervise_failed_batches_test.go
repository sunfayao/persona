package assistant

import (
	"strings"
	"testing"
)

func TestFailedStartSettingsWordingKeepsWordBoundaries(t *testing.T) {
	s, _ := newQuietService(t)
	want := map[string]string{
		"assistant.main_supervises_own_tasks": "failed starts with no current leader and of what",
	}
	for _, setting := range s.ownSettings() {
		fragment, ok := want[setting.Key]
		if !ok {
			continue
		}
		if !strings.Contains(setting.About, fragment) {
			t.Errorf("%s lost a word boundary in %q", setting.Key, setting.About)
		}
		if strings.Contains(setting.About, "themain") || strings.Contains(setting.About, "ofwhat") {
			t.Errorf("%s joined adjacent words: %q", setting.Key, setting.About)
		}
		delete(want, setting.Key)
	}
	for key := range want {
		t.Errorf("setting %s was not found", key)
	}
}
