package node

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

// requireCurlURLQuery skips a test that sends --url-query through the real curl: the flag arrived in curl 7.87, and
// an older curl (Debian 10 ships 7.64) rejects it with exit status 2 before any request leaves.
func requireCurlURLQuery(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	if out, err := exec.Command("curl", "--url-query", "a=b", "--version").CombinedOutput(); err != nil {
		t.Skipf("curl has no --url-query (7.87 or newer): %s", strings.TrimSpace(string(out)))
	}
}

func TestReviewConfirmedURLQueryFlag(t *testing.T) {
	requireCurlURLQuery(t)
	r := newSpaceRig(t)
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	const body = `{"text":"n"}`
	command := `ex POST '/notes/N1?a=1' '{"text":"n"}' --url-query 'b=2'`
	if decision := r.check(cwd, "s1", command); decision != "allow" {
		t.Fatalf("larkCheck = %q, want allow", decision)
	}
	if card := r.hub.lastCreated(t); !strings.Contains(card.Summary, "?a=1&b=2") {
		t.Errorf("confirmation card does not show the full query: %q", card.Summary)
	}

	curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
		"-X", http.MethodPost, "-H", "content-type: application/json", "--data-raw", body,
		"--url-query", "b=2", base+"/api/v1/notes/N1?a=1")
	out, err := curl.CombinedOutput()
	if err != nil {
		t.Fatalf("curl: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "200" {
		t.Errorf("confirmed command with --url-query returned %s, want 200", got)
	}
	got := r.api.requests()
	if len(got) != 1 || got[0].Query != "a=1&b=2" {
		t.Errorf("upstream requests = %+v, want the confirmed request once", got)
	}
}

func TestGateGetDataReadsKeepActualCurlSemantics(t *testing.T) {
	requireCurlURLQuery(t)
	tests := []struct {
		name, method, pathQuery string
		flags                   []string
		query                   string
	}{
		{name: "GET appends multiple data flags to an existing query", method: http.MethodGet, pathQuery: "existing=0",
			flags: []string{"--data-raw", "a=1", "--data", "b=2"}, query: "existing=0&a=1&b=2"},
		{name: "GET data replaces url-query additions", method: http.MethodGet,
			flags: []string{"--url-query", "u=1", "--data-raw", "d=2"}, query: "d=2"},
		{name: "GET data-urlencode uses form escaping", method: http.MethodGet,
			flags: []string{"--data-urlencode", "a=hello world", "--data-urlencode", "=x&y"}, query: "a=hello+world&x%26y"},
		{name: "HEAD also moves data into the query", method: http.MethodHead,
			flags: []string{"--data-raw", "a=1"}, query: "a=1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			path := "/notes/N1"
			if tc.pathQuery != "" {
				path += "?" + tc.pathQuery
			}
			command := fmt.Sprintf("ex %s %q -G", tc.method, path)
			for _, flag := range tc.flags {
				command += fmt.Sprintf(" %q", flag)
			}
			if decision := r.check(cwd, "s1", command); decision != "allow" {
				t.Fatalf("larkCheck = %q, want allow for a read", decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("read created %d cards, want zero", cards)
			}
			args := []string{"-sS", "-o", os.DevNull, "-w", "%{http_code}", "-X", tc.method, "-G"}
			if tc.method == http.MethodHead {
				args = append(args, "--head") // -X HEAD alone makes curl wait for a response body it will never receive.
			}
			args = append(args, tc.flags...)
			args = append(args, base+"/api/v1"+path)
			out, err := exec.Command("curl", args...).CombinedOutput()
			if err != nil {
				t.Fatalf("curl: %v: %s", err, out)
			}
			if code := strings.TrimSpace(string(out)); code != "200" {
				t.Errorf("read returned %s, want 200", code)
			}
			got := r.api.requests()
			if len(got) != 1 || got[0].Method != tc.method || got[0].Query != tc.query || got[0].Body != "" {
				t.Errorf("upstream requests = %+v, want %s with query %q and no body", got, tc.method, tc.query)
			}
		})
	}
}

