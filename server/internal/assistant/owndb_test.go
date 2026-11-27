package assistant

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/db/dbtest"
	"github.com/sunfayao/persona/server/internal/managerconf"
)

func TestDatabaseMovedAt(t *testing.T) {
	for _, input := range []string{
		"2025-01-02T03:04:05Z",
		"2025-01-02T17:04:05+14:00",
		"2025-01-01T15:04:05-12:00",
	} {
		t.Run(input, func(t *testing.T) {
			moved, err := time.Parse(time.RFC3339, input)
			if err != nil {
				t.Fatal(err)
			}
			if got := databaseMovedAt(moved); got != "2025-01-02T03:04:05Z" {
				t.Fatalf("databaseMovedAt(%q) = %q; want an explicit UTC instant", input, got)
			}
		})
	}
}

// newDatabase is a database of its own next to the test's, dropped at the end.
func newDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv(dbtest.Env)
	if base == "" {
		t.Skip(dbtest.Env + " not set")
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	name := "persona_owndb_t_" + hex.EncodeToString(b)
	url, err := db.WithDatabase(base, name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.EnsureDatabase(context.Background(), url); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if admin, err := pgx.Connect(context.Background(), base); err == nil {
			_, _ = admin.Exec(context.Background(), `DROP DATABASE IF EXISTS "`+name+`" WITH (FORCE)`)
			_ = admin.Close(context.Background())
		}
	})
	return url
}

// Two hubs on one machine never share the manager's container or second database.
func TestOwnDatabaseNames(t *testing.T) {
	t.Setenv("_PERSONA_ASSISTANT_DB_ID", "")
	u, _ := os.UserHomeDir()
	if id := ownContainerID(filepath.Join(u, ".persona")); !strings.HasPrefix(id, "assistant-") {
		t.Errorf("the user's own hub: %s", id)
	}
	a, b := ownContainerID("/srv/hub-a"), ownContainerID("/srv/hub-b")
	if a == b || !strings.HasPrefix(a, "assistant-") {
		t.Errorf("two hubs: %s %s", a, b)
	}
	if n := ownDatabaseName("postgres://u:p@[fd00::20]:5432/jarvis?sslmode=disable"); n != "jarvis_manager" {
		t.Errorf("second database %s", n)
	}
	if n := ownDatabaseName("postgres://u:p@[fd00::20]:5432/" + strings.Repeat("x", 70)); len(n) > 63 {
		t.Errorf("too long: %d", len(n))
	}
}

// A manager.json database_container is used only when it has the persona-mgr-<id>-pg shape a manager makes
// (pgdocker.ManagerContainer).
func TestManagerContainerID(t *testing.T) {
	for _, tc := range []struct {
		name, id string
		ok       bool
	}{
		{"persona-mgr-abc-pg", "abc", true},
		{"persona-mgr-assistant-0a1b2c3d-pg", "assistant-0a1b2c3d", true},
		{"somebody-else-pg", "", false},
		{"persona-mgr--pg", "", false},
		{"persona-mgr-abc", "", false},
		{"persona-mgr-abc-data", "", false},
		{"persona-mgr-pg", "", false},
		{"-pg", "", false},
		{"", "", false},
	} {
		if id, ok := managerContainerID(tc.name); id != tc.id || ok != tc.ok {
			t.Errorf("managerContainerID(%q) = %q, %v; want %q, %v", tc.name, id, ok, tc.id, tc.ok)
		}
	}
}

// A database_container a manager does not make is refused before Docker is asked anything: no container is made or
// started for it. PATH is empty, so a regression finds no docker rather than a real one.
func TestManagerContainerRefusedBeforeDocker(t *testing.T) {
	t.Setenv("PATH", "")
	for _, name := range []string{"somebody-else-pg", "persona-mgr--pg", "persona-mgr-abc"} {
		kept := managerconf.OwnDatabase{Container: name, Location: LocationContainer}
		got, created, err := provisionContainer(context.Background(), t.TempDir(), kept)
		if err == nil || !strings.Contains(err.Error(), "is not one a manager makes") || created || got != kept {
			t.Errorf("%q: %+v %v %v; want it refused and kept as it was", name, got, created, err)
		}
	}
}
