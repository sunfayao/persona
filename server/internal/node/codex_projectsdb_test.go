package node

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const nativeProjectSchema = `CREATE TABLE projects (id TEXT PRIMARY KEY, name TEXT NOT NULL, position INTEGER);
CREATE TABLE project_roots (project_id TEXT NOT NULL, position INTEGER NOT NULL, path TEXT NOT NULL);`

func projectState(t *testing.T, home string, state any) string {
	t.Helper()
	b, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".codex-global-state.json")
	if err := os.WriteFile(p, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func projectSQL(t *testing.T, home, sql string) string {
	t.Helper()
	p := filepath.Join(home, "state_5.sqlite")
	cmd := exec.Command(sqlite3WithJSON(t), p)
	// Read SQL as UTF-8 bytes; Windows sqlite builds may decode command-line SQL with the active code page.
	cmd.Stdin = strings.NewReader(sql)
	hideCodexSQLiteWindow(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v: %s", err, out)
	}
	return p
}

func expireProjectCatalog(home string) {
	codexProjectCatalogs.Lock()
	c := codexProjectCatalogs.byHome[home]
	codexProjectCatalogs.Unlock()
	if c != nil {
		c.Lock()
		c.at = time.Time{}
		c.Unlock()
	}
}

func TestCodexDesktopProjectsLegacyAndEmptySources(t *testing.T) {
	home := t.TempDir()
	p, order, err := codexDesktopProjects(home)
	if err != nil || p != nil || order != nil {
		t.Fatalf("absent source = %v, %v, %v", p, order, err)
	}
	projectState(t, home, map[string]any{"local-projects": map[string]any{}})
	p, order, err = codexDesktopProjects(home)
	if err != nil || p == nil || len(p) != 0 || len(order) != 0 {
		t.Fatalf("empty source = %v, %v, %v", p, order, err)
	}
	projectState(t, home, map[string]any{
		"local-projects": map[string]any{"z": codexAppProject{Name: "Last"}, "a": codexAppProject{Name: "First"}},
		"project-order":  []string{"z", "z", "gone"},
	})
	p, order, err = codexDesktopProjects(home)
	if err != nil || len(p) != 2 || !reflect.DeepEqual(order, []string{"z", "a"}) {
		t.Fatalf("legacy projects = %v, %v, %v", p, order, err)
	}
}

func TestCodexDesktopProjectsNativeWithoutLegacyState(t *testing.T) {
	home := t.TempDir()
	projectSQL(t, home, nativeProjectSchema+`INSERT INTO projects VALUES ('empty', 'Empty project', 0);`)
	p, order, err := codexDesktopProjects(home)
	if err != nil || len(p) != 1 || p["empty"].Name != "Empty project" || !reflect.DeepEqual(order, []string{"empty"}) {
		t.Fatalf("native-only projects = %v, %v, %v", p, order, err)
	}
}

func TestCodexDesktopProjectsOldSchemaKeepsLegacy(t *testing.T) {
	home := t.TempDir()
	projectState(t, home, map[string]any{"local-projects": map[string]any{"legacy": codexAppProject{Name: "Still here"}}})
	projectSQL(t, home, `CREATE TABLE threads (id TEXT);`)
	p, order, err := codexDesktopProjects(home)
	if err != nil || len(p) != 1 || p["legacy"].Name != "Still here" || !reflect.DeepEqual(order, []string{"legacy"}) {
		t.Fatalf("old schema = %v, %v, %v", p, order, err)
	}
}

func TestCodexDesktopProjectsFailedRefreshKeepsLastCatalog(t *testing.T) {
	home := t.TempDir()
	projectSQL(t, home, nativeProjectSchema+`INSERT INTO projects VALUES ('native', 'Keep me', 0);`)
	if _, _, err := codexDesktopProjects(home); err != nil {
		t.Fatal(err)
	}
	projectSQL(t, home, `ALTER TABLE projects RENAME COLUMN name TO other_name;`)
	expireProjectCatalog(home)
	p, order, err := codexDesktopProjects(home)
	if err == nil || p["native"].Name != "Keep me" || !reflect.DeepEqual(order, []string{"native"}) {
		t.Fatalf("failed refresh = %v, %v, %v", p, order, err)
	}
	projectSQL(t, home, `ALTER TABLE projects RENAME COLUMN other_name TO name; UPDATE projects SET name = 'Recovered';`)
	expireProjectCatalog(home)
	p, _, err = codexDesktopProjects(home)
	if err != nil || p["native"].Name != "Recovered" {
		t.Fatalf("recovered refresh = %v, %v", p, err)
	}
}

func TestCodexDesktopProjectsCacheAndCorruptState(t *testing.T) {
	home := t.TempDir()
	state := projectState(t, home, map[string]any{"local-projects": map[string]any{"legacy": codexAppProject{Name: "Keep me"}}})
	p, _, err := codexDesktopProjects(home)
	if err != nil || p["legacy"].Name != "Keep me" {
		t.Fatal(p, err)
	}
	codexProjectCatalogs.Lock()
	c := codexProjectCatalogs.byHome[home]
	codexProjectCatalogs.Unlock()
	c.Lock()
	before := c.at
	c.Unlock()
	codexDesktopProjects(home)
	c.Lock()
	after := c.at
	c.Unlock()
	if !before.Equal(after) {
		t.Fatal("unchanged catalog read again within refresh window")
	}
	if err := os.WriteFile(state, []byte(`{"local-projects":`), 0o600); err != nil {
		t.Fatal(err)
	}
	p, _, err = codexDesktopProjects(home)
	if err == nil || p["legacy"].Name != "Keep me" {
		t.Fatalf("corrupt state discarded last catalog: %v, %v", p, err)
	}
}

const nativeProjectThreadSchema = `CREATE TABLE threads (id TEXT PRIMARY KEY, project_id TEXT, archived INTEGER NOT NULL DEFAULT 0);`

func TestCodexDesktopProjectSnapshotOldThreadSchemas(t *testing.T) {
	for _, tc := range []struct {
		name, schema string
	}{
		{name: "no threads table"},
		{name: "no project column", schema: `CREATE TABLE threads (id TEXT PRIMARY KEY, archived INTEGER); INSERT INTO threads VALUES ('old', 0);`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			projectSQL(t, home, nativeProjectSchema+tc.schema+`INSERT INTO projects VALUES ('native', 'Still available', 0);`)
			snapshot, err := codexDesktopProjectSnapshot(home)
			if err != nil || snapshot.Projects["native"].Name != "Still available" || len(snapshot.ThreadProjects) != 0 {
				t.Fatalf("old assignment schema = %+v, %v", snapshot, err)
			}
		})
	}
}