func TestGateGetDataWritesAreRefused(t *testing.T) {
	tests := []struct {
		name, path string
		flags      []string
	}{
		{name: "explicit POST", path: "/notes/N1", flags: []string{"--data-raw", "a=1"}},
		{name: "multiple data flags", path: "/notes/N1", flags: []string{"--data-raw", "a=1", "--data", "b=2"}},
		{name: "existing raw query", path: "/notes/N1?existing=0", flags: []string{"--data-raw", "a=1"}},
		{name: "url-query", path: "/notes/N1", flags: []string{"--url-query", "u=1", "--data-raw", "d=2"}},
		{name: "data-urlencode", path: "/notes/N1", flags: []string{"--data-urlencode", "a=hello world"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			r.processIn(cwd, "s1")
			command := fmt.Sprintf("ex POST %q -G", tc.path)
			for _, flag := range tc.flags {
				command += fmt.Sprintf(" %q", flag)
			}
			if decision := r.check(cwd, "s1", command); decision != "deny" {
				t.Errorf("larkCheck = %q, want deny: -G moves the confirmed body into an unbound query", decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("-G data write created %d approval cards, want zero", cards)
			}
		})
	}
}

func TestGateNonAuthGetDataBodyScopedRule(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	const body = `{"text":"n"}`
	for _, tc := range []struct {
		name, decision string
		rule           proto.LarkRule
	}{
		{name: "body rule no longer matches", decision: "deny",
			rule: proto.LarkRule{Method: http.MethodPost, Path: "/notes/**", Body: map[string]any{"text": "n"}, Action: "allow"}},
		{name: "non-body rule still allows", decision: "allow",
			rule: proto.LarkRule{Method: http.MethodPost, Path: "/notes/**", Action: "allow"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			gate := exampleGate(r.api.URL)
			gate.Rules = []proto.LarkRule{tc.rule}
			r.n.mu.Lock()
			r.n.larkCfg.Gates = []proto.CommandGate{gate}
			r.n.mu.Unlock()
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			if decision := r.check(cwd, "s1", `ex POST '/notes/N1' '{"text":"n"}' -G`); decision != tc.decision {
				t.Errorf("larkCheck = %q, want %s", decision, tc.decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("rule-allowed -G parse created %d cards, want zero", cards)
			}
			if _, ok := firstLark(gate.Rules, larkCall{Method: http.MethodPost, Path: "/notes/N1", Body: body}); !ok {
				t.Fatal("test rule did not match the parsed body")
			}
			// Byte-level control only: a non-Auth script would send directly with its own token. larkCheck above is what
			// must stop the body-scoped case; this curl proves what the actual request body would have been.
			curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
				"-X", http.MethodPost, "-H", "content-type: application/json", "-H", "Authorization: Bearer test-token",
				"-G", "--data-raw", body, base+"/api/v1/notes/N1")
			out, err := curl.CombinedOutput()
			if err != nil {
				t.Fatalf("curl: %v: %s", err, out)
			}
			if code := strings.TrimSpace(string(out)); code != "200" {
				t.Fatalf("direct byte-control curl = %s, want 200", code)
			}
			got := r.api.requests()
			if len(got) != 1 || got[0].Body != "" || got[0].Query == "" {
				t.Fatalf("direct -G request = %+v, want data in query and an empty body", got)
			}
			actual := larkCall{Method: http.MethodPost, Path: "/notes/N1", Body: got[0].Body}
			_, actualMatches := firstLark(gate.Rules, actual)
			if tc.decision == "deny" && actualMatches {
				t.Errorf("empty actual body still satisfies the body-scoped rule")
			}
			if tc.decision == "allow" && !actualMatches {
				t.Errorf("non-body rule no longer matches the actual empty body")
			}
		})
	}
}

func TestGateAuthGetDataBodyScopedGETRule(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	const body = `{"text":"n"}`
	for _, tc := range []struct {
		name, decision, code string
		rule                 proto.LarkRule
	}{
		{name: "body rule no longer matches", decision: "deny", code: "403",
			rule: proto.LarkRule{Method: http.MethodGet, Path: "/notes/**", Body: map[string]any{"text": "n"}, Action: "allow"}},
		{name: "non-body rule still allows", decision: "allow", code: "200",
			rule: proto.LarkRule{Method: http.MethodGet, Path: "/notes/**", Action: "allow"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			gate := proxiedGate(r.api.URL)
			gate.Rules = []proto.LarkRule{tc.rule}
			r.n.mu.Lock()
			r.n.larkCfg.Gates = []proto.CommandGate{gate}
			r.n.mu.Unlock()
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			if decision := r.check(cwd, "s1", `ex GET '/notes/N1' '{"text":"n"}' -G`); decision != tc.decision {
				t.Errorf("larkCheck = %q, want %s", decision, tc.decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("rule-allowed -G parse created %d cards, want zero", cards)
			}
			curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
				"-X", http.MethodGet, "-H", "content-type: application/json", "-G", "--data-raw", body, base+"/api/v1/notes/N1")
			out, err := curl.CombinedOutput()
			if err != nil {
				t.Fatalf("curl: %v: %s", err, out)
			}
			if code := strings.TrimSpace(string(out)); code != tc.code {
				t.Errorf("proxy GET = %s, want %s", code, tc.code)
			}
			got := r.api.requests()
			if tc.decision == "deny" && len(got) != 0 {
				t.Errorf("empty-body GET reached upstream: %+v", got)
			}
			if tc.decision == "allow" && (len(got) != 1 || got[0].Body != "") {
				t.Errorf("non-body rule GET upstream = %+v, want one empty-body request", got)
			}
		})
	}
}

func TestGateGetDataRuleAllowedWriteStillAllow(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	r := newSpaceRig(t, proto.LarkRule{Method: http.MethodPost, Path: "/notes/**", Action: "allow"})
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	const command = `ex POST '/notes/N1?a=1' -G --data-raw 'b=2'`
	if decision := r.check(cwd, "s1", command); decision != "allow" {
		t.Fatalf("larkCheck = %q, want allow for a rule-allowed write", decision)
	}
	if cards := r.cards(); cards != 0 {
		t.Errorf("rule-allowed -G write created %d cards, want zero", cards)
	}
	curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
		"-X", http.MethodPost, "-G", "--data-raw", "b=2", base+"/api/v1/notes/N1?a=1")
	out, err := curl.CombinedOutput()
	if err != nil {
		t.Fatalf("curl: %v: %s", err, out)
	}
	if code := strings.TrimSpace(string(out)); code != "200" {
		t.Errorf("rule-allowed -G write returned %s, want 200", code)
	}
	got := r.api.requests()
	if len(got) != 1 || got[0].Query != "a=1&b=2" || got[0].Body != "" {
		t.Errorf("upstream requests = %+v, want the actual -G query and no body", got)
	}
	p := r.n.gates()
	p.mu.Lock()
	grants := len(p.grants)
	p.mu.Unlock()
	if grants != 0 {
		t.Errorf("rule-allowed -G write created %d grants, want zero", grants)
	}
}

