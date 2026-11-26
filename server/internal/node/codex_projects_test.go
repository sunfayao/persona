package node

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
)

func TestParseCodexDesktopProjectIDUsesVerifiedNativeAliases(t *testing.T) {
	home := t.TempDir()
	canonicalHome := codex.CanonicalHome(home, "")
	previous := readCodexProjectSelection
	t.Cleanup(func() { readCodexProjectSelection = previous })
	readCodexProjectSelection = func(got string) (codexProjectSnapshot, error) {
		if got != canonicalHome {
			t.Fatalf("read home = %q, want %q", got, canonicalHome)
		}
		return codexProjectSnapshot{
			Projects:         map[string]codexAppProject{"legacy": {}, "native-only": {}, "old-only": {}},
			NativeProjectIDs: map[string]string{"native": "legacy", "native-only": "native-only"},
		}, nil
	}
	for _, tc := range []struct{ source, want string }{
		{"", ""},
		{codexDesktopProjectID(canonicalHome, "legacy"), "native"},
		{codexDesktopProjectID(canonicalHome, "native"), "native"},
		{codexDesktopProjectID(canonicalHome, "native-only"), "native-only"},
	} {
		got, err := parseCodexDesktopProjectID(home, tc.source)
		if err != nil || got != tc.want {
			t.Errorf("parse %q = %q, %v; want %q", tc.source, got, err, tc.want)
		}
	}
	for _, id := range []string{"old-only", "missing", " "} {
		if _, err := parseCodexDesktopProjectID(home, codexDesktopProjectID(canonicalHome, id)); err == nil {
			t.Errorf("accepted unavailable selection %q", id)
		}
	}
	if _, err := parseCodexDesktopProjectID(home, codexDesktopProjectID(filepath.Join(home, "other"), "native")); err == nil {
		t.Fatal("accepted another Codex profile")
	}
	readCodexProjectSelection = func(string) (codexProjectSnapshot, error) {
		return codexProjectSnapshot{}, errors.New("snapshot unavailable")
	}
	if _, err := parseCodexDesktopProjectID(home, codexDesktopProjectID(canonicalHome, "native")); err == nil {
		t.Fatal("accepted unavailable snapshot")
	}
	readCodexProjectSelection = func(string) (codexProjectSnapshot, error) {
		return codexProjectSnapshot{
			Projects:         map[string]codexAppProject{"legacy": {}},
			NativeProjectIDs: map[string]string{"first": "legacy", "second": "legacy"},
		}, nil
	}
	if _, err := parseCodexDesktopProjectID(home, codexDesktopProjectID(canonicalHome, "legacy")); err == nil {
		t.Fatal("accepted ambiguous native mappings")
	}
}
