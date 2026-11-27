package extmgr

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
)

// Every registry change of an extension (configuration, switched off, switched on) tells the assistant that the
// extensions' tools may have changed, as it is told for transcription.
func TestToolsChangedOnRegistryChange(t *testing.T) {
	h, _ := extensionTestHub(t)
	var changes atomic.Int64
	h.opts.ToolsChanged = func() { changes.Add(1) }
	mux, token := h.routes(), h.cfg.Token
	pkg := notesPackage(t, "1.0.0")
	code, preview := call(t, mux, "POST", "/api/v1/extensions/-/preview", token, `{"source":"dir:`+pkg+`"}`)
	if code != 200 {
		t.Fatalf("preview: %d %v", code, preview)
	}
	body := `{"preview_id":"` + preview["preview_id"].(string) + `","permissions_digest":"` + preview["permissions_digest"].(string) + `"}`
	if code, out := call(t, mux, "POST", "/api/v1/extensions/-/install", token, body); code != 200 {
		t.Fatalf("install: %d %v", code, out)
	}
	if changes.Load() == 0 {
		t.Fatal("install did not announce a tool change")
	}
	ctx := context.Background()
	for _, change := range []struct {
		name string
		run  func() error
	}{
		{"configuration", func() error {
			return h.extensions.reg.SetConfig(ctx, "example-notes", json.RawMessage(`{"folders":["work"]}`), "test")
		}},
		{"disabled", func() error { return h.extensions.reg.SetEnabled(ctx, "example-notes", false, "test") }},
		{"enabled", func() error { return h.extensions.reg.SetEnabled(ctx, "example-notes", true, "test") }},
	} {
		t.Run(change.name, func(t *testing.T) {
			before := changes.Load()
			if err := change.run(); err != nil {
				t.Fatal(err)
			}
			if changes.Load() <= before {
				t.Fatal("registry change did not announce a tool change")
			}
		})
	}
}

// A settings change says which tools it gave or took away, and nothing when it moved none.
func TestToolChangeText(t *testing.T) {
	for _, tc := range []struct {
		name          string
		before, after []string
		want          string
	}{
		{"gained", []string{"computer_run"}, []string{"browser_look", "browser_run", "computer_run"},
			" New tools: browser_look, browser_run. Running conversations are sent their new tool list."},
		{"lost", []string{"browser_look", "computer_run"}, []string{"computer_run"},
			" Tools no longer offered: browser_look. Running conversations are sent their new tool list."},
		{"unchanged", []string{"computer_run"}, []string{"computer_run"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := toolChangeText(tc.before, tc.after); got != tc.want {
				t.Fatalf("toolChangeText = %q, want %q", got, tc.want)
			}
		})
	}
}
