package assistant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Joining again (a new install code into the same directory) changes who the manager is and nothing else: its
// sections of the configuration stay.
func TestWriteManagerConfigKeepsTheSections(t *testing.T) {
	dir := t.TempDir()
	c := ManagerConfig{ID: "work", Hub: "https://hub.example.com", Token: "first", Fingerprint: "fp"}
	if err := WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ManagerFile)
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), "{", `{"google": {"proxy":"socks5://mail.example:1080"}, "moved_from_hub_json": {"keys": []},`, 1))
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c.Token, c.Fingerprint = "second", ""
	if err := WriteManagerConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	got, err := ReadManagerConfig(dir)
	if err != nil || got.Token != "second" || got.Fingerprint != "" {
		t.Fatalf("%+v %v", got, err)
	}
	var raw map[string]json.RawMessage
	b, _ = os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil || string(raw["google"]) == "" || raw["moved_from_hub_json"] == nil {
		t.Fatalf("sections lost: %s", b)
	}
	if _, ok := raw["fingerprint"]; ok {
		t.Fatalf("a cleared field stayed: %s", b)
	}
}

// managerIdentity is every key of ManagerConfig: WriteManagerConfig would keep a stale one otherwise.
func TestManagerIdentityKeys(t *testing.T) {
	var keys []string
	typ := reflect.TypeFor[ManagerConfig]()
	for i := range typ.NumField() {
		keys = append(keys, strings.Split(typ.Field(i).Tag.Get("json"), ",")[0])
	}
	if !slices.Equal(keys, managerIdentity) {
		t.Fatalf("ManagerConfig's keys %v, managerIdentity %v", keys, managerIdentity)
	}
}
