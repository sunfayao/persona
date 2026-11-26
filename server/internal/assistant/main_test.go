package assistant

import (
	"os"
	"testing"

	"github.com/nwverse/persona/server/internal/testhome"
)

// The package's tests never see the developer's HOME or PERSONA_HOME (testhome); most of them need a database and skip
// in CI, this one keeps the TestMain wiring from being dropped there.
func TestTestsRunInAThrowAwayHome(t *testing.T) {
	if !testhome.Active() || os.Getenv("PERSONA_HOME") != "" {
		t.Fatalf("not isolated: active %v, PERSONA_HOME %q", testhome.Active(), os.Getenv("PERSONA_HOME"))
	}
}
