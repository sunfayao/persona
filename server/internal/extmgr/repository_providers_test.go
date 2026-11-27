package extmgr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/sdk/go/wire"
	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/extension"
	"github.com/sunfayao/persona/server/internal/extension/host"
	pserve "github.com/sunfayao/persona/server/internal/extension/serve"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// acmeProvider is the fake provider's id: extension acme-code, contribution acme.
const acmeProvider = "ext:acme-code/acme"

// serveRepositoryProviderExtensionForTest is acme-code's program. Its repo namespace answers by the address's last
// segment, a few of them with an answer the manager must refuse, and writes each call's method and params as a line of
// JSON to REPOS_OUT.
func serveRepositoryProviderExtensionForTest() {
	out := os.Getenv("REPOS_OUT")
	var mu sync.Mutex
	note := func(method string, params json.RawMessage) {
		mu.Lock()
		defer mu.Unlock()
		if f, err := os.OpenFile(out, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			b, _ := json.Marshal(map[string]any{"method": method, "params": params})
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
	last := func(s string) string { return s[strings.LastIndex(s, "/")+1:] }
	_ = pserve.Serve(context.Background(), os.Stdin, os.Stdout, pserve.Program{ID: "acme-code", Version: "1.0.0",
		Namespaces: map[string]pserve.Handler{
			"repo": func(ctx context.Context, method string, params json.RawMessage) (any, error) {
				note(method, params)
				switch method {
				case "parse":
					var p wire.RepoParseParams
					_ = json.Unmarshal(params, &p)
					p.URL, _, _ = strings.Cut(p.URL, "#")
					name := strings.TrimSuffix(last(p.URL), ".git")
					if _, after, ok := strings.Cut(name, ":"); ok { // git@ssh.acme.test:name
						name = after
					}
					res := wire.RepoParseResult{InstanceURL: "https://code.acme.test", Path: "team/" + name,
						CanonicalURL: "https://code.acme.test/team/" + name, CloneURL: "ssh://git@ssh.acme.test/team/" + name + ".git",
						DisplayName: "Acme\n" + name}
					switch name {
					case "elsewhere": // a host it does not claim
						res.InstanceURL, res.CanonicalURL = "https://code.other.test", "https://code.other.test/team/elsewhere"
					case "plain":
						res.InstanceURL, res.CanonicalURL = "http://code.acme.test", "http://code.acme.test/team/plain"
					case "creds":
						res.InstanceURL, res.CanonicalURL = "https://user:pw@code.acme.test", "https://user:pw@code.acme.test/team/creds"
					case "clonecreds":
						res.CloneURL = "https://user:pw@code.acme.test/team/clonecreds.git"
					case "mismatch":
						res.CanonicalURL = "https://code.acme.test/team/other"
					}
					return res, nil
				case "check":
					var p wire.RepoCheckParams
					_ = json.Unmarshal(params, &p)
					switch last(p.CanonicalURL) {
					case "ok":
						return wire.Empty{}, nil
					case "missing":
						return nil, &rpc.Error{Code: "not_found", Message: "no such repository"}
					case "signed_out":
						return nil, &rpc.Error{Code: "not_signed_in", Message: "token\nrevoked"}
					case "refuse":
						return nil, &rpc.Error{Code: "refused", Message: "policy\nsays no " + strings.Repeat("and more words ", 30)}
					case "noisy":
						return nil, &rpc.Error{Code: "refused", Message: "policy\x1b[31m says\x00 no\x1c\x1dmore"}
					case "gone": // as a version that no longer has the contribution
						return nil, pserve.NoContribution(p.Contribution)
					case "slow": // answers only when the hub gives up on the call (or the program stops)
						<-ctx.Done()
						return nil, ctx.Err()
					case "crash": // the program falls over
						os.Exit(3)
					}
					return nil, errors.New("the source fell over")
				case "link":
					var p wire.RepoLinkParams
					_ = json.Unmarshal(params, &p)
					p.URL, _, _ = strings.Cut(p.URL, "#")
					base := p.Repo.CanonicalURL
					segment := map[string]string{"issue": "issues", "pull_request": "merge_requests"}
					if p.URL == "" {
						number := p.Number
						if number == "13" { // another link than asked
							number = "14"
						}
						return wire.RepoLinkResult{Kind: p.Kind, Number: number, CanonicalURL: base + "/-/" + segment[p.Kind] + "/" + number}, nil
					}
					switch last(p.URL) {
					case "outside":
						return wire.RepoLinkResult{Kind: "issue", Number: "3", CanonicalURL: "https://code.other.test/team/ok/-/issues/3"}, nil
					case "sibling":
						return wire.RepoLinkResult{Kind: "issue", Number: "3", CanonicalURL: base + "-two/-/issues/3"}, nil
					}
					kind := "issue"
					if strings.Contains(p.URL, "/merge_requests/") {
						kind = "pull_request"
					}
					return wire.RepoLinkResult{Kind: kind, Number: last(p.URL), CanonicalURL: base + "/-/" + segment[kind] + "/" + last(p.URL),
						Title: "Fix\nthe build"}, nil
				case "describe":
					var p wire.RepoDescribeParams
					_ = json.Unmarshal(params, &p)
					switch last(p.CanonicalURL) {
					case "old": // as a version without the optional method
						return nil, pserve.UnknownMethod(method)
					case "garbled": // not an answer of the method's shape
						return "garbled", nil
					}
					return wire.RepoDescribeResult{Title: "Fix\tthe build", State: "merged"}, nil
				case "pr_status":
					var p wire.RepoPRStatusParams
					_ = json.Unmarshal(params, &p)
					yes := true
					switch last(p.CanonicalURL) {
					case "7":
						return wire.RepoPRStatusResult{State: "merged", MergeCommit: "abc1234", HeadSha: "NOT-HEX", HeadRef: "fix/build",
							BaseRef: "main", Author: "someone\nelse", ViewerIsAuthor: &yes}, nil
					case "8":
						return wire.RepoPRStatusResult{State: "draft"}, nil
					}
					return wire.RepoPRStatusResult{State: "open", MergeCommit: "abc1234"}, nil
				}
				return nil, pserve.UnknownMethod(method)
			}}})
}

// repositoryProviderPackage is acme-code's package as id, started with the hub so that a stopped one stays stopped
// (unless extra, more manifest fields, has its own activation_events).
func repositoryProviderPackage(t *testing.T, id, out string, extra ...string) string {
	dir := t.TempDir()
	self, _ := os.Executable()
	_ = os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "bin", "code"), []byte("#!/bin/sh\nPERSONA_TEST_PLUGIN=repos exec "+self+" \"$@\"\n"), 0o755)
	activation, fields, capabilities := `"activation_events": ["on_startup"], `, "", ""
	for _, f := range extra {
		if f == prStatusCapability {
			capabilities = `, "capabilities": ["pr_status"]`
			continue
		}
		if strings.HasPrefix(f, `"activation_events"`) {
			activation = ""
		}
		fields += f + ", "
	}
	manifest := fmt.Sprintf(`{"manifest_version": 1, "id": %q, "name": "Acme Code", "version": "1.0.0", %s%s
		"engines": {"plugin_protocol": 1}, "runtime": {"command": {%q: "bin/code"}, "env": {"REPOS_OUT": %q}},
		"contributes": {"repository_providers": [{"id": "acme", "name": "Acme Code", "hosts": ["code.acme.test", "ssh.acme.test"],
			"link_kinds": [{"kind": "pull_request", "label": "MR"}]%s}]}}`, id, activation, fields, runtime.GOOS+"-"+runtime.GOARCH, out, capabilities)
	_ = os.WriteFile(filepath.Join(dir, extension.ManifestFile), []byte(manifest), 0o644)
	return dir
}

