package hubcli

import (
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/clitext"
)

// emptyHome is a HOME without hub.json.
func emptyHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PERSONA_HOME", "")
	t.Setenv("PERSONA_LISTEN", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	return home
}

// `hub pair`'s argument mistakes are usage mistakes (exit 2, no log timestamp), found before hub.json is read or made.
func TestPairArgumentMistakes(t *testing.T) {
	home := emptyHome(t)
	for _, args := range [][]string{
		{"x"},
		{"--peer", "x"},
		{"--bogus"},
		{"--peer", "--machine", "m1"},
		{"--peer", "--legacy"},
		{"--peer", "--add"},
		{"--peer", "--name", "n"},
		{"--peer", "--name="},
		{"--machine="},
		{"--machine", "m1", "--legacy"},
		{"--add"},
		{"--name", "n"},
		{"--machine", "m1", "--name", "n"},
	} {
		err := pair(context.Background(), args)
		var ue clitext.UsageError
		if !errors.As(err, &ue) {
			t.Errorf("pair %q: %v, want a usage mistake", args, err)
		} else if !strings.Contains(string(ue), "usage: ") {
			t.Errorf("pair %q: %q, want the usage with it", args, ue)
		}
	}
	if err := pair(context.Background(), []string{"-h"}); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("pair -h: %v, want flag.ErrHelp (exit 0)", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".persona", "hub.json")); !os.IsNotExist(err) {
		t.Errorf("a usage mistake touched hub.json: %v", err)
	}
}
