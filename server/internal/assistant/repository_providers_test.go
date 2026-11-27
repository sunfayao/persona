package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistant/repository"
)

const acmeProviderID = "ext:acme-code/acme"

// acmeProvider is a repository provider extension on code.acme.test (ssh.acme.test for git): it answers like the
// extensions' manager does, re-checking its own answers, and records every call.
type acmeProvider struct {
	mu        sync.Mutex
	installed bool
	state     string
	checkErr  error
	calls     []string
	lookups   int
}

func newAcmeProvider() *acmeProvider {
	return &acmeProvider{installed: true, state: repository.ProviderAvailable}
}

func (f *acmeProvider) set(installed bool, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed, f.state = installed, state
}

// asked are the provider's calls since the last time, forgotten.
func (f *acmeProvider) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *acmeProvider) looked() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.lookups
	f.lookups = 0
	return n
}

func (f *acmeProvider) info() repository.ProviderInfo {
	return repository.ProviderInfo{ID: acmeProviderID, Name: "Acme Code", Hosts: []string{"code.acme.test", "ssh.acme.test"}, State: f.state,
		LinkKinds: []repository.LinkLabel{{Kind: "pull_request", Label: "Review"}}}
}

func (f *acmeProvider) RepositoryProviders() []repository.ProviderInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.installed {
		return nil
	}
	archive := repository.ProviderInfo{ID: "ext:acme-code/archive", Name: "Acme Archive", Hosts: []string{"archive.acme.test"}, State: repository.ProviderOff}
	return []repository.ProviderInfo{f.info(), archive}
}

func (f *acmeProvider) RepositoryProvider(id string) (repository.ProviderInfo, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	return f.info(), f.installed && id == acmeProviderID
}

func (f *acmeProvider) RepositoryProviderForHost(host string) (repository.ProviderInfo, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	return f.info(), f.installed && slices.Contains(f.info().Hosts, host)
}

func (f *acmeProvider) call(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name)
}

func (f *acmeProvider) RepositoryParse(_ context.Context, provider, remote string) (repository.Parsed, error) {
	f.call("parse")
	path := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(remote, "https://code.acme.test/"), "git@ssh.acme.test:"), ".git")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return repository.Parsed{}, &repository.ProviderError{Code: "not_found", Provider: "Acme Code"}
	}
	path = parts[0] + "/" + parts[1]
	return repository.CheckParseAnswer(f.info(), repository.ParseAnswer{InstanceURL: "https://code.acme.test", Path: path,
		CanonicalURL: "https://code.acme.test/" + path, CloneURL: "git@ssh.acme.test:" + path + ".git", DisplayName: strings.ToUpper(parts[1][:1]) + parts[1][1:]})
}

func (f *acmeProvider) RepositoryCheck(context.Context, repository.Identity) error {
	f.call("check")
	return f.checkErr
}

func (f *acmeProvider) RepositoryLink(_ context.Context, repo repository.Identity, in repository.LinkInput) (repository.TaskLink, error) {
	f.call("link")
	kind, number := in.Kind, in.Number
	if in.URL != "" {
		rest := strings.Split(strings.TrimPrefix(in.URL, repo.CanonicalURL+"/"), "/")
		if len(rest) < 2 || rest[0] != "reviews" && rest[0] != "issues" {
			return repository.TaskLink{}, &repository.ProviderError{Code: "not_found", Provider: "Acme Code"}
		}
		kind, number = map[string]string{"reviews": "pull_request", "issues": "issue"}[rest[0]], rest[1]
	}
	segment := map[string]string{"pull_request": "reviews", "issue": "issues"}[kind]
	return repository.CheckLinkAnswer(repo, in, repository.LinkAnswer{Kind: kind, Number: number, CanonicalURL: repo.CanonicalURL + "/" + segment + "/" + number})
}

func (f *acmeProvider) RepositoryDescribe(_ context.Context, _, canonicalURL string) (repository.Description, error) {
	f.call("describe")
	return repository.CheckDescribeAnswer("Title of "+canonicalURL[strings.LastIndex(canonicalURL, "/")+1:], "open"), nil
}

// acmeService is repositoryService with the Acme Code provider installed and on.
func acmeService(t *testing.T, store Store) (*Service, *acmeProvider, *int) {
	t.Helper()
	s, checks := repositoryService(t, store)
	f := newAcmeProvider()
	s.repoExt = f
	return s, f, checks
}

