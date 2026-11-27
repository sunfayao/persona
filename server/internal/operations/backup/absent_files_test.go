package backup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/secretkey"
)

// pendingKeyName is a secret.key.next-* file as a key rotation names it.
func pendingKeyName(t *testing.T, key []byte) string {
	t.Helper()
	return "secret.key.next-" + secretkey.Fingerprint(key)
}

// previousFilesHolding lists the entries of the previous-files directories of
// home that keep name (as NNN-<name>).
func previousFilesHolding(t *testing.T, home, name string) []string {
	t.Helper()
	var found []string
	roots, _ := filepath.Glob(filepath.Join(home, ".persona-backup-previous-*"))
	for _, root := range roots {
		entries, _ := os.ReadDir(root)
		for _, entry := range entries {
			if _, kept, ok := strings.Cut(entry.Name(), "-"); ok && kept == name {
				found = append(found, filepath.Join(root, entry.Name()))
			}
		}
	}
	return found
}

func assertFile(t *testing.T, name string, want []byte) {
	t.Helper()
	raw, err := os.ReadFile(name)
	if err != nil || !bytes.Equal(raw, want) {
		errorf(t, "%s = %q, %v; want %q", name, raw, err, want)
	}
}

// A failed restore leaves what the bundle lacks untouched as well, and removes
// the previous-files directory once everything is back.
func TestFailedRestoreLeavesWhatTheBundleLacksAndNoPreviousFiles(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	pending := bytes.Repeat([]byte{7}, secretkey.Size)
	writeFixtureFile(t, filepath.Join(fixture.home, "link.json"), []byte("link"))
	writeFixtureFile(t, filepath.Join(fixture.home, pendingKeyName(t, pending)), pending)
	fixture.mutate(t)
	fixture.backend.failInstallPart = "manager"
	if _, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true}); err == nil {
		fatal(t, "restore succeeded")
	}
	assertFile(t, filepath.Join(fixture.home, "link.json"), []byte("link"))
	assertFile(t, filepath.Join(fixture.home, pendingKeyName(t, pending)), pending)
	if got := readNote(t, fixture); got != "changed knowledge" {
		errorf(t, "files were not rolled back: %q", got)
	}
	if left, _ := filepath.Glob(filepath.Join(fixture.home, ".persona-backup-previous-*")); len(left) != 0 {
		errorf(t, "previous files remain after the rollback: %v", left)
	}
}

// The built-in manager's own pending key goes with its directory: it belongs to
// the previous manager database (kept as persona_previous_m_*), and a restored
// manager would delete a key file its database does not name at its start
// (keyRotation.removeOtherNexts). So it is in the named previous files.
func TestRestoreKeepsThePreviousManagersPendingKeyWithItsDirectory(t *testing.T) {
	fixture := newBackupFixture(t, "persona_manager")
	operator := fixture.operator()
	if _, err := operator.Create(context.Background(), fixture.options()); err != nil {
		fatal(t, err)
	}
	pending := bytes.Repeat([]byte{6}, secretkey.Size)
	writeFixtureFile(t, filepath.Join(fixture.home, "managers", "assistant", pendingKeyName(t, pending)), pending)
	fixture.mutate(t)
	result, err := operator.Restore(context.Background(), Options{Home: fixture.home, Bundle: fixture.output, WritesQuiesced: true})
	if err != nil {
		fatal(t, err)
	}
	if _, err := os.Stat(filepath.Join(fixture.home, "managers", "assistant", pendingKeyName(t, pending))); !errors.Is(err, os.ErrNotExist) {
		errorf(t, "the previous manager's pending key is beside the restored manager: %v", err)
	}
	dirs := previousFilesHolding(t, fixture.home, "assistant")
	if len(dirs) != 1 || !strings.HasPrefix(dirs[0], result.PreviousFiles) {
		fatalf(t, "previous manager directory %v not under the named previous files %q", dirs, result.PreviousFiles)
	}
	assertFile(t, filepath.Join(dirs[0], pendingKeyName(t, pending)), pending)
}
