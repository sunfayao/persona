package node

import (
	"os"
	"slices"
	"strings"
	"testing"
)

// starts are the lines a fake bridge wrote to path, one per start (and "stopped" when it was stopped).
func starts(path string) []string {
	b, _ := os.ReadFile(path)
	return strings.Fields(string(b))
}

func TestJSRuntimeEnv(t *testing.T) {
	base := []string{"A=1", "ELECTRON_RUN_AS_NODE=1", "B=2"}
	if got := (jsRuntime{Path: "node"}).env(base); !slices.Equal(got, []string{"A=1", "B=2"}) {
		t.Fatalf("node: %q", got)
	}
	if got := (jsRuntime{Path: "SomeApp", Electron: true}).env(base); !slices.Equal(got, []string{"A=1", "B=2", "ELECTRON_RUN_AS_NODE=1"}) {
		t.Fatalf("app binary: %q", got)
	}
	if !slices.Equal(base, []string{"A=1", "ELECTRON_RUN_AS_NODE=1", "B=2"}) {
		t.Fatalf("base changed: %q", base)
	}
}

// A line longer than the buffer is cut and the lines after it still come (#3959): the program's log is read to its end.
func TestLogLinesReadsPastALongLine(t *testing.T) {
	var got []string
	long := strings.Repeat("y", 2<<20) // past any buffer the reader may have
	logLines(strings.NewReader("one\r\n"+long+"\n"+strings.Repeat("z", 3000)+"\ntwo"), func(l string) { got = append(got, l) })
	want := []string{"one", long[:2000] + "…", strings.Repeat("z", 2000) + "…", "two"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %d lines, want %d: %.60q", len(got), len(want), got)
	}
}
