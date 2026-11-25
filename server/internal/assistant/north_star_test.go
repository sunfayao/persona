package assistant

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nwverse/persona/server/internal/db"
	"github.com/nwverse/persona/server/internal/i18n"
	"github.com/nwverse/persona/server/internal/prompts"
)

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func shanghai(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	mustDo(t, err)
	return loc
}

type failingNorthStarStore struct{ Store }

func (failingNorthStarStore) Setting(context.Context, string, any) (bool, error) {
	return false, errors.New("unavailable")
}
func (failingNorthStarStore) NorthStarCards(context.Context, time.Time, time.Time) ([]db.NorthStarCard, error) {
	return nil, errors.New("unavailable")
}
func (failingNorthStarStore) WorkEventsBetween(context.Context, time.Time, time.Time, []string) ([]db.WorkEvent, error) {
	return nil, errors.New("unavailable")
}

func TestNorthStarHelpers(t *testing.T) {
	for _, c := range []struct{ code, msg, want string }{
		{"internal", "assistant exited: No conversation found with session ID: x", "conversation_not_found"},
		{"timeout", "", "timeout"},
		{"node_unreachable", "", "machine_offline"},
		{"", "context canceled", "cancelled"},
		{"claude_missing", "", "claude_missing"},
		{"internal", "boom", "internal"},
	} {
		if got := sendReason(c.code, c.msg); got != c.want {
			t.Errorf("sendReason(%q, %q) = %q, want %q", c.code, c.msg, got, c.want)
		}
	}
	for dir, want := range map[string]bool{"/tmp": true, "/tmp/x/y": true, "/private/tmp/a": true, "/var/folders/ab/T/x": true, "/tmpfoo": false, "/home/me/code": false, "": false} {
		if probeDir(dir) != want {
			t.Errorf("probeDir(%q) != %v", dir, want)
		}
	}
	if percent(1) != "100%" || percent(2.0/3) != "66.7%" || signedPoints(0.25) != "+25 pp" || signedPoints(-0.0001) != "0 pp" || signed(-2) != "-2" {
		t.Fatal("formatting")
	}
}

func TestGoldenNorthStarBriefFacts(t *testing.T) {
	golden(t, "routine_facts.north_star", prompts.RenderBriefFacts(prompts.BriefFacts{Hours: 12, NorthStar: []string{
		"Interruptions today: 1 (0 questions · 0 cards to act on · 1 notifications · 0 authorization cards); yesterday by this time 3, -2",
		"Availability today (main and leader conversations): 75% (3 of 4 starts and resumes worked); yesterday by this time 50%, +25 pp",
	}}))
}

// Failure reasons are said in the briefing's language; a node's own code without a text is named as it is.
func TestNorthStarFailureReasonsAreLocalized(t *testing.T) {
	b := briefNorthStar{todayOK: true, Today: NorthStar{Availability: Availability{Failures: []FailureReason{{"conversation_not_found", 2}, {"claude_missing", 1}}}}}
	for l, want := range map[i18n.Lang]string{
		"en":        "Failures by reason: conversation not found 2, other (claude_missing) 1",
		i18n.ZhHans: "失败原因：找不到对话 2, 其他（claude_missing） 1",
		"ja":        "失敗の理由：会話が見つかりません 2, その他（claude_missing） 1",
	} {
		lines := b.lines(l)
		if lines[2] != want {
			t.Errorf("%s: %q, want %q", l, lines[2], want)
		}
	}
}

func TestGoldenNorthStarRunFacts(t *testing.T) {
	golden(t, "north_star_facts", prompts.RenderNorthStarFacts([]string{
		"Interruptions today: 1 (0 questions · 0 cards to act on · 1 notifications · 0 authorization cards); yesterday by this time 3, -2",
		"Availability today (main and leader conversations): 75% (3 of 4 starts and resumes worked); yesterday by this time 50%, +25 pp",
	}))
}

// northStarDB is an isolated database with the main conversation "main-chat".
func northStarDB(t *testing.T) (*Service, *db.DB, string) {
	t.Helper()
	dsn := newDatabase(t)
	database, err := db.Open(context.Background(), dsn, db.ManagerSet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	s, _ := newQuietServiceOn(t, database)
	setSettings(t, s, func(v *Settings) { v.Main = "main-chat" })
	return s, database, dsn
}
