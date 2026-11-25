package hub

import (
	"os"
	"path/filepath"
	"testing"
)

// The hub.json of a hub that ran before the knowledge base, the mobile push and the other removed features were cut has
// their sections, and the new hub starts with it: it reads what is left and leaves the rest alone.
func TestHubConfigIgnoresTheSectionsOfRemovedFeatures(t *testing.T) {
	dir := t.TempDir()
	raw := `{"listen":":18999","token":"hub-token","database_url":"postgres://u:p@127.0.0.1:5432/persona",
		"kb":{"dir":"/var/lib/persona/kb","ai":{"model":"m"}},"apns":{"team":"T","key_id":"K","key_file":"k.p8"},
		"push_relay":{"url":"https://relay.example.com"},"health":{"enabled":true},"watch":{"complication":true},
		"recordings":{"days":14},"workspace_board":{"on":true},"assistant":{"owner":"Example","external":false,"kb_enabled":true}}`
	if err := os.WriteFile(filepath.Join(dir, "hub.json"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := readConfigFile(dir)
	if err != nil {
		t.Fatalf("hub.json with the sections of removed features: %v", err)
	}
	if cfg.Token != "hub-token" {
		t.Fatalf("the token: %q", cfg.Token)
	}
}