func TestGateGetDataWriteCannotLendGrantToNonGetBody(t *testing.T) {
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("no curl")
	}
	r := newSpaceRig(t)
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	const body = `{"text":"n"}`
	command := `ex POST '/notes/N1' '` + body + `' -G`
	if decision := r.check(cwd, "s1", command); decision != "deny" {
		t.Errorf("larkCheck = %q, want deny for a -G data write", decision)
	}
	// Without -G this is the same method, path, query and JSON body the old parser put on the grant. It must not consume
	// an approval for curl's very different -G request, which has the body in its query and no request body.
	curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
		"-X", http.MethodPost, "-H", "content-type: application/json", "--data-raw", body, base+"/api/v1/notes/N1")
	out, err := curl.CombinedOutput()
	if err != nil {
		t.Fatalf("curl: %v: %s", err, out)
	}
	if code := strings.TrimSpace(string(out)); code != "403" {
		t.Errorf("non--get consumer returned %s, want 403", code)
	}
	if got := r.api.requests(); len(got) != 0 {
		t.Errorf("non--get consumer reached upstream: %+v", got)
	}
}

func TestGateURLQueryFragmentTwoOutstandingGrants(t *testing.T) {
	requireCurlURLQuery(t)
	r := newSpaceRig(t)
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	const body = `{"text":"n"}`
	if decision := r.check(cwd, "s1", `ex POST '/notes/N1?a' '{"text":"n"}'`); decision != "allow" {
		t.Fatalf("first larkCheck = %q, want allow", decision)
	}
	const fragmentCommand = `ex POST '/notes/N1' '{"text":"n"}' --url-query 'a#b=c' --url-query 'c=3'`
	if decision := r.check(cwd, "s1", fragmentCommand); decision != "allow" {
		t.Fatalf("fragment larkCheck = %q, want allow", decision)
	}
	if card := r.hub.lastCreated(t); !strings.Contains(card.Summary, "?a") || strings.Contains(card.Summary, "&c=3") {
		t.Errorf("fragment confirmation is not the final URL query ?a: %q", card.Summary)
	}
	if code, response := send(t, http.MethodPost, base+"/api/v1/notes/N1?a", body); code != http.StatusOK {
		t.Errorf("first outstanding ?a grant = %d %s, want 200", code, response)
	}
	curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
		"-X", http.MethodPost, "-H", "content-type: application/json", "--data-raw", body,
		"--url-query", "a#b=c", "--url-query", "c=3", base+"/api/v1/notes/N1")
	out, err := curl.CombinedOutput()
	if err != nil {
		t.Fatalf("curl: %v: %s", err, out)
	}
	if code := strings.TrimSpace(string(out)); code != "200" {
		t.Errorf("real fragment command returned %s, want the second outstanding ?a grant", code)
	}
	got := r.api.requests()
	if len(got) != 2 {
		t.Errorf("upstream requests = %+v, want two ?a requests", got)
	}
	for _, request := range got {
		if request.Query != "a" || request.Auth != "Bearer test-token" {
			t.Errorf("upstream request = %+v, want ?a with gate credential", request)
		}
	}
	p := r.n.gates()
	p.mu.Lock()
	remaining := len(p.grants)
	p.mu.Unlock()
	if remaining != 0 {
		t.Errorf("two ?a requests left %d grants, want both consumed", remaining)
	}
}

