package assistant

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/db"
)

// startingAcme is Acme Code right after a restart, as the extensions' manager answers then: no provider is listed
// until it has started.
type startingAcme struct {
	*acmeProvider
	started chan struct{}
}

func (p startingAcme) up() bool {
	select {
	case <-p.started:
		return true
	default:
		return false
	}
}

func (p startingAcme) RepositoryProviders() []repository.ProviderInfo {
	if !p.up() {
		return nil
	}
	return p.acmeProvider.RepositoryProviders()
}

func (p startingAcme) RepositoryProvider(id string) (repository.ProviderInfo, bool) {
	if !p.up() {
		return repository.ProviderInfo{}, false
	}
	return p.acmeProvider.RepositoryProvider(id)
}

func (p startingAcme) RepositoryProviderForHost(host string) (repository.ProviderInfo, bool) {
	if !p.up() {
		return repository.ProviderInfo{}, false
	}
	return p.acmeProvider.RepositoryProviderForHost(host)
}

// providersRestarting makes s's providers those of a manager that has not started yet; the channel starts it.
func providersRestarting(s *Service, f *acmeProvider) chan struct{} {
	started := make(chan struct{})
	s.repoExt, s.repoStarted = startingAcme{f, started}, started
	return started
}

// shorten sets *v to short until the test ends.
func shorten[T any](t *testing.T, v *T, short T) {
	t.Helper()
	old := *v
	*v = short
	t.Cleanup(func() { *v = old })
}

// Right after a restart the catalog waits, within its budget, for the providers; still not listed, it says they are
// starting (409), never that the provider is missing or the address is no repository's.
func TestRepositoryCatalogWaitsForTheProviders(t *testing.T) {
	s, f, _ := acmeService(t, ruleStore{&memStore{}})
	f.asked()
	for _, req := range []struct{ method, path, body string }{
		{"POST", repoBase, `{"url":"https://code.acme.test/team/web","provider":"` + acmeProviderID + `"}`},
		{"POST", repoBase, `{"url":"https://code.acme.test/team/api"}`},
	} {
		started := providersRestarting(s, f)
		time.AfterFunc(50*time.Millisecond, func() { close(started) })
		if w := placeDo(s, "", req.method, req.path, req.body); w.Code != 200 {
			t.Fatalf("%s %s: %d %s", req.method, req.body, w.Code, w.Body)
		}
	}
	shorten(t, &providerBudget, 100*time.Millisecond)
	providersRestarting(s, f)
	f.asked()
	for _, req := range []struct{ method, path, body string }{
		{"POST", repoBase, `{"url":"https://code.acme.test/team/new"}`},
		{"POST", repoBase, `{"url":"https://code.acme.test/team/new","provider":"` + acmeProviderID + `"}`},
	} {
		w := placeDo(s, "", req.method, req.path, req.body)
		if w.Code != 409 || !strings.Contains(w.Body.String(), `"repository_providers_starting"`) || !strings.Contains(w.Body.String(), "扩展还在启动中") {
			t.Errorf("%s %s: %d %s", req.method, req.body, w.Code, w.Body)
		}
	}
	if _, err := s.toolRepositoryAdd(t.Context(), "server", "c1", json.RawMessage(`{"url":"https://code.acme.test/team/new"}`)); err == nil ||
		!strings.HasPrefix(err.Error(), "repository_providers_starting: ") {
		t.Fatal("tool:", err)
	}
	if w := placeDo(s, "", "POST", repoBase, `{"url":"https://github.com/example/web"}`); w.Code != 200 {
		t.Fatalf("built-in while starting: %d %s", w.Code, w.Body)
	}
	if got := f.asked(); len(got) != 0 {
		t.Fatal("asked while starting", got)
	}
}

// stuckAcme is Acme Code getting no answer about an address containing stuck: the call waits out its time and fails
// the way the extensions' manager says a call out of time; a call already out of time fails the same way.
type stuckAcme struct {
	*acmeProvider
	stuck string
}

func (p *stuckAcme) RepositoryParse(ctx context.Context, provider, remote string) (repository.Parsed, error) {
	if p.stuck != "" && strings.Contains(remote, p.stuck) {
		<-ctx.Done()
	}
	if ctx.Err() != nil {
		p.call("parse-timeout")
		return repository.Parsed{}, &repository.ProviderError{Code: "failed", Provider: "Acme Code", Why: "no answer in time"}
	}
	return p.acmeProvider.RepositoryParse(ctx, provider, remote)
}

// The links of one reply each get a provider budget of their own, and only a provider's answer stops a later reply
// from asking again: a call out of time, or the providers still starting, does not.
func TestReportedLinksEachGetTheirOwnBudget(t *testing.T) {
	database, err := db.Open(t.Context(), newDatabase(t), db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	s, f, _ := acmeService(t, database)
	p := &stuckAcme{acmeProvider: f, stuck: "/reviews/1"}
	s.repoExt = p
	shorten(t, &providerBudget, 200*time.Millisecond)
	shorten(t, &reportedLinkStart, 50*time.Millisecond)
	todo, err := s.createPlainTodo(t.Context(), "Reported work", "", db.TodoEdit{})
	if err != nil {
		t.Fatal(err)
	}
	const app = "https://code.acme.test/team/app"
	links := func() []string {
		t.Helper()
		var out []string
		for _, row := range todoRepositoriesOf(t, s, todo.ID).Repositories {
			for _, l := range row.Links {
				out = append(out, l.CanonicalURL)
			}
		}
		slices.Sort(out)
		return out
	}
	reply := func(text string) {
		t.Helper()
		s.advanceTaskReply(t.Context(), todoNow(s, todo.ID), "", "", text)
		s.reportedLinks.runs.Wait()
	}
	reply("Opened " + app + "/reviews/1 and " + app + "/reviews/2")
	if got := links(); !slices.Equal(got, []string{app + "/reviews/2"}) {
		t.Fatal("a slow first link left the second no time:", got, f.asked())
	}
	p.stuck = ""
	f.asked()
	reply("Still open: " + app + "/reviews/1 and " + app + "/reviews/2")
	if got := f.asked(); !slices.Equal(got, []string{"parse", "link", "describe"}) {
		t.Fatal("asked again", got) // reviews/1 got no answer before; reviews/2 was answered
	}
	if got := links(); !slices.Equal(got, []string{app + "/reviews/1", app + "/reviews/2"}) {
		t.Fatal(got)
	}
	// Still starting: nothing is asked, and the next reply after the start asks.
	started := make(chan struct{})
	s.repoStarted = started
	reply("Opened " + app + "/reviews/3")
	if got := f.asked(); len(got) != 0 {
		t.Fatal("asked while starting", got)
	}
	close(started)
	reply("Opened " + app + "/reviews/3")
	if got := links(); len(got) != 3 || !slices.Contains(got, app+"/reviews/3") {
		t.Fatal("after the start:", got, f.asked())
	}
}