// prStatusCapability, given to repositoryProviderPackage, declares the pr_status capability on its contribution.
const prStatusCapability = "capabilities:pr_status"

// installRepositoryProvider installs acme-code and waits for its program to answer; the calls it gets go to the
// file it returns.
func installRepositoryProvider(t *testing.T, h *Manager, mux http.Handler) string {
	t.Helper()
	out := installRepositoryProviderPackage(t, h, mux)
	waitRepositoryProviderUp(t, h)
	return out
}

// installRepositoryProviderPackage installs acme-code (with extra manifest fields) without waiting for it.
func installRepositoryProviderPackage(t *testing.T, h *Manager, mux http.Handler, extra ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "calls.jsonl")
	c, pv := call(t, mux, "POST", "/api/v1/plugins/-/preview", h.cfg.Token, `{"source": "dir:`+repositoryProviderPackage(t, "acme-code", out, extra...)+`"}`)
	if c != 200 || !strings.Contains(fmt.Sprint(pv["permissions"]), "code.acme.test") {
		t.Fatalf("the install page names the hosts it reads: %d %v", c, pv)
	}
	if c, body := call(t, mux, "POST", "/api/v1/plugins/-/install", h.cfg.Token, fmt.Sprintf(`{"preview_id": %q, "permissions_digest": %q}`, pv["preview_id"], pv["permissions_digest"])); c != 200 {
		t.Fatalf("install: %d %v", c, body)
	}
	return out
}

