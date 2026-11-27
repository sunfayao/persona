package link

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A rename left from long ago is not taken.
func TestAStaleRenameIsDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "link.json")
	b, _ := json.Marshal(renameRequest{ID: "old", Name: "x", At: time.Now().Add(-time.Hour)})
	if err := os.WriteFile(RenamePath(path), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := takeRename(path, time.Now()); ok {
		t.Fatal("a stale rename was taken")
	}
	if _, err := os.Stat(RenamePath(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a stale rename stays")
	}
}
