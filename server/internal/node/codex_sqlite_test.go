package node

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func sqliteTestName() string {
	if runtime.GOOS == "windows" {
		return "sqlite3.exe"
	}
	return "sqlite3"
}

func sqliteFallbackForTest(t *testing.T, dirs ...string) {
	t.Helper()
	old := codexSQLiteFallbackDirs
	codexSQLiteFallbackDirs = func() []string { return dirs }
	t.Cleanup(func() { codexSQLiteFallbackDirs = old })
}

func TestCodexSQLiteRespectsPathBeforeFallback(t *testing.T) {
	preferred, fallback := t.TempDir(), t.TempDir()
	for _, dir := range []string{preferred, fallback} {
		if err := os.WriteFile(filepath.Join(dir, sqliteTestName()), nil, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", preferred)
	sqliteFallbackForTest(t, fallback)
	path, err := codexSQLiteProgram()
	if err != nil || path != filepath.Join(preferred, sqliteTestName()) {
		t.Fatalf("SQLite = %q, %v; want PATH's executable", path, err)
	}
}

func TestCodexSQLiteFallbackDoesNotSearchRelativeDirectories(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.WriteFile(sqliteTestName(), nil, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	sqliteFallbackForTest(t, "", ".", "not-an-absolute-folder")
	if path, err := codexSQLiteProgram(); path != "" || !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("SQLite = %q, %v; a working-directory executable must not be used", path, err)
	}
}