func TestCodexDesktopProjectSnapshotRefreshNativeAssignments(t *testing.T) {
	home := t.TempDir()
	projectSQL(t, home, nativeProjectSchema+nativeProjectThreadSchema+
		`INSERT INTO projects VALUES ('first', 'First', 0), ('second', 'Second', 1); INSERT INTO threads VALUES ('thread', 'first', 0);`)
	before, err := codexDesktopProjectSnapshot(home)
	if err != nil || before.ThreadProjects["thread"] != "first" {
		t.Fatalf("initial assignment = %+v, %v", before, err)
	}
	projectSQL(t, home, `UPDATE threads SET project_id = 'second';`)
	after, err := codexDesktopProjectSnapshot(home)
	if err != nil || after.ThreadProjects["thread"] != "second" || after.Stamp == before.Stamp {
		t.Fatalf("database update did not refresh snapshot = %+v, %v", after, err)
	}
	if before.ThreadProjects["thread"] != "first" {
		t.Fatal("refresh mutated the previous snapshot")
	}
	projectSQL(t, home, `UPDATE threads SET archived = 1;`)
	archived, err := codexDesktopProjectSnapshot(home)
	if err != nil || len(archived.ThreadProjects) != 0 || archived.Stamp == after.Stamp {
		t.Fatalf("archive did not refresh assignments = %+v, %v", archived, err)
	}
}

func TestCodexProjectSnapshotReusesSuccessfulFingerprintAndRetriesFailure(t *testing.T) {
	var c codexProjectCatalog
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	attempts := 0
	var readErr error
	incomplete := false
	read := func() (codexProjectSnapshot, error) {
		attempts++
		return codexProjectSnapshot{retry: incomplete}, readErr
	}
	c.refresh("state;db;wal-1", now, read)
	if snap, err := c.refresh("state;db;wal-1", now.Add(time.Hour), read); err != nil || snap.Revision != 1 || attempts != 1 {
		t.Fatalf("unchanged source was reread: revision=%d attempts=%d err=%v", snap.Revision, attempts, err)
	}
	readErr = os.ErrPermission
	if snap, err := c.refresh("state;db;wal-2", now.Add(time.Hour), read); err != readErr || snap.Revision != 1 || attempts != 2 {
		t.Fatalf("WAL change did not attempt a refresh: revision=%d attempts=%d err=%v", snap.Revision, attempts, err)
	}
	if _, err := c.refresh("state;db;wal-2", now.Add(time.Hour+time.Second), read); err != readErr || attempts != 2 {
		t.Fatalf("a failed source did not back off: attempts=%d err=%v", attempts, err)
	}
	readErr, incomplete = nil, true
	if snap, err := c.refresh("state;db;wal-2", now.Add(2*time.Hour), read); err != nil || snap.Revision != 2 || attempts != 3 {
		t.Fatalf("legacy fallback was unavailable: revision=%d attempts=%d err=%v", snap.Revision, attempts, err)
	}
	incomplete = false
	if snap, err := c.refresh("state;db;wal-2", now.Add(3*time.Hour), read); err != nil || snap.Revision != 3 || attempts != 4 {
		t.Fatalf("incomplete read was not retried: revision=%d attempts=%d err=%v", snap.Revision, attempts, err)
	}
}

func BenchmarkCodexProjectSnapshotUnchanged(b *testing.B) {
	var c codexProjectCatalog
	now := time.Now()
	c.refresh("state;db;wal", now, func() (codexProjectSnapshot, error) {
		return codexProjectSnapshot{}, nil
	})
	read := func() (codexProjectSnapshot, error) {
		b.Fatal("unchanged snapshot reached the SQL reader")
		return codexProjectSnapshot{}, nil
	}
	for b.Loop() {
		c.refresh("state;db;wal", now.Add(time.Hour), read)
	}
}
