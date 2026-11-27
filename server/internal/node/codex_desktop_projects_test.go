package node

import (
	"os"
	"testing"
)

func TestCodexAppStateFindsNativeProjectsWhenSQLiteBecomesAvailable(t *testing.T) {
	home := t.TempDir()
	projectState(t, home, map[string]any{
		"local-projects": map[string]codexAppProject{"legacy": {Name: "Old name"}},
		"app-server-project-id-by-legacy-project-id-by-host": map[string]any{"local:" + home: map[string]string{"legacy": "native"}},
	})
	projectSQL(t, home, nativeProjectSchema+`INSERT INTO projects VALUES ('native', 'Current name', 0);`)
	previousDirs, previousPath := codexSQLiteFallbackDirs, os.Getenv("PATH")
	codexSQLiteFallbackDirs = func() []string { return nil }
	t.Cleanup(func() { codexSQLiteFallbackDirs = previousDirs })
	t.Setenv("PATH", t.TempDir())
	if first := loadCodexAppState(home); first == nil || first.projects["legacy"].Name != "Old name" {
		t.Fatalf("legacy state without SQLite = %+v", first)
	}
	codexSQLiteFallbackDirs = previousDirs
	t.Setenv("PATH", previousPath)
	expireProjectCatalog(home)
	if recovered := loadCodexAppState(home); recovered == nil || recovered.projects["legacy"].Name != "Current name" {
		t.Fatalf("reader recovery with unchanged files = %+v", recovered)
	}
}

func TestCodexAppStateClearsOnlyMigratedNativeUnassignments(t *testing.T) {
	home := t.TempDir()
	projectState(t, home, map[string]any{
		"local-projects": map[string]codexAppProject{"legacy": {Name: "Saved project"}},
		"thread-project-assignments": map[string]any{
			appChat: map[string]string{"projectId": "legacy"},
			appOld:  map[string]string{"projectId": "legacy"},
		},
		"app-server-project-id-by-legacy-project-id-by-host": map[string]any{"local:" + home: map[string]string{"legacy": "native"}},
		"app-server-projects-migration-by-host": map[string]any{"local:" + home: map[string]any{
			"threadAssignmentsMigrated": true, "threadAssignmentsReadMigrated": true, "pendingThreadAssignmentIds": []string{appOld},
		}},
	})
	projectSQL(t, home, nativeProjectSchema+
		`INSERT INTO projects VALUES ('native', 'Saved project', 0);`+
		`CREATE TABLE threads (id TEXT PRIMARY KEY, project_id TEXT, archived INTEGER);`+
		`INSERT INTO threads VALUES ('`+appChat+`', NULL, 0), ('`+appOld+`', NULL, 0), ('`+appExec+`', NULL, 0);`)
	state := loadCodexAppState(home)
	if state == nil || state.assigned[appChat] != "" || !state.inNoProject(appChat) ||
		state.assigned[appOld] != "legacy" || state.inNoProject(appOld) || state.listed(appExec) {
		t.Fatalf("migration-aware unassignment = %+v", state)
	}
}
