package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeEmpty(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadFileRefusesICloud(t *testing.T) {
	n := &Node{home: t.TempDir()}
	if _, err := n.fileService().Read("/Users/x/Library/Mobile Documents/com~apple~CloudDocs/a", "", 0, false); !errors.Is(err, errICloud) {
		t.Fatalf("iCloud path should be refused: %v", err)
	}
}
