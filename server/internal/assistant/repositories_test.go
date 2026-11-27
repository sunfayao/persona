package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/assistant/repositorystore"
	"github.com/sunfayao/persona/server/internal/db"
)

func repositoryService(t *testing.T, store Store) (*Service, *int) {
	t.Helper()
	if database, ok := store.(*db.DB); ok {
		store = repositorystore.New(database)
	}
	s := New(Options{Hub: newFakeHub(), Store: func() Store { return store }})
	calls := new(int)
	s.repositoryCheck = func(context.Context, repository.Identity) error { *calls++; return nil }
	return s, calls
}

const repoBase = "/api/v1/assistant/repositories"
const repoExample = `{"url":"https://gitlab.example.com/example/subgroup/project.git","provider":"gitlab","name":"Example","aliases":["app"],"purpose":"Demo project"}`

func TestRepositorySettingsIsolationAndVerificationGate(t *testing.T) {
	st := ruleStore{&memStore{}}
	s, calls := repositoryService(t, st)
	other, otherCalls := repositoryService(t, ruleStore{&memStore{}})
	other.id = "work"
	w := placeDo(s, "", "POST", repoBase, repoExample)
	var saved struct{ Repository RepositoryBinding }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Repository.ID == "" || saved.Repository.VerifiedAt.IsZero() {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	if *calls != 1 {
		t.Fatal("creation checks once")
	}
	if w := placeDo(s, "", "POST", repoBase, strings.ReplaceAll(repoExample, "https://gitlab.example.com/example/subgroup/project.git", "git@gitlab.example.com:example/subgroup/project.git")); w.Code != 409 || *calls != 1 {
		t.Fatalf("duplicate is refused without recheck: %d %s, calls %d", w.Code, w.Body, *calls)
	}
	if w := placeDo(other, "", "POST", repoBase, repoExample); w.Code != 200 || *otherCalls != 1 {
		t.Fatalf("independent binding: %d %s", w.Code, w.Body)
	}

	if w := placeDo(s, "read", "POST", repoBase, repoExample); w.Code != 403 {
		t.Errorf("read-only create: %d", w.Code)
	}
	for _, bad := range []string{
		`{"url":"https://token@code.example.com/example/project"}`,
		`{"url":"https://github.com/example/project","verified_at":"2026-01-01T00:00:00Z"}`,
		`{"url":"https://github.com/example/project","manager_id":"other"}`,
		repoExample + `{}`,
	} {
		if w := placeDo(s, "", "POST", repoBase, bad); w.Code != 400 {
			t.Errorf("invalid: %d %s", w.Code, w.Body)
		}
	}
}

func TestRepositoryStorageFailuresStayFailures(t *testing.T) {
	s, calls := repositoryService(t, nil)
	if w := placeDo(s, "", "POST", repoBase, repoExample); w.Code != 503 || *calls != 0 {
		t.Fatalf("no store: %d calls %d", w.Code, *calls)
	}
	s, _ = repositoryService(t, repositoryFailStore{Store: ruleStore{&memStore{}}})
	if w := placeDo(s, "", "POST", repoBase, repoExample); w.Code != 503 {
		t.Fatalf("read failure: %d %s", w.Code, w.Body)
	}
}

type repositoryFailStore struct{ Store }

func (repositoryFailStore) Setting(context.Context, string, any) (bool, error) {
	return false, errors.New("synthetic private store detail")
}

type repositoryCASStore struct {
	Store
	saved bool
	err   error
}

func (s repositoryCASStore) CompareSetting(context.Context, string, int64, any) (bool, error) {
	return s.saved, s.err
}

func TestRepositoryWriteFailures(t *testing.T) {
	for _, tc := range []struct {
		saved  bool
		err    error
		status int
	}{
		{false, nil, 409}, {false, errors.New("synthetic private write detail"), 503},
	} {
		s, _ := repositoryService(t, repositoryCASStore{ruleStore{&memStore{}}, tc.saved, tc.err})
		w := placeDo(s, "", "POST", repoBase, repoExample)
		if w.Code != tc.status || strings.Contains(w.Body.String(), "private") {
			t.Fatalf("failed write: %d %s", w.Code, w.Body)
		}
		if w := placeDo(s, "", "GET", repoBase, ""); strings.Contains(w.Body.String(), "repo_") {
			t.Fatal("failed write became visible", w.Body)
		}
	}
}

func TestRepositoryFailedChecksNeverPersist(t *testing.T) {
	for _, code := range []string{"timeout", "canceled", "network", "rate_limited", "unsafe_target"} {
		t.Run(code, func(t *testing.T) {
			s, _ := repositoryService(t, ruleStore{&memStore{}})
			s.repositoryCheck = func(context.Context, repository.Identity) error { return &repository.CheckError{Code: code} }
			if w := placeDo(s, "", "POST", repoBase, repoExample); w.Code != 422 {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			doc, err := s.repositoryDocument(context.Background())
			if err != nil || len(doc.Repositories) != 0 || doc.Revision != 0 {
				t.Fatalf("failed check changed storage: %+v %v", doc, err)
			}
		})
	}
}

func TestRepositoryTaskWorkflowPersistenceValidationAndTools(t *testing.T) {
	s, calls := repositoryService(t, ruleStore{&memStore{}})

	w := placeDo(s, "", "POST", repoBase, strings.TrimSuffix(repoExample, "}")+`,"task_workflow":"shared_branch"}`)
	var saved struct{ Repository RepositoryBinding }
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &saved) != nil || saved.Repository.TaskWorkflow != "shared_branch" || *calls != 1 {
		t.Fatalf("save: %d %s", w.Code, w.Body)
	}
	id := saved.Repository.ID

	// Surfaced in repository_get and repositories_list.
	out, err := s.toolRepositories(context.Background(), "repository_get", json.RawMessage(`{"id":"`+id+`"}`))
	if err != nil || !strings.Contains(out, `"task_workflow":"shared_branch"`) {
		t.Fatalf("repository_get: %s %v", out, err)
	}
	out, err = s.toolRepositories(context.Background(), "repositories_list", json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, `"task_workflow":"shared_branch"`) {
		t.Fatalf("repositories_list: %s %v", out, err)
	}

	if w := placeDo(s, "", "POST", repoBase, `{"url":"https://github.com/example/other","task_workflow":"bogus"}`); w.Code != 400 {
		t.Fatalf("invalid create task_workflow: %d %s", w.Code, w.Body)
	}
}
