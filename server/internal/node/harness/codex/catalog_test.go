package codex

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCodexVersionWords(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		out, version string
		reads        bool
	}{
		{"codex-cli 0.167.0\n", "0.167.0", true},
		{"codex-cli 0.161.0", "0.161.0", true},
		{"codex-cli 1.0.0", "1.0.0", true},
		{"codex-cli 0.160.9", "0.160.9", false},
		{"codex-cli 0.161.0-alpha.2", "0.161.0-", false},
		{"codex-cli 0.170.0-alpha.1", "0.170.0-", true},
		{"codex-cli 0.167.0+build.5", "0.167.0", true},
		{"codex-cli", "", false},
		{"", "", false},
		{"codex-cli 0.167", "", false},
		{"codex-cli v0.167.0", "", false},
		{"codex-cli 0.167.0\nnote: a newer version is out\n", "0.167.0", true},
	} {
		v := ParseVersion(c.out)
		if v != c.version || ReadsCatalog(v) != c.reads {
			t.Errorf("%q: version %q reads %v, want %q %v", c.out, v, ReadsCatalog(v), c.version, c.reads)
		}
	}
	if got := CatalogClientVersion("0.170.0-"); got != "0.170.0" {
		t.Errorf("client_version %q", got)
	}
	if strings.Contains(CatalogClientVersion("0.167.0"), "-") {
		t.Error("a release's client_version")
	}
}

// catalogEntry is a Codex model entry with every field Codex requires (codexCatalogFields), and a few it reads.
func catalogEntry(slug, visibility string, levels ...string) map[string]any {
	ls := []map[string]any{}
	for _, l := range levels {
		ls = append(ls, map[string]any{"effort": l, "description": l})
	}
	return map[string]any{"slug": slug, "display_name": slug, "description": "From example-gateway", "visibility": visibility,
		"supported_reasoning_levels": ls, "shell_type": "shell_command", "supported_in_api": true, "priority": 1,
		"support_verbosity": false, "truncation_policy": map[string]any{"mode": "tokens", "limit": 10000},
		"experimental_supported_tools": []string{}, "context_window": 1000000}
}

func catalogJSON(entries ...map[string]any) string {
	b, _ := json.Marshal(map[string]any{"models": entries})
	return string(b)
}

// codexCatalogBody is a Codex model catalog as a gateway serves it: a listed model with levels, one that takes none,
// and a hidden one.
var codexCatalogBody = catalogJSON(catalogEntry("example-big", "list", "low", "high"), catalogEntry("example-plain", "list"),
	catalogEntry("example-hidden", "hide", "low"))

// openAIModelsBody is OpenAI's model list, which Codex cannot read as a catalog.
const openAIModelsBody = `{"object":"list","data":[{"id":"example-big","object":"model"}]}`

// A catalog is one Codex can read: every entry with every field Codex requires, of its kind, nested ones too. One that
// lacks any (Codex then fails the whole list, lists nothing and runs models on fallback metadata) is none.
func TestIsCodexCatalogNeedsWhatCodexNeeds(t *testing.T) {
	t.Parallel()
	if !IsModelCatalog([]byte(codexCatalogBody)) {
		t.Fatal("a whole catalog is none")
	}
	without := func(field string) map[string]any {
		e := catalogEntry("a", "list", "low")
		delete(e, field)
		return e
	}
	with := func(field string, v any) map[string]any {
		e := catalogEntry("a", "list", "low")
		e[field] = v
		return e
	}
	cases := map[string]string{
		"empty":         `{"models":[]}`,
		"not JSON":      `{"models":[`,
		"OpenAI's list": openAIModelsBody,
		"the four fields the first check read": catalogJSON(map[string]any{"slug": "a", "display_name": "A", "visibility": "list",
			"supported_reasoning_levels": []any{}}),
		"duplicate slug":         catalogJSON(catalogEntry("a", "list"), catalogEntry("a", "list")),
		"empty slug":             catalogJSON(catalogEntry(" ", "list")),
		"slug a number":          catalogJSON(with("slug", 7)),
		"nothing listed":         catalogJSON(catalogEntry("a", "hide")),
		"priority a string":      catalogJSON(with("priority", "1")),
		"priority a fraction":    catalogJSON(with("priority", 1.5)),
		"limit in exponent form": catalogJSON(with("truncation_policy", map[string]any{"mode": "tokens", "limit": json.RawMessage("1e4")})),
		"levels null":            catalogJSON(with("supported_reasoning_levels", nil)),
		"a level no description": catalogJSON(with("supported_reasoning_levels", []any{map[string]any{"effort": "low"}})),
		"a level a word":         catalogJSON(with("supported_reasoning_levels", []any{"low"})),
		"truncation no limit":    catalogJSON(with("truncation_policy", map[string]any{"mode": "tokens"})),
		"tools null":             catalogJSON(with("experimental_supported_tools", nil)),
		"no slug":                catalogJSON(without("slug")),
	}
	for field := range codexCatalogFields() {
		cases["no "+field] = catalogJSON(without(field))
	}
	for name, body := range cases {
		if IsModelCatalog([]byte(body)) {
			t.Errorf("%s: taken for a catalog", name)
		}
	}
	// What Codex reads with a default may be missing.
	e := catalogEntry("a", "list")
	delete(e, "description")
	delete(e, "context_window")
	if !IsModelCatalog([]byte(catalogJSON(e))) {
		t.Error("an entry without the optional fields is none")
	}
}