func TestGateURLQueryEmptyFragmentTwoOutstandingGrants(t *testing.T) {
	requireCurlURLQuery(t)
	r := newSpaceRig(t)
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	const body = `{"text":"n"}`
	if decision := r.check(cwd, "s1", `ex POST '/notes/N1' '{"text":"n"}'`); decision != "allow" {
		t.Fatalf("first larkCheck = %q, want allow", decision)
	}
	const fragmentCommand = `ex POST '/notes/N1' '{"text":"n"}' --url-query '+#frag' --url-query 'c=3'`
	if decision := r.check(cwd, "s1", fragmentCommand); decision != "allow" {
		t.Fatalf("fragment larkCheck = %q, want allow", decision)
	}
	if card := r.hub.lastCreated(t); strings.Contains(card.Summary, "?") {
		t.Errorf("empty-fragment confirmation shows a query: %q", card.Summary)
	}
	if code, response := send(t, http.MethodPost, base+"/api/v1/notes/N1?c=3", body); code != http.StatusForbidden {
		t.Errorf("parser-only c=3 consumer = %d %s, want 403", code, response)
	}
	if code, response := send(t, http.MethodPost, base+"/api/v1/notes/N1", body); code != http.StatusOK {
		t.Errorf("first empty-query grant = %d %s, want 200", code, response)
	}
	curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
		"-X", http.MethodPost, "-H", "content-type: application/json", "--data-raw", body,
		"--url-query", "+#frag", "--url-query", "c=3", base+"/api/v1/notes/N1")
	out, err := curl.CombinedOutput()
	if err != nil {
		t.Fatalf("curl: %v: %s", err, out)
	}
	if code := strings.TrimSpace(string(out)); code != "200" {
		t.Errorf("real empty-fragment command returned %s, want the second empty-query grant", code)
	}
	got := r.api.requests()
	if len(got) != 2 || got[0].Query != "" || got[1].Query != "" {
		t.Errorf("upstream requests = %+v, want exactly two empty-query requests", got)
	}
	p := r.n.gates()
	p.mu.Lock()
	remaining := len(p.grants)
	p.mu.Unlock()
	if remaining != 0 {
		t.Errorf("two empty-query requests left %d grants, want both consumed", remaining)
	}
}

