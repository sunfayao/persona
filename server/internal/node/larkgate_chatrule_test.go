package node

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sunfayao/persona/server/internal/connector"
)

// The gate proxy, which every request of the gated command passes however it was made, holds to the served rule the
// same way: the conversation of the rule goes through unasked, written in any way that means the same path (the proxy
// reads the path decoded once, and forwards that), and anything else, another conversation, a path that climbs out
// of it, a reply, a file, another method, needs the phone's yes first and so is refused here.
func TestGateProxyHoldsToTheServedChatRule(t *testing.T) {
	user, err := connector.ChatRuleFor("lark", "POST /conversations/F1/messages", json.RawMessage(`{"conversation":"F1"}`))
	if err != nil {
		t.Fatal(err)
	}
	api := newFakeAPI(t)
	n, _ := gateNode(t, api.URL, connector.LarkRules([]connector.Rule{user.Rule}, nil)...)
	base, release, err := n.gates().open("ex", "")
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	for _, c := range []struct {
		name, method, path string
		want               int
		reached            string // the path the API saw, when the call went on
	}{
		{"the conversation of the rule", "POST", "/api/v1/conversations/F1/messages", http.StatusOK, "/api/v1/conversations/F1/messages"},
		{"the same path, an id character encoded", "POST", "/api/v1/conversations/F%31/messages", http.StatusOK, "/api/v1/conversations/F1/messages"},
		{"another conversation", "POST", "/api/v1/conversations/F2/messages", http.StatusForbidden, ""},
		{"a longer id", "POST", "/api/v1/conversations/F11/messages", http.StatusForbidden, ""},
		{"a path that climbs out of it, encoded", "POST", "/api/v1/conversations/F1%2F..%2FF2/messages", http.StatusForbidden, ""},
		{"a path that climbs out of it, in two segments", "POST", "/api/v1/conversations/F1/messages/../../F2/messages", http.StatusForbidden, ""},
		{"a reply", "POST", "/api/v1/messages/M1/replies", http.StatusForbidden, ""},
		{"a reply under the conversation", "POST", "/api/v1/conversations/F1/messages/M1/replies", http.StatusForbidden, ""},
		{"a file", "POST", "/api/v1/conversations/F1/files", http.StatusForbidden, ""},
		{"another method", "PUT", "/api/v1/conversations/F1/messages", http.StatusForbidden, ""},
	} {
		before := len(api.requests())
		code, _ := send(t, c.method, base+c.path, `{"text":"hi"}`)
		seen := api.requests()[before:]
		if code != c.want || (c.reached == "") != (len(seen) == 0) || len(seen) > 0 && seen[0].Path != c.reached {
			t.Errorf("%s: %d, the API saw %+v; want %d reaching %q", c.name, code, seen, c.want, c.reached)
		}
	}
}
