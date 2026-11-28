package signalrule

import (
	"strings"
	"testing"
	"time"
)

func TestSentUp(t *testing.T) {
	for _, tc := range []struct {
		kind, report string
		want         bool
	}{
		{"review", "completion", true}, {"decide", "", true}, {"escalated", "", true},
		{"review", "", false}, {"report", "done", false}, {"stalled", "", false}, {"ask", "", false},
	} {
		if got := SentUp(tc.kind, tc.report); got != tc.want {
			t.Errorf("SentUp(%q, %q) = %v", tc.kind, tc.report, got)
		}
	}
}

func TestReportAnswered(t *testing.T) {
	at := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		reviewed time.Time
		want     bool
	}{{time.Time{}, false}, {at.Add(-time.Second), false}, {at, false}, {at.Add(time.Second), true}} {
		if got := ReportAnswered(at, tc.reviewed); got != tc.want {
			t.Errorf("review at %v: %v", tc.reviewed, got)
		}
	}
}

func TestCardlessStart(t *testing.T) {
	base := time.Date(2026, 11, 28, 10, 0, 0, 0, time.UTC)
	told := base.Add(40 * time.Minute)
	stale := base.Add(-time.Hour)
	for _, tc := range []struct {
		name string
		told *time.Time
		open bool
		want time.Time
		ok   bool
	}{
		{"its review waits", &told, true, time.Time{}, false},
		{"counts from the delivery", &told, false, told, true},
		{"a delivery of an earlier round does not count", &stale, false, base, true},
		{"no record of one", nil, false, base, true},
	} {
		got, ok := CardlessStart(base, tc.told, tc.open)
		if ok != tc.ok || !got.Equal(tc.want) {
			t.Errorf("%s: %v, %v", tc.name, got, ok)
		}
	}
}

func TestExtension(t *testing.T) {
	for _, tc := range []struct {
		expect, done int
		progressed   bool
		want         int
		ok           bool
	}{
		{60, 0, true, 90, true}, {90, 1, true, 135, true}, {10, 0, true, 20, true}, {16, 0, true, 26, true},
		{135, 2, true, 135, false}, {60, 0, false, 60, false}, {0, 0, true, 0, false},
	} {
		if got, ok := Extension(tc.expect, tc.done, tc.progressed); got != tc.want || ok != tc.ok {
			t.Errorf("Extension(%d, %d, %v) = %d, %v", tc.expect, tc.done, tc.progressed, got, ok)
		}
	}
	if ProgressWindow(20) != 10*time.Minute || ProgressWindow(120) != 30*time.Minute {
		t.Error("the progress window is the larger of ten minutes and a quarter of the estimate")
	}
}

func TestRepeats(t *testing.T) {
	for _, tc := range []struct {
		text, said string
		want       bool
	}{
		{"All done.\n  PR merged", "All done. PR merged", true}, {"All done. PR merged, thanks", "PR merged", false}, {"All done. PR merged, thanks", "All done. PR merged, thank", true},
		{"The long final message with details of the whole change", "Done.", false}, {"PR merged", "All done. PR merged, thanks", true},
		{"", "x", false}, {"x", "", false}, {"one thing", "another", false},
	} {
		if got := Repeats(tc.text, tc.said); got != tc.want {
			t.Errorf("Repeats(%q, %q) = %v", tc.text, tc.said, got)
		}
	}
}

func TestSameWork(t *testing.T) {
	if refs := IssueRefs("fix #12 and #2813, see https://github.com/o/r/pull/3001"); strings.Join(refs, ",") != "#2813,#3001" {
		t.Fatalf("refs = %q", refs)
	}
	if !DirsOverlap("/code/app/", "/code/app/web") || DirsOverlap("/code/app", "/code/apple") || DirsOverlap("", "/x") {
		t.Error("directories overlap when one holds the other")
	}
	if TitleSimilarity("fix login crash", "fix login crash on ipad") < 0.7 || TitleSimilarity("fix login crash", "write docs") > 0.4 {
		t.Error("similar titles score high, others low")
	}
}
