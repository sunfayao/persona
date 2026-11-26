package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/apierr"
	"github.com/sunfayao/persona/server/internal/assistant/repository"
	"github.com/sunfayao/persona/server/internal/i18n"
)

const repositoriesKey = "assistant.repositories"
const repositoriesMax = 200

type RepositoryBinding struct {
	repository.Identity
	ID       string   `json:"id"`
	Name     string   `json:"name,omitempty"`
	Aliases  []string `json:"aliases,omitempty"`
	Purpose  string   `json:"purpose,omitempty"`
	CloneURL string   `json:"clone_url,omitempty"`
	// ProviderName is an extension provider's name when the reference was saved (absent for GitHub and GitLab).
	ProviderName string `json:"provider_name,omitempty"`
	// TaskWorkflow is an optional per-repository default task-collaboration cadence ("isolated" or "shared_branch").
	TaskWorkflow string    `json:"task_workflow,omitempty"`
	VerifiedAt   time.Time `json:"verified_at"`
}

type repositoryRemoved struct {
	Binding RepositoryBinding `json:"binding"`
	Receipt string            `json:"receipt"`
	At      time.Time         `json:"at"`
}

type repositoryDocument struct {
	Revision     int64               `json:"revision"`
	Repositories []RepositoryBinding `json:"repositories"`
	Removed      []repositoryRemoved `json:"removed,omitempty"`
}

func (doc repositoryDocument) taskCatalog() repository.Catalog {
	out := repository.Catalog{Revision: doc.Revision, Repositories: make([]repository.CatalogEntry, 0, len(doc.Repositories))}
	for _, entry := range doc.Repositories {
		out.Repositories = append(out.Repositories, repository.CatalogEntry{Identity: entry.Identity, ID: entry.ID, CloneURL: entry.CloneURL,
			ProviderName: entry.ProviderName, TaskWorkflow: entry.TaskWorkflow})
	}
	return out
}

type repositoryInput struct {
	URL          string   `json:"url"`
	Provider     string   `json:"provider"`
	Name         string   `json:"name"`
	Aliases      []string `json:"aliases"`
	Purpose      string   `json:"purpose"`
	CloneURL     *string  `json:"clone_url,omitempty"`
	TaskWorkflow string   `json:"task_workflow,omitempty"`
}

type repositoryProblem string

func (e repositoryProblem) Error() string { return string(e) }

func repositoryText(value string, max int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsFunc(value, func(r rune) bool {
		return unicode.IsControl(r) || unicode.In(r, unicode.Cf)
	})
}

// binding is in for the built-in providers, offline (Service.repositoryBinding routes an extension provider's address).
func (in repositoryInput) binding() (RepositoryBinding, error) {
	id, err := repository.Parse(in.URL, in.Provider)
	if err != nil {
		return RepositoryBinding{}, repositoryProblem("remote")
	}
	out := RepositoryBinding{Identity: id}
	if in.CloneURL != nil {
		transport, err := repository.CloneTransport(*in.CloneURL, id)
		if err != nil {
			return out, repositoryProblem("remote")
		}
		out.CloneURL = transport
	}
	return in.details(out)
}

// details are in's name, purpose and aliases on out, checked.
func (in repositoryInput) details(out RepositoryBinding) (RepositoryBinding, error) {
	out.Name, out.Purpose = strings.TrimSpace(in.Name), strings.TrimSpace(in.Purpose)
	if !repositoryText(out.Name, 80) || !repositoryText(out.Purpose, 500) || len(in.Aliases) > 10 {
		return out, repositoryProblem("metadata")
	}
	for _, alias := range in.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" || !repositoryText(alias, 80) {
			return out, repositoryProblem("metadata")
		}
		if !slices.ContainsFunc(out.Aliases, func(v string) bool { return strings.EqualFold(v, alias) }) {
			out.Aliases = append(out.Aliases, alias)
		}
	}
	wf, wfErr := repository.NormalizeTaskWorkflow(in.TaskWorkflow)
	if wfErr != "" {
		return out, repositoryProblem("metadata")
	}
	out.TaskWorkflow = wf
	return out, nil
}

func (s *Service) repositoryDocument(ctx context.Context) (repositoryDocument, error) {
	doc := repositoryDocument{Repositories: []RepositoryBinding{}}
	st := s.store()
	if st == nil {
		return doc, repositoryProblem("store")
	}
	if _, err := st.Setting(ctx, repositoriesKey, &doc); err != nil {
		return doc, repositoryProblem("store")
	}
	if doc.Repositories == nil {
		doc.Repositories = []RepositoryBinding{}
	}
	return doc, nil
}

