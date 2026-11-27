package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentWebRecoveryPreservesConflictingOverride(t *testing.T) {
	dir := t.TempDir()
	p := Pending{RecoveryReady: true, Web: filepath.Join(dir, "web"), WebPrev: filepath.Join(dir, "previous")}
	for _, path := range []string{p.Web, p.WebPrev} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "index.html"), []byte(path), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := RestoreAll(p); err == nil {
		t.Fatal("overwrote a changed override")
	}
	for _, path := range []string{p.Web, p.WebPrev} {
		if got, err := os.ReadFile(filepath.Join(path, "index.html")); err != nil || string(got) != path {
			t.Fatal("changed Web recovery contents", err)
		}
	}
}

func TestDevelopmentAdoptionFenceMatchesExactAttempt(t *testing.T) {
	dir := t.TempDir()
	p := Pending{ToBuild: 202611130059, Development: &DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
	if err := BeginDevelopmentAdoption(dir, p); err != nil {
		t.Fatal(err)
	}
	if err := BeginDevelopmentAdoption(dir, p); err == nil {
		t.Fatal("replayed unknown adoption")
	}
	other := p
	other.ToBuild++
	ClearDevelopmentAdoption(dir, other)
	if _, err := os.Stat(DevelopmentAdoptionPath(dir)); err != nil {
		t.Fatal("another attempt cleared the fence")
	}
	ClearDevelopmentAdoption(dir, p)
	if _, err := os.Stat(DevelopmentAdoptionPath(dir)); !os.IsNotExist(err) {
		t.Fatal("matching fence retained", err)
	}
	if got := DevelopmentNumber(p.ToBuild); got <= p.ToBuild {
		t.Fatal("non-monotonic identity", got)
	}
}

func TestDevelopmentBackupConfigCoordinatedRequiresHooksForExtras(t *testing.T) {
	cases := []struct {
		name        string
		cfg         DevelopmentBackupConfig
		coordinated bool
		external    bool
	}{
		{"builtin default", DevelopmentBackupConfig{}, true, false},
		{"external hooks", DevelopmentBackupConfig{Quiesce: []string{"p"}, Resume: []string{"r"}}, true, true},
		{"only quiesce", DevelopmentBackupConfig{Quiesce: []string{"p"}}, false, false},
		{"only resume", DevelopmentBackupConfig{Resume: []string{"r"}}, false, false},
		{"extra database without hooks", DevelopmentBackupConfig{ExtraDatabases: []string{"postgres://localhost/extra"}}, false, false},
		{"extra root without hooks", DevelopmentBackupConfig{ExtraRoots: []string{"/srv/extra"}}, false, false},
		{"extra database with hooks", DevelopmentBackupConfig{Quiesce: []string{"p"}, Resume: []string{"r"}, ExtraDatabases: []string{"postgres://localhost/extra"}}, true, true},
		{"extra root with hooks", DevelopmentBackupConfig{Quiesce: []string{"p"}, Resume: []string{"r"}, ExtraRoots: []string{"/srv/extra"}}, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cfg.Coordinated(); got != tc.coordinated {
				t.Fatalf("Coordinated() = %v, want %v", got, tc.coordinated)
			}
			if got := tc.cfg.ExternalQuiesce(); got != tc.external {
				t.Fatalf("ExternalQuiesce() = %v, want %v", got, tc.external)
			}
		})
	}
}
