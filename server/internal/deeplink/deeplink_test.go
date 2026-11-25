package deeplink

import (
	"encoding/json"
	"os"
	"testing"
)

// fixture is spec/fixtures/deep-links/cases.json's sections this package writes (its README says what each holds).
type fixture struct {
	Version int `json:"version"`
	Build   []struct {
		Name string            `json:"name"`
		Page map[string]string `json:"page"`
		Path string            `json:"path"`
	} `json:"build"`
	Links []struct {
		Name string            `json:"name"`
		Base string            `json:"base"`
		Page map[string]string `json:"page"`
		Web  string            `json:"web"`
		App  string            `json:"app"`
	} `json:"links"`
}

func readFixture(t *testing.T) fixture {
	t.Helper()
	raw, err := os.ReadFile("../../../spec/fixtures/deep-links/cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f fixture
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	if f.Version != 1 || len(f.Build) == 0 || len(f.Links) == 0 {
		t.Fatalf("fixture version %d with %d build and %d links cases", f.Version, len(f.Build), len(f.Links))
	}
	return f
}


func TestBuildMatchesSharedCases(t *testing.T) {
	for _, c := range readFixture(t).Build {
		if got := build(c.Page); got != c.Path {
			t.Errorf("%s: %v wrote %q, want %q", c.Name, c.Page, got, c.Path)
		}
	}
}


