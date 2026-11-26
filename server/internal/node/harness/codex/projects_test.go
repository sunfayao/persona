package codex

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func projectReply(result any, value any) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, result)
}

func TestCodexDesktopProjectsListPages(t *testing.T) {
	t.Parallel()
	calls := 0
	projects, err := ListDesktopProjects(func(method string, params any, result any) error {
		calls++
		if method != "project/list" {
			t.Fatalf("unexpected method %q", method)
		}
		p := params.(map[string]any)
		if p["limit"] != 100 || p["sortKey"] != "position" {
			t.Fatalf("params = %#v", p)
		}
		cursor := p["cursor"].(*string)
		switch calls {
		case 1:
			if cursor != nil {
				t.Fatal("the first page has a cursor")
			}
			return projectReply(result, map[string]any{"data": []any{map[string]any{"id": "first", "name": "Empty", "roots": []any{}}}, "nextCursor": "next"})
		case 2:
			if cursor == nil || *cursor != "next" {
				t.Fatalf("cursor = %v", cursor)
			}
			return projectReply(result, map[string]any{"data": []any{map[string]any{"id": "first"}, map[string]any{"id": "second", "name": "Project"}}, "nextCursor": nil})
		default:
			t.Fatalf("unexpected page %d", calls)
			return nil
		}
	})
	if err != nil || len(projects) != 2 || projects[0].Name != "Empty" || projects[1].ID != "second" {
		t.Fatalf("projects = %+v, %v", projects, err)
	}
}

func TestCodexDesktopProjectsListRejectsBrokenPagination(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"repeated cursor", "empty cursor", "missing project id", "server error"} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			_, err := ListDesktopProjects(func(_ string, _ any, result any) error {
				calls++
				switch name {
				case "server error":
					return errors.New("project/list unsupported")
				case "missing project id":
					return projectReply(result, map[string]any{"data": []any{map[string]string{"name": "Invalid"}}})
				case "empty cursor":
					return projectReply(result, map[string]any{"data": []any{}, "nextCursor": ""})
				default:
					return projectReply(result, map[string]any{"data": []any{}, "nextCursor": "same"})
				}
			})
			if err == nil || calls > 2 {
				t.Fatalf("calls = %d, error = %v", calls, err)
			}
		})
	}
}

func TestCodexDesktopProjectForDirectory(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	root, nested := filepath.Join(base, "app"), filepath.Join(base, "app", "module")
	projects := []DesktopProject{
		{ID: "broad", Roots: []DesktopProjectRoot{{Path: root}}, Position: 1},
		{ID: "nested-later", Roots: []DesktopProjectRoot{{Path: nested}}, Position: 2},
		{ID: "nested-first", Roots: []DesktopProjectRoot{{Path: nested}}, Position: 0},
		{ID: "invalid", Roots: []DesktopProjectRoot{{Path: "relative"}}},
	}
	for _, tc := range []struct{ dir, want string }{
		{root, "broad"},
		{filepath.Join(nested, "src"), "nested-first"},
		{filepath.Join(base, "app-other"), ""},
		{"relative", ""},
	} {
		if got := desktopProjectIDForDir(projects, tc.dir); got != tc.want {
			t.Errorf("project for %q = %q, want %q", tc.dir, got, tc.want)
		}
	}
	if runtime.GOOS == "windows" {
		if got := desktopProjectIDForDir(projects, strings.ToUpper(filepath.Join(nested, "src"))); got != "nested-first" {
			t.Fatalf("Windows case-insensitive project = %q", got)
		}
	}
}

func TestSelectCodexDesktopProjectRespectsExactSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	projects := []DesktopProject{
		{ID: "first", Roots: []DesktopProjectRoot{{Path: root}}, Position: 0},
		{ID: "second", Roots: []DesktopProjectRoot{{Path: root}}, Position: 1},
	}
	if got, err := SelectDesktopProject(projects, root, ""); err != nil || got != "first" {
		t.Fatalf("automatic selection = %q, %v", got, err)
	}
	if got, err := SelectDesktopProject(projects, filepath.Join(root, "src"), "second"); err != nil || got != "second" {
		t.Fatalf("explicit selection = %q, %v", got, err)
	}
	for _, tc := range []struct{ cwd, id string }{
		{root, "missing"},
		{root + "-other", "second"},
		{"relative", "second"},
	} {
		if got, err := SelectDesktopProject(projects, tc.cwd, tc.id); err == nil || got != "" {
			t.Errorf("invalid selection %+v = %q, %v", tc, got, err)
		}
	}
}
