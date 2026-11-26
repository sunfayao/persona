package files

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// One stat that does not answer leaves its path missing and does not hold up the others.
func TestStatPathsTimeout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.go"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	config := testConfig(dir)
	config.StatTimeout = 100 * time.Millisecond
	stat := config.StatPath
	hang := make(chan struct{})
	defer close(hang)
	config.StatPath = func(path string) (string, os.FileInfo, error) {
		if path == filepath.Join(dir, "stuck") {
			<-hang
		}
		return stat(path)
	}
	start := time.Now()
	got := New(config).Stat([]string{"~/stuck", "~/a.go"}, "")
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %s", took)
	}
	if len(got) != 2 || got[0].Kind != "missing" || got[0].Path != "~/stuck" || got[1].Kind != "file" {
		t.Fatalf("%+v", got)
	}
}