func TestGateURLQueryVariableReferencesCannotBeConfirmed(t *testing.T) {
	for _, tc := range []struct{ name, quoted string }{
		{name: "double quoted", quoted: `"a=$value"`},
		{name: "single quoted", quoted: `'a=$value'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			// Shell quote information is gone when parseGateCall sees the argument. Even a previously known value cannot
			// make expansion safe: the single-quoted form must remain literal while the double-quoted form would expand.
			command := `value=expanded; ex POST '/notes/N1' '{"text":"n"}' --url-query ` + tc.quoted
			if decision := r.check(cwd, "s1", command); decision != "deny" {
				t.Errorf("larkCheck = %q, want deny for either quoting of a variable reference", decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("variable reference created %d cards, want zero", cards)
			}
			p := r.n.gates()
			p.mu.Lock()
			grants := len(p.grants)
			p.mu.Unlock()
			if grants != 0 {
				t.Errorf("variable reference created %d grants, want zero", grants)
			}
		})
	}
}

func TestGateURLQueryLiteralDollarNeedsNoExpansion(t *testing.T) {
	c, deny, ok := parseGateCall(proxiedGate("http://example.invalid"),
		[]string{"POST", "/notes/N1", `{"text":"n"}`, "--url-query", "a=cost$"}, nil, t.TempDir())
	if !ok || deny != "" {
		t.Fatalf("parseGateCall = ok %v, deny %q", ok, deny)
	}
	if c.queryUnknown || c.Query != "a=cost%24" {
		t.Errorf("parseGateCall query = %q, unknown %v; want literal dollar a=cost%%24", c.Query, c.queryUnknown)
	}
}

func TestGateURLQueryPreencodedLiteralVariableBytes(t *testing.T) {
	requireCurlURLQuery(t)
	r := newSpaceRig(t)
	cwd := t.TempDir()
	base := r.processIn(cwd, "s1")
	// With no shell reference in the command, leading + preserves the percent-encoded dollar as literal query bytes.
	const command = `ex POST '/notes/N1' '{"text":"n"}' --url-query '+a=%24value'`
	if decision := r.check(cwd, "s1", command); decision != "allow" {
		t.Fatalf("larkCheck = %q, want allow for literal preencoded bytes", decision)
	}
	curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
		"-X", http.MethodPost, "-H", "content-type: application/json", "--data-raw", `{"text":"n"}`,
		"--url-query", "+a=%24value", base+"/api/v1/notes/N1")
	out, err := curl.CombinedOutput()
	if err != nil {
		t.Fatalf("curl: %v: %s", err, out)
	}
	if code := strings.TrimSpace(string(out)); code != "200" {
		t.Errorf("preencoded literal query returned %s, want 200", code)
	}
	if got := r.api.requests(); len(got) != 1 || got[0].Query != "a=%24value" {
		t.Errorf("upstream requests = %+v, want literal a=%%24value", got)
	}
}

func TestGateURLQueryUnknownConfirmedAndAllowedGET(t *testing.T) {
	requireCurlURLQuery(t)
	queries := []struct {
		name, suffix, stdin, query string
		args                       []string
	}{
		{name: "-G data", suffix: ` -G --data-raw 'a=1'`, args: []string{"-G", "--data-raw", "a=1"}, query: "a=1"},
		{name: "unknown expansion", suffix: ` --url-query "a=$missing"`, args: []string{"--url-query", "a="}, query: "a="},
		{name: "stdin", suffix: ` --url-query '@-'`, args: []string{"--url-query", "@-"}, stdin: "a=1", query: "a%3d1"},
	}
	routes := []struct {
		name, path, decision, code string
	}{
		{name: "confirmed GET", path: "/links/open", decision: "deny", code: "403"},
		{name: "rule-allowed GET", path: "/me", decision: "allow", code: "200"},
	}
	for _, query := range queries {
		for _, route := range routes {
			t.Run(query.name+"/"+route.name, func(t *testing.T) {
				r := newSpaceRig(t)
				cwd := t.TempDir()
				base := r.processIn(cwd, "s1")
				command := fmt.Sprintf("ex GET %q%s", route.path, query.suffix)
				if decision := r.check(cwd, "s1", command); decision != route.decision {
					t.Errorf("larkCheck = %q, want %s", decision, route.decision)
				}
				if cards := r.cards(); cards != 0 {
					t.Errorf("unknown query created %d cards, want zero", cards)
				}
				args := []string{"-sS", "-o", os.DevNull, "-w", "%{http_code}", "-X", http.MethodGet}
				args = append(args, query.args...)
				args = append(args, base+"/api/v1"+route.path)
				curl := exec.Command("curl", args...)
				curl.Stdin = strings.NewReader(query.stdin)
				out, err := curl.CombinedOutput()
				if err != nil {
					t.Fatalf("curl: %v: %s", err, out)
				}
				if code := strings.TrimSpace(string(out)); code != route.code {
					t.Errorf("GET returned %s, want %s", code, route.code)
				}
				got := r.api.requests()
				if route.decision == "deny" {
					if len(got) != 0 {
						t.Errorf("unconfirmed GET reached upstream: %+v", got)
					}
				} else if len(got) != 1 || got[0].Query != query.query {
					t.Errorf("allowed GET upstream = %+v, want query %q", got, query.query)
				}
				p := r.n.gates()
				p.mu.Lock()
				grants := len(p.grants)
				p.mu.Unlock()
				if grants != 0 {
					t.Errorf("unknown query left %d grants, want zero", grants)
				}
			})
		}
	}
}

func TestGateNonAuthUnknownQueryNeedsConfirmation(t *testing.T) {
	commands := []struct{ name, command string }{
		{name: "-G data", command: `ex POST '/notes/N1' -G --data-raw 'a=1'`},
		{name: "unknown expansion", command: `ex POST '/notes/N1' '{"text":"n"}' --url-query "a=$missing"`},
		{name: "stdin", command: `ex POST '/notes/N1' '{"text":"n"}' --url-query '@-'`},
	}
	for _, tc := range commands {
		for _, ruleAllowed := range []bool{false, true} {
			name := tc.name + "/confirmation required"
			if ruleAllowed {
				name = tc.name + "/rule allowed"
			}
			t.Run(name, func(t *testing.T) {
				r := newSpaceRig(t)
				var rules []proto.LarkRule
				if ruleAllowed {
					rules = []proto.LarkRule{{Method: http.MethodPost, Path: "/notes/**", Action: "allow"}}
				}
				// A non-Auth gate's script owns the token and talks to the API directly: larkCheck is the only backstop.
				r.n.mu.Lock()
				r.n.larkCfg.Gates = []proto.CommandGate{exampleGate(r.api.URL, rules...)}
				r.n.mu.Unlock()
				cwd := t.TempDir()
				want := "deny"
				if ruleAllowed {
					want = "allow"
				}
				if decision := r.check(cwd, "s1", tc.command); decision != want {
					t.Errorf("larkCheck = %q, want %s for a non-Auth direct command", decision, want)
				}
				if cards := r.cards(); cards != 0 {
					t.Errorf("non-Auth unknown query created %d cards, want zero", cards)
				}
				if got := r.api.requests(); len(got) != 0 {
					t.Errorf("test unexpectedly executed a direct command: %+v", got)
				}
			})
		}
	}
}

func TestGateURLQueryUnresolvedExpansionCannotBeConfirmed(t *testing.T) {
	for _, query := range []string{"a=$missing", "a=$(printf expanded)", "a=`printf expanded`", "a=$1", "a=$@", "a=$?", "a=${value:-default}"} {
		t.Run(query, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			command := fmt.Sprintf(`ex POST '/notes/N1' '{"text":"n"}' --url-query %q`, query)
			if decision := r.check(cwd, "s1", command); decision != "deny" {
				t.Errorf("larkCheck = %q, want deny for an execution-time query expansion", decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("unresolved expansion created %d cards, want zero", cards)
			}
			if query == "a=$missing" {
				// The old parser granted the literal percent-encoded dollar. That request is not what a shell executes.
				if code, response := send(t, http.MethodPost, base+"/api/v1/notes/N1?a=%24missing", `{"text":"n"}`); code != http.StatusForbidden {
					t.Errorf("parser-only unresolved-variable query = %d %s, want 403", code, response)
				}
			}
			if got := r.api.requests(); len(got) != 0 {
				t.Errorf("unresolved-variable consumer reached upstream: %+v", got)
			}
			p := r.n.gates()
			p.mu.Lock()
			grants := len(p.grants)
			p.mu.Unlock()
			if grants != 0 {
				t.Errorf("unresolved expansion created %d grants, want zero", grants)
			}
		})
	}
}

func TestGateURLQueryUnresolvedExpansionRulesStillAllow(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost} {
		t.Run(method, func(t *testing.T) {
			var rules []proto.LarkRule
			if method == http.MethodPost {
				rules = []proto.LarkRule{{Method: http.MethodPost, Path: "/notes/**", Action: "allow"}}
			}
			r := newSpaceRig(t, rules...)
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			command := fmt.Sprintf(`ex %s '/notes/N1'`, method)
			body := ""
			if method == http.MethodPost {
				command += ` '{"text":"n"}'`
				body = `{"text":"n"}`
			}
			command += ` --url-query "a=$missing"`
			if decision := r.check(cwd, "s1", command); decision != "allow" {
				t.Fatalf("larkCheck = %q, want allow for a call that needs no grant", decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("unresolved expansion without a grant created %d cards", cards)
			}
			// An unset shell variable expands to an empty value before curl sees a=.
			if code, response := send(t, method, base+"/api/v1/notes/N1?a=", body); code != http.StatusOK {
				t.Errorf("actual expanded %s = %d %s, want 200", method, code, response)
			}
			if got := r.api.requests(); len(got) != 1 || got[0].Query != "a=" {
				t.Errorf("upstream requests = %+v, want actual expanded a=", got)
			}
			p := r.n.gates()
			p.mu.Lock()
			grants := len(p.grants)
			p.mu.Unlock()
			if grants != 0 {
				t.Errorf("call without a grant created %d grants", grants)
			}
		})
	}
}

func TestGateURLQueryRelativeFile(t *testing.T) {
	requireCurlURLQuery(t)
	const body = `{"text":"n"}`
	for _, tc := range []struct{ name, flag, query string }{
		{name: "unnamed", flag: "@query.txt", query: "a%3drelative%2b%ce%b2%0a"},
		{name: "named", flag: "name@query.txt", query: "name=a%3drelative%2b%ce%b2%0a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			if err := os.WriteFile(filepath.Join(cwd, "query.txt"), []byte("a=relative+β\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			base := r.processIn(cwd, "s1")
			command := fmt.Sprintf(`ex POST '/notes/N1' '{"text":"n"}' --url-query %q`, tc.flag)
			if decision := r.check(cwd, "s1", command); decision != "allow" {
				t.Fatalf("larkCheck = %q, want allow", decision)
			}
			if card := r.hub.lastCreated(t); !strings.Contains(card.Summary, "?"+tc.query) {
				t.Errorf("confirmation card does not show the file's encoded query %q: %q", tc.query, card.Summary)
			}
			curl := exec.Command("curl", "-sS", "-o", os.DevNull, "-w", "%{http_code}",
				"-X", http.MethodPost, "-H", "content-type: application/json", "--data-raw", body,
				"--url-query", tc.flag, base+"/api/v1/notes/N1")
			curl.Dir = cwd
			out, err := curl.CombinedOutput()
			if err != nil {
				t.Fatalf("curl: %v: %s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != "200" {
				t.Errorf("confirmed relative-file query returned %s, want 200", got)
			}
			if got := r.api.requests(); len(got) != 1 || got[0].Query != tc.query {
				t.Errorf("upstream requests = %+v, want the confirmed file query once", got)
			}
		})
	}
}

func TestGateURLQueryStdinRulesStillAllow(t *testing.T) {
	requireCurlURLQuery(t)
	for _, tc := range []struct {
		name, method, path, flag, query string
		rules                           []proto.LarkRule
	}{
		{name: "read", method: http.MethodGet, path: "/me", flag: "@-", query: "a%3dstdin%2bx%0a"},
		{name: "named read", method: http.MethodGet, path: "/me", flag: "name@-", query: "name=a%3dstdin%2bx%0a"},
		{name: "rule-allowed write", method: http.MethodPost, path: "/notes/N1", flag: "@-", query: "a%3dstdin%2bx%0a",
			rules: []proto.LarkRule{{Method: http.MethodPost, Path: "/notes/**", Action: "allow"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newSpaceRig(t, tc.rules...)
			cwd := t.TempDir()
			base := r.processIn(cwd, "s1")
			command := fmt.Sprintf("ex %s %q", tc.method, tc.path)
			if tc.method == http.MethodPost {
				command += ` '{"text":"n"}'`
			}
			command += fmt.Sprintf(" --url-query %q", tc.flag)
			if decision := r.check(cwd, "s1", command); decision != "allow" {
				t.Fatalf("larkCheck = %q, want allow for a call that needs no query-bound grant", decision)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("rule-allowed stdin query created %d cards, want zero", cards)
			}
			args := []string{"-sS", "-o", os.DevNull, "-w", "%{http_code}", "-X", tc.method}
			if tc.method == http.MethodPost {
				args = append(args, "-H", "content-type: application/json", "--data-raw", `{"text":"n"}`)
			}
			args = append(args, "--url-query", tc.flag, base+"/api/v1"+tc.path)
			curl := exec.Command("curl", args...)
			curl.Stdin = strings.NewReader("a=stdin+x\n")
			out, err := curl.CombinedOutput()
			if err != nil {
				t.Fatalf("curl: %v: %s", err, out)
			}
			if got := strings.TrimSpace(string(out)); got != "200" {
				t.Errorf("rule-allowed stdin query returned %s, want 200", got)
			}
			if got := r.api.requests(); len(got) != 1 || got[0].Query != tc.query {
				t.Errorf("upstream requests = %+v, want the actual stdin query once", got)
			}
			p := r.n.gates()
			p.mu.Lock()
			grants := len(p.grants)
			p.mu.Unlock()
			if grants != 0 {
				t.Errorf("rule-allowed stdin query created %d grants, want zero", grants)
			}
		})
	}
}

func TestGateURLQueryStdinCannotBeConfirmed(t *testing.T) {
	for _, query := range []string{"@-", "name@-"} {
		t.Run(query, func(t *testing.T) {
			r := newSpaceRig(t)
			cwd := t.TempDir()
			r.processIn(cwd, "s1")
			command := fmt.Sprintf(`ex POST '/notes/N1' '{"text":"n"}' --url-query %q`, query)
			if decision := r.check(cwd, "s1", command); decision != "deny" {
				t.Errorf("larkCheck = %q, want deny: curl reads %s from stdin, which the approval cannot see", decision, query)
			}
			if cards := r.cards(); cards != 0 {
				t.Errorf("stdin query created %d approval cards, want zero", cards)
			}
			p := r.n.gates()
			p.mu.Lock()
			grants := len(p.grants)
			p.mu.Unlock()
			if grants != 0 {
				t.Errorf("stdin query created %d grants, want zero", grants)
			}
		})
	}
}
