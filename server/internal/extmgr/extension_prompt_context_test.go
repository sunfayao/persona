package extmgr

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/extension"
)

func TestClipLines(t *testing.T) {
	for _, c := range []struct {
		in   string
		most int
		want string
		cut  bool
	}{
		{"short", 10, "short", false},
		{"one\ntwo\nthree", 9, "one\ntwo", true},
		{"一二三四五", 7, "一二", true}, // three bytes a character: never half of one
	} {
		got, cut := clipLines(c.in, c.most)
		if got != c.want || cut != c.cut {
			t.Errorf("%q at %d: %q %v", c.in, c.most, got, cut)
		}
	}
}

// What an extension wrote for the Inbox reaches the manager's prompts: cleaned, cut at a line end
// past the limit, the never-quote sentences apart; the status says what is used; switched off, nothing.
func TestPromptContextContributions(t *testing.T) {
	h, _ := extensionTestHub(t)
	mux := h.routes()
	master := h.cfg.Token
	pkg := declarativePackage(t, `{"manifest_version": 1, "id": "notes", "name": "Notes", "version": "1.0.0", "contributes": {
		"prompts": [{"id": "inbox", "kind": "inbox_context", "file": "inbox/context.md", "never_quote": "inbox/never_quote.txt"}]}}`)
	installForTest(t, h, pkg)
	data := h.extensions.reg.DataDir("notes")
	mustWrite(t, filepath.Join(data, "inbox", "context.md"), "The user answers briefly.\n")
	mustWrite(t, filepath.Join(data, "inbox", "never_quote.txt"), "Thinks the vendor is unreliable\n\n  Seems tired lately  \nThinks the vendor is unreliable\n")

	inbox := h.extensions.inboxContext()
	if len(inbox.Blocks) != 1 || inbox.Blocks[0].Text != "The user answers briefly." ||
		strings.Join(inbox.NeverQuote, "|") != "Thinks the vendor is unreliable|Seems tired lately" {
		t.Fatalf("%+v", inbox)
	}

	_, out := call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	items := out["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("%v", out)
	}
	ib := items[0].(map[string]any)
	if ib["kind"] != extension.PromptInboxContext || ib["state"] != dataStateAdopted || ib["never_quote"] != float64(2) ||
		ib["hash"] != textDigest("The user answers briefly.") {
		t.Fatalf("inbox status: %v", ib)
	}

	if code, out := call(t, mux, "PUT", "/api/v1/extensions/notes/-/enabled", master, `{"enabled": false}`); code != 200 {
		t.Fatalf("%d %v", code, out)
	}
	if len(h.extensions.inboxContext().NeverQuote) != 0 {
		t.Fatal("switched off: nothing")
	}
	_, out = call(t, mux, "GET", "/api/v1/extensions/notes/-/assistant-data", master, "")
	for _, it := range out["items"].([]any) {
		if v := it.(map[string]any); v["state"] != dataStateNone || v["reason"] != dataDisabled {
			t.Fatalf("switched off: %v", v)
		}
	}
}
