package assistant

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db/dbtest"
	"github.com/nwverse/persona/server/internal/testhome"
)

// The tests that need Postgres run on a database of their own (dbtest, #583); without PERSONA_TEST_DATABASE_URL they
// skip. Run with a throw-away HOME (testhome): nothing in the package may write the default ~/.persona. Every inbox
// list checks the item bytes GET /api/v1/inbox kept against a fresh encoding (inboxMemoVerify): a change of an item
// that forgets nothing (touched, chainAdd, saveInboxPrepared) fails the run.
//
// Retries that give a real model or hub time to recover wait a millisecond here: the fakes answer a retry as they
// answered the first try, so the real pause (three seconds for the done judge) only
// added about a minute and a half to the package's run (#7644). The tests of the retries themselves set their own.
func TestMain(m *testing.M) {
	inboxMemoVerify = true
	taskDoneRetryAfter = time.Millisecond
	os.Exit(testhome.Main(func() int { return staleListedCheck(dbtest.Main(m)) }))
}


// The package's tests never see the developer's HOME or PERSONA_HOME (testhome); most of them need a database and skip
// in CI, this one keeps the TestMain wiring from being dropped there.
func TestTestsRunInAThrowAwayHome(t *testing.T) {
	if !testhome.Active() || os.Getenv("PERSONA_HOME") != "" {
		t.Fatalf("not isolated: active %v, PERSONA_HOME %q", testhome.Active(), os.Getenv("PERSONA_HOME"))
	}
}
