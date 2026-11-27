package nodeupdate

import (
	"context"
	"os"
	"testing"
)

// A resident that recorded its executable's identity at startup still refuses
// to commit over a file renamed into place meanwhile (an in-place rewrite is
// the hub's call to supersede: the commit re-hashes what is there).
func TestCommitRefusesWhenExecutableIdentityChanged(t *testing.T) {
	m, data, _ := testManager(t)
	st, err := os.Stat(m.exe)
	if err != nil {
		t.Fatal(err)
	}
	m.identity = st
	id := receive(t, m, data, spec(data, "next"))
	replaceExternally(t, m.exe, []byte("independent deployment"))
	if _, err := m.Commit(context.Background(), id, nil); err == nil {
		t.Fatal("commit installed over a replaced executable")
	}
	if got, _ := os.ReadFile(m.exe); string(got) != "independent deployment" {
		t.Fatalf("commit overwrote an unstaged file: %q", got)
	}
	if _, err := os.Stat(markerPath(m.exe)); !os.IsNotExist(err) {
		t.Fatal("refused commit left a pending marker")
	}
}
