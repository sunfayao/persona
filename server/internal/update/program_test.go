package update

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sunfayao/persona/server/internal/nodeproto"
)

// The suffix ProgramOf looks for is the package persona-node is built from.
func TestProgramOfMatchesTheCommandDirectory(t *testing.T) {
	if fi, err := os.Stat(filepath.Join("..", "..", "cmd", nodeproto.ProgramPersonaNode, "main.go")); err != nil || fi.IsDir() {
		t.Fatalf("server/cmd/%s/main.go: %v", nodeproto.ProgramPersonaNode, err)
	}
}