func waitRepositoryProviderUp(t *testing.T, h *Manager) {
	t.Helper()
	waitLong(t, "the provider answering", func() bool {
		return h.RepositoryCheck(context.Background(), acmeRepository("ok")) == nil
	})
}

// acmeRepository is team/<name> on code.acme.test, as repo.parse answers it.
func acmeRepository(name string) repository.Identity {
	return repository.Identity{Provider: acmeProvider, InstanceURL: "https://code.acme.test", Path: "team/" + name,
		CanonicalURL: "https://code.acme.test/team/" + name}
}

// providerErr is err's ProviderError, or a failure of the test.
func providerErr(t *testing.T, err error) *repository.ProviderError {
	t.Helper()
	var pe *repository.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("not a ProviderError: %v", err)
	}
	return pe
}

// repoCalls is how many calls the program wrote to path.
func repoCalls(path string) int {
	raw, _ := os.ReadFile(path)
	return strings.Count(string(raw), "\n")
}

// A provider whose program calls start (on_call) is available while it is idle, and a call starts it; once its program
// has failed it is off, as its calls answer, while it waits out its backoff.
func TestRepositoryProviderLazyProgram(t *testing.T) {
	saved := host.RestartFirst
	host.RestartFirst = 50 * time.Millisecond // tried again soon after it falls over (restored after the hub stops)
	t.Cleanup(func() { host.RestartFirst = saved })
	h, _ := extensionTestHub(t)
	mux := h.routes()
	ctx := context.Background()
	out := installRepositoryProviderPackage(t, h, mux, `"activation_events": ["on_call"]`)
	state := func() string {
		info, _ := h.RepositoryProvider(acmeProvider)
		return info.State
	}

	// Idle: nothing has started its program yet, and it is available.
	waitLong(t, "its program handle", func() bool {
		hp := h.extensions.get("acme-code")
		return hp != nil && hp.proc != nil
	})
	proc := h.extensions.get("acme-code").proc
	if st, why := proc.Status(); st != host.StateStopped || !proc.Spec().OnCall || repoCalls(out) != 0 {
		t.Fatalf("not idle: %s %q, %d calls", st, why, repoCalls(out))
	}
	if got := h.RepositoryProviders(); len(got) != 1 || got[0].State != repository.ProviderAvailable {
		t.Fatalf("idle, listed: %+v", got)
	}
	if got := state(); got != repository.ProviderAvailable {
		t.Fatalf("idle, by id: %s", got)
	}

	// A call starts it.
	if err := h.RepositoryCheck(ctx, acmeRepository("ok")); err != nil {
		t.Fatalf("a call to an idle provider: %v", err)
	}
	if st, _ := proc.Status(); st != host.StateRunning || state() != repository.ProviderAvailable {
		t.Fatalf("started by the call: %s, %s", st, state())
	}

	// Its program falls over: still available, since calls start it again, and a later call reaches it.
	if pe := providerErr(t, h.RepositoryCheck(ctx, acmeRepository("crash"))); pe.Code != "failed" {
		t.Fatalf("the call it fell over in: %+v", pe)
	}
	if got, ok := h.RepositoryProviderForHost("ssh.acme.test"); !ok || got.State != repository.ProviderAvailable {
		t.Fatalf("fell over, by host: %+v %v", got, ok)
	}
	sent := repoCalls(out)
	waitLong(t, "a call answered again", func() bool { return h.RepositoryCheck(ctx, acmeRepository("ok")) == nil })
	if repoCalls(out) == sent || state() != repository.ProviderAvailable {
		t.Fatalf("after falling over: %d calls reached it, %s", repoCalls(out)-sent, state())
	}
}

