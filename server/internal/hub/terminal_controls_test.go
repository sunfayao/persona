package hub

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"unicode"

	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extwire"
)

// A body's strings are read as JSON reads them, keys and values, in every value of the body: a control character
// other than newline, carriage return and tab in any of them is found; a body that is not JSON, or is cut short, is
// an error.
func TestTerminalControls(t *testing.T) {
	str := func(s string) string {
		b, _ := json.Marshal(s)
		return string(b)
	}
	for _, c := range []struct {
		name, body string
		found      bool
	}{
		{"plain text", `{"text":"carry on with the plan"}`, false},
		{"CJK", `{"text":"继续，按计划来"}`, false},
		{"emoji", `{"text":"done 🎉"}`, false},
		{"an escaped emoji", `{"text":"\ud83d\ude00"}`, false},
		{"newline, carriage return and tab", `{"text":"a\nb\r\nc\td"}`, false},
		{"numbers, booleans and null", `{"n":1.5,"ok":true,"none":null,"list":[1,2]}`, false},
		{"no body", ``, false},
		{"two values", `{"text":"a"} {"text":"b"}`, false},
		{"escape", `{"text":"\u001b"}`, true},
		{"an escape sequence", `{"text":` + str("go on\x1b[0m") + `}`, true},
		{"DEL", `{"text":` + str("a\x7fb") + `}`, true},
		{"DEL as it is", "{\"text\":\"a\x7fb\"}", true},
		{"U+009B", `{"text":"\u009b"}`, true},
		{"U+009B as it is", `{"text":"` + "\u009b" + `"}`, true},
		{"NUL", `{"text":"\u0000"}`, true},
		{"a key spelled in another case", `{"Text":"\u001b"}`, true},
		{"a key", `{"\u001b":"x"}`, true},
		{"deep in the body", `{"attachments":[{"name":"a\u001bb"}]}`, true},
		{"a second value", `{"text":"ok"} {"text":"\u001b"}`, true},
		{"a body that is a string", `"\u001b"`, true},
	} {
		found, err := terminalControls([]byte(c.body))
		if err != nil || found != c.found {
			t.Errorf("%s: %v %v, want %v", c.name, found, err, c.found)
		}
	}
	for _, body := range []string{`{"text":"a`, `{"text":"ok"`, `[`, `{"text":"ok"} x`, "{\"text\":\"\x1b\"}", `{"a" "b"}`} {
		if found, err := terminalControls([]byte(body)); err == nil {
			t.Errorf("%q: read (%v)", body, found)
		}
	}
	for r := rune(0); r <= 0x2000; r++ {
		want := (r < 0x20 || r >= 0x7f && r <= 0x9f) && r != '\n' && r != '\r' && r != '\t'
		if isTerminalControl(r) != want || want != (unicode.IsControl(r) && !strings.ContainsRune("\n\r\t", r)) {
			t.Errorf("U+%04X: %v, want %v", r, isTerminalControl(r), want)
		}
	}
}

// A message to a personal Agent goes to the manager the same way: an extension's token with terminal control
// characters is refused before it is relayed and audited; the app's token and a phone's with the same body are
// relayed, and so is an extension's plain text.
func TestExtensionMessagesToPersonalAgentsHaveNoTerminalControls(t *testing.T) {
	h, _, fx, srv := relayTestHub(t)
	tok := fx.issue("bx", "BX", []string{extension.ScopeAgentsControl})
	_, phone := relayDevice(t, srv, h.cfg.Token, "iPhone", "phone")
	var mu sync.Mutex
	var audits []string
	fx.on(extwire.HubAudit, func(params json.RawMessage) (any, error) {
		mu.Lock()
		defer mu.Unlock()
		audits = append(audits, string(params))
		return map[string]any{}, nil
	})
	audited := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), audits...)
	}
	const path = "/api/v1/agents/sessions/src%3Ak1/messages"
	bad, _ := json.Marshal(map[string]string{"text": "go\x1b[0m"})
	if code, out := call(t, srv.Config.Handler, "POST", path, tok, string(bad)); code != http.StatusForbidden || out["code"] != codeTerminalControls {
		t.Fatalf("the extension: %d %v", code, out)
	}
	if a := audited(); len(a) != 1 || !strings.Contains(a[0], `"action":"terminal_control_refused"`) ||
		!strings.Contains(a[0], `"path":"`+path+`"`) || strings.Contains(a[0], "[0m") {
		t.Fatalf("audited %q", a)
	}
	for who, by := range map[string]string{"the app": h.cfg.Token, "a phone": phone} {
		if code, out := call(t, srv.Config.Handler, "POST", path, by, string(bad)); code != http.StatusOK {
			t.Fatalf("%s: %d %v", who, code, out)
		}
	}
	if code, out := call(t, srv.Config.Handler, "POST", path, tok, `{"text":"carry on\n\tplease"}`); code != http.StatusOK {
		t.Fatalf("the extension's plain text: %d %v", code, out)
	}
	if a := audited(); len(a) != 1 {
		t.Fatalf("audited %q", a)
	}
}