func savedRepository(t *testing.T, s *Service, body string) RepositoryBinding {
	t.Helper()
	w := placeDo(s, "", "POST", repoBase, body)
	var saved struct{ Repository RepositoryBinding }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	return saved.Repository
}

func TestRepositoryCatalogThroughAProvider(t *testing.T) {
	s, f, checks := acmeService(t, ruleStore{&memStore{}})
	// The host says which provider it is; the provider's own name for the repository is the default name.
	saved := savedRepository(t, s, `{"url":"https://code.acme.test/team/app","purpose":"Demo"}`)
	if saved.Provider != acmeProviderID || saved.ProviderName != "Acme Code" || saved.Name != "App" || saved.CanonicalURL != "https://code.acme.test/team/app" ||
		saved.CloneURL != "git@ssh.acme.test:team/app.git" || saved.VerifiedAt.IsZero() {
		t.Fatalf("%+v", saved)
	}
	if got := f.asked(); !slices.Equal(got, []string{"parse", "check"}) || *checks != 0 {
		t.Fatal("save asked", got, *checks)
	}
	// An explicit GitLab stays GitLab, offline and checked as before, on a host an extension claims.
	gitlab := savedRepository(t, s, `{"url":"https://code.acme.test/team/web","provider":"gitlab"}`)
	if gitlab.Provider != "gitlab" || gitlab.ProviderName != "" || *checks != 1 || len(f.asked()) != 0 {
		t.Fatalf("explicit gitlab: %+v %d", gitlab, *checks)
	}
	// The provider's check answers map to the catalog's codes.
	for code, want := range map[string]string{"not_found": "repository_provider_not_found", "not_signed_in": "repository_provider_not_signed_in",
		"refused": "repository_provider_refused", "failed": "repository_provider_failed", "unheard_of": "repository_provider_failed"} {
		f.checkErr = &repository.ProviderError{Code: code, Provider: "Acme Code", Why: "the words of " + code}
		w := placeDo(s, "", "POST", repoBase, `{"url":"https://code.acme.test/team/other"}`)
		if w.Code != 422 || !strings.Contains(w.Body.String(), `"`+want+`"`) || !strings.Contains(w.Body.String(), "Acme Code") ||
			(code == "refused" || code == "failed") && !strings.Contains(w.Body.String(), "the words of "+code) {
			t.Errorf("%s: %d %s", code, w.Code, w.Body)
		}
	}
	f.checkErr = nil
	f.asked()
	f.set(true, repository.ProviderOff)
	if w := placeDo(s, "", "POST", repoBase, `{"url":"https://code.acme.test/team/new"}`); w.Code != 409 || !strings.Contains(w.Body.String(), `"repository_provider_off"`) || !strings.Contains(w.Body.String(), "Acme Code") {
		t.Fatalf("new reference, provider off: %d %s", w.Code, w.Body)
	}
	if _, err := s.toolRepositoryAdd(t.Context(), "server", "c1", json.RawMessage(`{"url":"https://code.acme.test/team/new"}`)); err == nil ||
		!strings.HasPrefix(err.Error(), "repository_provider_off: ") || !strings.Contains(err.Error(), "Acme Code") {
		t.Fatal("tool, provider off:", err)
	}
	if got := f.asked(); len(got) != 0 {
		t.Fatal("an off provider was asked", got)
	}
	// Uninstalled: the saved reference stays editable, a new one names the missing provider.
	f.set(false, repository.ProviderOff)
	if w := placeDo(s, "", "POST", repoBase, `{"url":"https://code.acme.test/team/new","provider":"`+acmeProviderID+`"}`); w.Code != 409 || !strings.Contains(w.Body.String(), `"repository_no_provider"`) {
		t.Fatalf("uninstalled: %d %s", w.Code, w.Body)
	}
	if w := placeDo(s, "", "POST", repoBase, `{"url":"https://code.acme.test/team/new"}`); w.Code != 400 || !strings.Contains(w.Body.String(), `"repository_remote"`) {
		t.Fatalf("unclaimed host without a provider: %d %s", w.Code, w.Body)
	}
	if got := f.asked(); len(got) != 0 {
		t.Fatal("an uninstalled provider was asked", got)
	}
}
