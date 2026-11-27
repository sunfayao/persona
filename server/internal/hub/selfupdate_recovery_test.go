package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/update"
)

func TestDevelopmentUnknownRecoveryIsRetained(t *testing.T) {
	for _, journal := range []string{"unready", "{", "{}"} {
		t.Run(journal, func(t *testing.T) {
			h := newTestHub(t)
			u := h.upd
			p := update.Pending{FromBuild: h.build.Number, ToBuild: 2, Target: filepath.Join(t.TempDir(), "personad"),
				Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
			if err := os.WriteFile(p.Target, []byte("untouched"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
				t.Fatal(err)
			}
			if journal != "unready" {
				if err := os.WriteFile(update.DevelopmentAdoptionPath(u.dir), []byte(journal), 0600); err != nil {
					t.Fatal(err)
				}
			}
			staged := u.stagedDir(p.ToBuild)
			if err := os.MkdirAll(staged, 0700); err != nil {
				t.Fatal(err)
			}
			if bootUpdater(t, u) {
				t.Fatal("unknown adoption replayed")
			}
			u.pruneDownloads(0)
			if got, _ := os.ReadFile(p.Target); string(got) != "untouched" {
				t.Fatal("unknown preimage restored")
			}
			if !u.adoptionUnresolved() {
				t.Fatal("unknown journal cleared")
			}
			if _, err := os.Stat(staged); err != nil {
				t.Fatal("unknown staging removed", err)
			}
		})
	}
}

func TestDevelopmentFailedRestoreRetainsRecovery(t *testing.T) {
	h := newTestHub(t)
	u := h.upd
	target := filepath.Join(t.TempDir(), "personad")
	p := update.Pending{FromBuild: 1, ToBuild: 2, Target: target, Prev: target + ".prev",
		Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
	if err := os.WriteFile(target, []byte("new program"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := update.Save(update.PendingPath(u.dir), p); err != nil {
		t.Fatal(err)
	}
	if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
		t.Fatal(err)
	}
	staged := u.stagedDir(p.ToBuild)
	if err := os.MkdirAll(staged, 0700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(staged, "saved-node")
	if err := os.WriteFile(artifact, []byte("verified staged package"), 0600); err != nil {
		t.Fatal(err)
	}
	u.exec = func(string) error { t.Fatal("executed after failed restore"); return nil }
	u.rollback(p, "fixture: missing preimage")
	// A refused retry of the same build also must not delete retained staging.
	u.installCandidate(context.Background(), false, &update.Published{
		Manifest: &update.Manifest{Build: p.ToBuild}, Development: p.Development})
	// A normal startup must not discard material retained by a failed restore.
	u.pruneDownloads(0)
	for _, path := range []string{update.PendingPath(u.dir), update.DevelopmentAdoptionPath(u.dir), artifact} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("lost recovery file %s: %v", filepath.Base(path), err)
		}
	}
	if _, err := os.Stat(update.RolledBackPath(u.dir)); !os.IsNotExist(err) {
		t.Fatal("reported rollback before programs were restored", err)
	}
}

func TestDevelopmentCrashRecoveryFailureBlocksAttemptedBuild(t *testing.T) {
	for _, failure := range []string{"preimage", "exec"} {
		t.Run(failure, func(t *testing.T) {
			h := newTestHub(t)
			h.build.Number = 2
			u := h.upd
			target := filepath.Join(t.TempDir(), "personad")
			p := update.Pending{FromBuild: 1, ToBuild: 2, Target: target, Prev: target + ".prev", RecoveryReady: true,
				Development: &update.DevelopmentCandidate{Commit: strings.Repeat("a", 40)}}
			if err := os.WriteFile(target, []byte("attempted program"), 0700); err != nil {
				t.Fatal(err)
			}
			if failure == "exec" {
				if err := os.WriteFile(p.Prev, []byte("previous program"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := update.BeginDevelopmentAdoption(u.dir, p); err != nil {
				t.Fatal(err)
			}
			staged := u.stagedDir(2)
			if err := os.MkdirAll(staged, 0700); err != nil {
				t.Fatal(err)
			}
			executed := false
			u.exec = func(string) error { executed = true; return fmt.Errorf("fixture exec failed") }
			if pending, err := u.boot(); pending || err == nil {
				t.Fatal("failed crash recovery allowed attempted build startup", pending, err)
			}
			if executed != (failure == "exec") {
				t.Fatal("wrong restoration/exec boundary", executed)
			}
			u.pruneDownloads(0)
			if !u.adoptionUnresolved() {
				t.Fatal("failed recovery discarded adoption journal")
			}
			if _, err := os.Stat(staged); err != nil {
				t.Fatal("failed recovery discarded staging", err)
			}
			if err := os.WriteFile(p.Prev, []byte("previous program"), 0700); err != nil {
				t.Fatal(err)
			}
			u.exec = func(string) error { return nil }
			if !bootUpdater(t, u) || u.adoptionUnresolved() {
				t.Fatal("repaired recovery could not finish")
			}
			if _, err := os.Stat(staged); !os.IsNotExist(err) {
				t.Fatal("successful recovery kept obsolete staging", err)
			}
		})
	}
}

// Most startup fixtures expect recovery to complete without an error.
func bootUpdater(t *testing.T, u *updater) bool {
	t.Helper()
	pending, err := u.boot()
	if err != nil {
		t.Fatal(err)
	}
	return pending
}
