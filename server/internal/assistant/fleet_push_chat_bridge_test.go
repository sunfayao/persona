package assistant

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/nwverse/persona/server/internal/proto"
	"github.com/nwverse/persona/server/internal/sessionorigin"
)

// bridgeSession is supAgentSession hosted in a Lark chat bridge: its pane and transport are those of the first
// "feishu" row of the origin rule's shared fixture, so the tests follow what sessionorigin.Of calls a chat bridge.
func bridgeSession(t *testing.T) proto.Session {
	t.Helper()
	raw, err := os.ReadFile("../sessionorigin/testdata/pane_sources.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Want    string        `json:"want"`
		Session proto.Session `json:"session"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Want != "feishu" {
			continue
		}
		sess := supAgentSession()
		sess.PaneID, sess.SourceKind = r.Session.PaneID, r.Session.SourceKind
		if got := sessionorigin.Of(sess); got != "feishu" {
			t.Fatalf("fixture session's origin = %q, want feishu", got)
		}
		return sess
	}
	t.Fatal("no feishu row in the origin fixture")
	return proto.Session{}
}

// bridgeHerdrSession is supAgentSession in a pane of the herdr session a chat bridge runs its agents in on its herdr
// backend (#8845): named as the bridge's own panes are, as the node writes a named session's pane (herdr.QualifyID).
func bridgeHerdrSession(t *testing.T) proto.Session {
	t.Helper()
	bridge, _, _ := strings.Cut(bridgeSession(t).PaneID, ":")
	sess := supAgentSession()
	sess.PaneID = "@" + bridge + ":wCD:p1"
	if got := sessionorigin.Of(sess); got != "terminal" {
		t.Fatalf("a herdr pane's origin = %q, want terminal", got)
	}
	return sess
}

// finishOnce drives one finished turn of sess (supAgent on mac) and decides it at once.
func (e *supervisedEnv) finishOnce(sess proto.Session) {
	e.t.Helper()
	e.holdDone()
	driveAgentDone(e.fleetTest, sess)
	e.finishNow()
}
