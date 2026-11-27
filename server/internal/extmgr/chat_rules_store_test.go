package extmgr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/connector"
	"github.com/sunfayao/persona/server/internal/db"
)

// settingsOnly hides all but the settings of a store: a manager on it has no ledger, so a test against the real database
// does not write rows into it.
type settingsOnly struct{ Store }

// onRuleSettings runs f once with the settings in memory and, when PERSONA_TEST_DATABASE_URL is set
// (scripts/test/server_test_env.py), once on Postgres with the manager's database: the rules' setting cleared before and
// after. manager makes a new manager on that store, as a restart does.
func onRuleSettings(t *testing.T, f func(t *testing.T, store Store, manager func() *Manager)) {
	run := func(t *testing.T, store Store) {
		f(t, store, func() *Manager {
			h := newTestManager(t)
			h.store = store
			return h
		})
	}
	t.Run("memory", func(t *testing.T) { run(t, settingsOnly{newKeepStore()}) })
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("PERSONA_TEST_DATABASE_URL")
		if url == "" {
			t.Skip("PERSONA_TEST_DATABASE_URL not set (use scripts/test/server_test_env.py)")
		}
		database, err := db.Open(context.Background(), url, db.ManagerSet)
		if err != nil {
			t.Fatal(err)
		}
		clear := func() {
			var gone json.RawMessage
			if _, err := database.TakeSetting(context.Background(), chatRulesKey, &gone); err != nil {
				t.Error(err)
			}
		}
		clear()
		t.Cleanup(func() {
			clear()
			database.Close()
		})
		run(t, settingsOnly{database})
	})
}

func ghUserRule(n int) connector.UserRule {
	rule, err := connector.ChatRuleFor("github", "github_comment", json.RawMessage(fmt.Sprintf(`{"ref":"example-org/example-repo#%d"}`, n)))
	if err != nil {
		panic(err)
	}
	return rule
}

func larkUserRule(conv string) connector.UserRule {
	rule, err := connector.ChatRuleFor("lark", "POST /conversations/"+conv+"/messages", json.RawMessage(`{"conversation":"`+conv+`"}`))
	if err != nil {
		panic(err)
	}
	return rule
}

func idsOf(list []connector.UserRule) []string {
	var out []string
	for _, u := range list {
		out = append(out, u.ID())
	}
	return out
}

// A slip in a setting someone edited by hand costs only the element that is no rule: it is left out one by one and said in
// the log, the good rules around it are read and kept.
func TestChatRuleStoreLeavesOutOnlyTheElementsThatAreNoRule(t *testing.T) {
	ctx := context.Background()
	good12, _ := json.Marshal(ghUserRule(12))
	lark1, _ := json.Marshal(larkUserRule("oc_1"))
	g12, l1 := string(good12), string(lark1)
	for name, c := range map[string]struct {
		setting []string
		bad     int // how many elements are no rule
		want    []string
	}{
		"a good rule and a number":              {[]string{g12, `5`}, 1, []string{"github:example-org/example-repo#12"}},
		"a number and a good rule":              {[]string{`5`, g12}, 1, []string{"github:example-org/example-repo#12"}},
		"a rule that is not an object between":  {[]string{g12, `{"rule": 5}`, l1}, 1, []string{"github:example-org/example-repo#12", "lark:oc_1"}},
		"a time that is a number, and a string": {[]string{`{"rule": {"connector": "github"}, "at": 12}`, g12, `"x"`}, 2, []string{"github:example-org/example-repo#12"}},
	} {
		onRuleSettings(t, func(t *testing.T, store Store, manager func() *Manager) {
			var logged []string
			h := manager()
			h.opts.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
			var elements []json.RawMessage
			for _, e := range c.setting {
				elements = append(elements, json.RawMessage(e))
			}
			if err := store.PutSetting(ctx, chatRulesKey, elements); err != nil {
				t.Fatal(err)
			}
			list, err := h.readUserRules(ctx)
			if err != nil || !slices.Equal(idsOf(list), c.want) {
				t.Fatalf("%s: read as %v, %v; want %v", name, idsOf(list), err, c.want)
			}
			if n := countLines(logged, "is not a rule"); n != c.bad {
				t.Errorf("%s: %d elements said in the log, want %d: %v", name, n, c.bad, logged)
			}
			// At start the good rules are in force.
			h.startConnectorGate(ctx)
			if got := h.connectors.UserRules(); len(got) != len(c.want) {
				t.Errorf("%s: the gate has %+v", name, got)
			}
		})
	}
}

func countLines(lines []string, part string) (n int) {
	for _, l := range lines {
		if strings.Contains(l, part) {
			n++
		}
	}
	return n
}

// unreadableStore is a store whose reads can fail, and that counts the writes it was asked for.