// changeRepositories serializes local writers and uses the existing Settings CAS
// to refuse concurrent changes from another process. Failed reads never erase data.
func (s *Service) changeRepositories(ctx context.Context, change func(*repositoryDocument) error) error {
	s.repositoriesMu.Lock()
	defer s.repositoriesMu.Unlock()
	doc, err := s.repositoryDocument(ctx)
	if err != nil {
		return err
	}
	st, ok := s.store().(standingCASStore)
	if !ok {
		return repositoryProblem("store")
	}
	expected := doc.Revision
	if err := change(&doc); err != nil {
		return err
	}
	doc.Revision++
	saved, err := st.CompareSetting(ctx, repositoriesKey, expected, doc)
	if err != nil {
		return repositoryProblem("store")
	}
	if !saved {
		return repositoryProblem("conflict")
	}
	return nil
}

func (s *Service) checkRepository(ctx context.Context, id repository.Identity) error {
	if repository.IsExtensionProvider(id.Provider) {
		return s.checkProviderRepository(ctx, id)
	}
	if s.repositoryCheck != nil {
		return s.repositoryCheck(ctx, id)
	}
	cfg, err := s.readRepositoryConfig()
	if err != nil {
		return repositoryProblem("store")
	}
	return cfg.checker().Check(ctx, id)
}

func (s *Service) saveRepository(ctx context.Context, binding RepositoryBinding) (RepositoryBinding, error) {
	err := s.changeRepositories(ctx, func(doc *repositoryDocument) error {
		binding.Identity = s.savedIdentity(*doc, binding.Identity)
		if slices.ContainsFunc(doc.Repositories, func(b RepositoryBinding) bool { return b.Identity == binding.Identity }) {
			return repositoryProblem("duplicate")
		}
		if len(doc.Repositories) >= repositoriesMax {
			return repositoryProblem("full")
		}
		if err := s.checkRepository(ctx, binding.Identity); err != nil {
			return err
		}
		binding.ID = "repo_" + randomHex(16)
		binding.VerifiedAt = time.Now().UTC()
		doc.Repositories = append(doc.Repositories, binding)
		return nil
	})
	return binding, err
}

func (s *Service) repositoryByID(ctx context.Context, id string) (RepositoryBinding, error) {
	doc, err := s.repositoryDocument(ctx)
	if err != nil {
		return RepositoryBinding{}, err
	}
	for _, binding := range doc.Repositories {
		if binding.ID == id {
			return binding, nil
		}
	}
	return RepositoryBinding{}, repositoryProblem("not_found")
}

func repositoryError(w http.ResponseWriter, err error) {
	code, status := "store", http.StatusServiceUnavailable
	var problem repositoryProblem
	var check *repository.CheckError
	var provider *repository.ProviderError
	switch {
	case errors.As(err, &problem):
		code = string(problem)
		status = map[string]int{"store": 503, "remote": 400, "metadata": 400, "duplicate": 409, "conflict": 409, "not_found": 404, "full": 409, "readonly": 403}[code]
	case errors.As(err, &check):
		code, status = check.Code, http.StatusUnprocessableEntity
	case errors.As(err, &provider):
		code, status = providerCode(provider), http.StatusUnprocessableEntity
		if code == "no_provider" || code == "provider_off" || code == providersStartingCode {
			status = http.StatusConflict
		}
		apierr.WriteCode(w, status, "repository_"+code, i18n.Err("assistant-repositories."+code, providerArgs(provider)))
		return
	}
	if status == 0 { // a code without a status is never written as one
		code, status = "store", http.StatusServiceUnavailable
	}
	apierr.WriteCode(w, status, "repository_"+code, i18n.Err("assistant-repositories."+code))
}

// Strict, bounded input excludes client-supplied verification, credentials, IDs,
// scope selectors and trailing JSON at every mutation/preview boundary.
func repositoryBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if !fullRole(r) {
		repositoryError(w, repositoryProblem("readonly"))
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		repositoryError(w, repositoryProblem("metadata"))
		return false
	}
	if err := d.Decode(new(any)); err != io.EOF {
		repositoryError(w, repositoryProblem("metadata"))
		return false
	}
	return true
}

func (s *Service) repositoryRoutes() {
	s.handle("GET /api/v1/assistant/repositories", func(w http.ResponseWriter, r *http.Request) {
		doc, err := s.repositoryDocument(r.Context())
		if err != nil {
			repositoryError(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"manager": s.managerIdentity(), "repositories": doc.Repositories, "catalog_revision": doc.Revision})
	})
	save := func(w http.ResponseWriter, r *http.Request) {
		var in repositoryInput
		if !repositoryBody(w, r, &in) {
			return
		}
		ctx := withProviderBudget(r.Context()) // the provider's parse and check share one budget
		binding, err := s.repositoryBinding(ctx, in)
		if err == nil {
			binding, err = s.saveRepository(ctx, binding)
		}
		if err != nil {
			repositoryError(w, err)
			return
		}
		apierr.WriteJSON(w, map[string]any{"manager": s.managerIdentity(), "repository": binding})
	}
	s.handle("POST /api/v1/assistant/repositories", save)
}
