package conformance

import (
	"os"
	"path/filepath"
	"testing"
)

// The repository root, from this package's directory.
const repoRoot = "../../../.."

func readRepoFile(t *testing.T, rel string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}