// Before the manager has started, lists are empty without waiting, and a call waits for startup as long as its
// context lets it. (Installing needs a started manager, so here there are no extensions: once started, off.)
func TestRepositoryProvidersBeforeStartup(t *testing.T) {
	h := New(Options{Dir: shortTempDir(t), Config: &Config{}})
	done := make(chan struct{})
	go func() {
		defer close(done)
		if got := h.RepositoryProviders(); got != nil {
			t.Errorf("listed: %+v", got)
		}
		if _, ok := h.RepositoryProvider(acmeProvider); ok {
			t.Error("by id")
		}
		if _, ok := h.RepositoryProviderForHost("code.acme.test"); ok {
			t.Error("by host")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a list waited for startup")
	}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := h.RepositoryParse(ctx, acmeProvider, "https://code.acme.test/team/ok")
	if took := time.Since(start); providerErr(t, err).Code != "provider_off" || took < 50*time.Millisecond {
		t.Fatalf("not started: %v after %s", err, took)
	}

	// Started while a call waits: it goes on then, not when its context ends.
	start = time.Now()
	time.AfterFunc(100*time.Millisecond, func() { close(h.started) })
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = h.RepositoryCheck(ctx, acmeRepository("ok"))
	if took := time.Since(start); providerErr(t, err).Code != "provider_off" || took < 100*time.Millisecond || took > 2*time.Second {
		t.Fatalf("started during the call: %v after %s", err, took)
	}
}

// The hosts no extension may claim are the ones the built-in providers read.
func TestRepositoryHostsReservedForBuiltins(t *testing.T) {
	reserved, builtin := slices.Clone(extension.ReservedRepositoryHosts), slices.Clone(repository.BuiltinHosts)
	slices.Sort(reserved)
	slices.Sort(builtin)
	if !slices.Equal(reserved, builtin) {
		t.Fatalf("extension.ReservedRepositoryHosts %v, repository.BuiltinHosts %v", reserved, builtin)
	}
}

// #11584: repo.pr_status is asked only of a contribution that declares pr_status, about a link on its hosts; its
// answer is re-checked: a state outside the enum fails it, a value that breaks its rule is dropped.
func TestRepositoryProviderPRStatus(t *testing.T) {
	h, _ := extensionTestHub(t)
	ctx := context.Background()
	mux := h.routes()
	out := installRepositoryProviderPackage(t, h, mux, prStatusCapability)
	waitRepositoryProviderUp(t, h)
	if info, ok := h.RepositoryProvider(acmeProvider); !ok || !slices.Equal(info.Capabilities, []string{"pr_status"}) {
		t.Fatalf("listed: %+v", info)
	}
	got, err := h.RepositoryPRStatus(ctx, acmeProvider, "https://code.acme.test/team/ok/-/merge_requests/7")
	yes := true
	want := repository.ReviewStatus{State: "merged", MergeCommit: "abc1234", HeadRef: "fix/build", BaseRef: "main", ViewerIsAuthor: &yes}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("merged: %+v %v", got, err)
	}
	if !strings.Contains(readFile(t, out), `{"method":"pr_status","params":{"contribution":"acme","canonical_url":"https://code.acme.test/team/ok/-/merge_requests/7"}}`) {
		t.Fatalf("what repo.pr_status was told: %s", readFile(t, out))
	}
	if got, err := h.RepositoryPRStatus(ctx, acmeProvider, "https://code.acme.test/team/ok/-/merge_requests/9"); err != nil ||
		got.State != "open" || got.MergeCommit != "" {
		t.Fatalf("open, a merge commit dropped: %+v %v", got, err)
	}
	_, err = h.RepositoryPRStatus(ctx, acmeProvider, "https://code.acme.test/team/ok/-/merge_requests/8")
	if pe := providerErr(t, err); pe.Code != "failed" || !strings.Contains(pe.Why, "state is not open, closed or merged") {
		t.Fatalf("a state outside the enum: %+v", pe)
	}
	sent := repoCalls(out)
	for _, link := range []string{"https://code.other.test/team/ok/-/merge_requests/7", "https://code.acme.test/team/ok/-/merge_requests/7?private_token=x"} {
		if _, err := h.RepositoryPRStatus(ctx, acmeProvider, link); providerErr(t, err).Code != "failed" {
			t.Fatalf("%s: %v", link, err)
		}
	}
	if repoCalls(out) != sent {
		t.Fatal("a link off its hosts was sent")
	}
}

// Without the capability repo.pr_status is never asked.
func TestRepositoryProviderPRStatusNeedsTheCapability(t *testing.T) {
	h, _ := extensionTestHub(t)
	out := installRepositoryProvider(t, h, h.routes())
	sent := repoCalls(out)
	_, err := h.RepositoryPRStatus(context.Background(), acmeProvider, "https://code.acme.test/team/ok/-/merge_requests/7")
	if pe := providerErr(t, err); pe.Code != "failed" || !strings.Contains(pe.Why, "does not read pull request states") || repoCalls(out) != sent {
		t.Fatalf("asked without the capability: %+v", pe)
	}
}
