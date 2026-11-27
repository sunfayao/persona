package hub

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The migrations are a hub set and a manager set: a hub's database has only the hub's
// tables, and the default manager is to get a database of its own. So what the hub asks of its store must only touch
// the hub's tables, and what the managers ask of theirs only the manager's. These read every interface a side
// declares, take the methods *db.DB serves of them, and look at the SQL those methods (and the db functions they
// call) run.

var createTableRE = regexp.MustCompile(`(?i)CREATE TABLE (?:IF NOT EXISTS )?([a-z_][a-z0-9_]*)`)

// extractCreateTables returns sorted, unique lowercase table names declared by CREATE TABLE statements in sql.
func extractCreateTables(sql string) []string {
	var out []string
	for _, m := range createTableRE.FindAllStringSubmatch(sql, -1) {
		if tb := strings.ToLower(m[1]); !slices.Contains(out, tb) {
			out = append(out, tb)
		}
	}
	sort.Strings(out)
	return out
}

// setTables are the tables migrations/<part> makes.
func setTables(t *testing.T, part string) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "db", "migrations", part, "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("migrations/%s: %v", part, err)
	}
	var out []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, tb := range extractCreateTables(string(b)) {
			if !slices.Contains(out, tb) {
				out = append(out, tb)
			}
		}
	}
	sort.Strings(out)
	return out
}

// dbFuncs are internal/db's functions and *DB's methods by name, each with the string literals in its body, the
// names it calls and the names it reads; and the package's constants as "const." and their name, each with the
// string literals of its value and the names it reads. So SQL kept in a constant (insertMetricSQL) counts for the
// functions that use it.
type dbFunc struct {
	strs  []string
	calls []string
	names []string
}

// tablesTouched are the tables of all the SQL method m of *DB runs, the db functions it calls and the constants they
// read included.
func tablesTouched(funcs map[string]dbFunc, m string, all []string) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(key string)
	walk = func(key string) {
		// The side's own settings table, by how the database was opened; and the copy of the hub's keys kept for a hub
		// build from before hub_settings on a database shared with the default manager (db.go keepForOlderHub).
		if seen[key] || key == "DB.settingsTable" || key == "DB.keepForOlderHub" || key == "DB.sharedWithOlderHub" {
			return
		}
		seen[key] = true
		fn, ok := funcs[key]
		if !ok {
			return
		}
		for _, s := range fn.strs {
			for _, tb := range all {
				if regexp.MustCompile(`\b`+tb+`\b`).MatchString(s) && !slices.Contains(out, tb) {
					out = append(out, tb)
				}
			}
		}
		for _, c := range fn.calls {
			walk("DB." + c)
			walk(c)
		}
		for _, n := range fn.names {
			walk("const." + n)
		}
	}
	walk("DB." + m)
	sort.Strings(out)
	return out
}

// Regression test for #12846 / #12833: table names with digits (such as a2a_tasks) must be parsed intact rather than
// truncated at the first digit into single-letter names that cause false positives on SQL aliases or English words.
func TestCreateTableREParsesDigitTableNames(t *testing.T) {
	sql := `
		CREATE TABLE a2a_tasks (id text PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS v2_items_3 (id int);
		create table OAuth2_Tokens (id text);
		CREATE TABLE a2a_tasks (id text);
	`
	got := extractCreateTables(sql)
	want := []string{"a2a_tasks", "oauth2_tokens", "v2_items_3"}
	if !slices.Equal(got, want) {
		t.Fatalf("extractCreateTables() = %v, want %v", got, want)
	}

	managerTables := setTables(t, "manager")
	if !slices.Contains(managerTables, "a2a_tasks") {
		t.Fatalf("manager tables %v do not contain a2a_tasks", managerTables)
	}
	if slices.Contains(managerTables, "a") {
		t.Fatalf("manager tables %v contain truncated single-letter table name \"a\"", managerTables)
	}

	funcs := map[string]dbFunc{
		"DB.HubQueryWithAliasA": {
			strs: []string{
				"SELECT 1 FROM share_machine_authorities a WHERE a.id = $1",
				"phone write arguments must be a JSON object of at most 64 KiB",
			},
		},
		"DB.ManagerQueryA2ATasks": {
			strs: []string{
				"SELECT id FROM a2a_tasks WHERE state = $1",
			},
		},
	}
	all := append([]string{"share_machine_authorities"}, got...)
	if touched := tablesTouched(funcs, "HubQueryWithAliasA", all); !slices.Equal(touched, []string{"share_machine_authorities"}) {
		t.Fatalf("HubQueryWithAliasA touched %v, want [share_machine_authorities]", touched)
	}
	if touched := tablesTouched(funcs, "ManagerQueryA2ATasks", all); !slices.Equal(touched, []string{"a2a_tasks"}) {
		t.Fatalf("ManagerQueryA2ATasks touched %v, want [a2a_tasks]", touched)
	}
}
