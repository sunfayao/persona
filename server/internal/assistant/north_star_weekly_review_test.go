package assistant

import (
	"os"
	"path/filepath"
	"testing"
)

func setNorthStarWeeklyOwnConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PERSONA_HOME", home)
	dir := filepath.Join(home, "managers", "assistant")
	mustDo(t, os.MkdirAll(dir, 0o700))
	mustDo(t, os.WriteFile(filepath.Join(dir, "manager.json"), []byte(body), 0o600))
}
