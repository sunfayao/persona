package knowledge

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnowledgeSearchRanksOnlyGrantedRedactedDocuments(t *testing.T) {
	t.Parallel()
	const visible = "knowledge/entities/runbooks/release.md"
	const hidden = "knowledge/entities/runbooks/other.md"
	scope := Scope{Machine: "node-a", Space: "work", Project: "rocket"}
	cfg := Config{Grants: []Grant{
		{Machine: scope.Machine, Space: &scope.Space, Project: scope.Project, Documents: []string{visible}},
		{Machine: "node-b", Space: &scope.Space, Project: scope.Project, Documents: []string{hidden}},
	}}
	local := filepath.ToSlash(filepath.Join(t.TempDir(), "store", "guide.md"))
	d := New(Dependencies{
		Authorize: func(context.Context, Caller) (Scope, bool) { return scope, true },
		Load:      func() (Config, error) { return cfg, nil },
		Read: func(_ context.Context, _, doc string) (string, error) {
			if doc != visible {
				t.Fatalf("reader received ungranted document %q", doc)
			}
			return "Release process starts here. directory=" + local + "\npassword=mock-value", nil
		},
	})
	ctx := context.Background()
	caller := Caller{Machine: scope.Machine, Session: "session-a"}
	for _, req := range []Request{
		{Action: "knowledge_search", Query: "release process"},
		{Action: "knowledge_read", Path: visible},
	} {
		got, err := d.Run(ctx, caller, req)
		if err != nil || !strings.Contains(got, visible) || !strings.Contains(got, "Release process") ||
			strings.Contains(got, local) || strings.Contains(got, "mock-value") || strings.Contains(got, hidden) {
			t.Fatalf("%s returned %q, %v", req.Action, got, err)
		}
	}
	if got, err := d.Run(ctx, caller, Request{Action: "knowledge_read", Path: hidden}); !errors.Is(err, ErrDenied) || got != "" {
		t.Fatalf("cross-scope read: %q, %v", got, err)
	}
	if got, err := d.Run(ctx, caller, Request{Action: "knowledge_read", Path: "knowledge/entities/../outside.md"}); !errors.Is(err, errInvalid) || got != "" {
		t.Fatalf("traversal: %q, %v", got, err)
	}
}

func TestKnowledgeRevocationDuringReadDiscardsTheResult(t *testing.T) {
	t.Parallel()
	doc := "knowledge/entities/runbooks/release.md"
	scope := Scope{Machine: "node-a", Project: "rocket"}
	enabled := true
	d := New(Dependencies{
		Authorize: func(context.Context, Caller) (Scope, bool) { return scope, true },
		Load: func() (Config, error) {
			if !enabled {
				return Config{}, os.ErrNotExist
			}
			return Config{Grants: []Grant{{Machine: scope.Machine, Space: &scope.Space, Project: scope.Project, Documents: []string{doc}}}}, nil
		},
		Read: func(context.Context, string, string) (string, error) {
			enabled = false
			return "a result from a revoked grant", nil
		},
	})
	got, err := d.Run(context.Background(), Caller{Machine: "node-a", Session: "a"}, Request{Action: "knowledge_read", Path: doc})
	if got != "" || !errors.Is(err, ErrDenied) {
		t.Fatalf("revoked result: %q, %v", got, err)
	}
}

func TestKnowledgeReaderErrorsNeverReachTheCaller(t *testing.T) {
	t.Parallel()
	doc := "knowledge/entities/runbooks/release.md"
	scope := Scope{Machine: "node-a", Project: "rocket"}
	d := New(Dependencies{
		Authorize: func(context.Context, Caller) (Scope, bool) { return scope, true },
		Load: func() (Config, error) {
			return Config{Grants: []Grant{{Machine: scope.Machine, Space: &scope.Space, Project: scope.Project, Documents: []string{doc}}}}, nil
		},
		Read: func(context.Context, string, string) (string, error) {
			return "", errors.New("internal CLI error")
		},
	})
	got, err := d.Run(context.Background(), Caller{Machine: "node-a", Session: "a"}, Request{Action: "knowledge_read", Path: doc})
	if got != "" || !errors.Is(err, errUnavailable) {
		t.Fatalf("reader error: %q, %v", got, err)
	}
}
