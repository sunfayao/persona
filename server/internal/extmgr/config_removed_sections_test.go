package extmgr

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/managerconf"
)

// A hub and manager that ran before the knowledge base, the mobile push and the other removed features were cut still
// have their sections in hub.json and manager.json (and "kb" in the move marker). The new programs read what is left
// and start; the removed sections stay in the files, unread.
func TestConfigStartsWithTheSectionsOfRemovedFeatures(t *testing.T) {
	dir := t.TempDir()
	hub := `{"listen":":18999","token":"hub-token","kb":{"dir":"/var/lib/persona/kb","ai":{"model":"m"}},
		"apns":{"team":"T","key_id":"K"},"push_relay":{"url":"https://relay.example.com"},"health":{"enabled":true},
		"watch":{"complication":true},"recordings":{"days":14},"google":{"proxy":"http://stale-mail.example:3128"}}`
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(hub), 0o600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "managers", "assistant", "manager.json")
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := `{"google":{"proxy":"socks5://mail.example:1080"},"kb":{"dir":"/var/lib/persona/kb","ai":{"model":"m"},"selection":{"kinds":["chats"]}},
		"meeting":{"minutes":true},"assistant":{"owner":"Example","kb_enabled":true,"health_space":"on"},
		"` + managerconf.MarkerKey + `":{"keys":["kb","google","bots"],"at":"2026-10-01T00:00:00Z","backups":{"kb":"hub.json.before-manager-json-x"}}}`
	if err := os.WriteFile(file, []byte(mine), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := managerconf.MoveExcept(dir, file, nil); err != nil {
		t.Fatalf("the move with the old marker: %v", err)
	}
	cfg, err := LoadConfig(dir, file)
	if err != nil {
		t.Fatalf("a manager with the sections of removed features: %v", err)
	}
	if cfg.Token != "hub-token" || cfg.Listen != ":18999" || cfg.Google.Proxy != "socks5://mail.example:1080" {
		t.Fatalf("what is left is read: %+v", cfg)
	}
	if len(cfg.warnings) != 0 {
		t.Fatalf("the removed sections warned: %v", cfg.warnings)
	}
}
