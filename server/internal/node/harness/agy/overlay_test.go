package agy

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func textItems(h *Projection) []string {
	out := make([]string, 0, len(h.items))
	for _, it := range h.items {
		out = append(out, it.Text)
	}
	return out
}

func TestOverlayCutsOnlyTheVerifiedNativeStepAndHidesOnlyMatchingInputs(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	r := New(Dependencies{Clock: testClock{}})
	input := agyParseLine(0, "USER_INPUT", "DONE", stamp, "Retried input")
	retry := agyParseLine(1, "USER_INPUT", "DONE", stamp, "Accepted retry")
	planner := agyParseLine(2, "PLANNER_RESPONSE", "DONE", stamp, "Abandoned")
	file := agyTestFile(t, ref.Root, ref.Conversation, input+retry+planner)
	base := readFixture(t, r, ref)
	ov := fmt.Sprintf(`{"version":1,"unknown":{"future":true},"cut":{"limit":2,"first":{"step_index":2,"type":"PLANNER_RESPONSE","created_at":%q}},"hidden":[{"step_index":0,"created_at":%q},{"step_index":1,"created_at":"2026-01-02T00:00:00Z"}]}`, stamp, stamp)
	path := writeFixture(t, ref, overlayPath, ov)
	cut := readFixture(t, r, ref)
	if got := textItems(cut); !slices.Equal(got, []string{"Accepted retry"}) || cut.token == base.token {
		t.Fatalf("cut/hide: %v, history %q", got, cut.token)
	}
	appendFixture(t, file, agyParseLine(2, "PLANNER_RESPONSE", "DONE", "2026-01-01T00:00:01Z", "Kept replacement"))
	if replaced := readFixture(t, r, ref); !slices.Equal(textItems(replaced), []string{"Accepted retry", "Kept replacement"}) || replaced.token == cut.token {
		t.Fatalf("the new planner inherited the stale cut: %v", textItems(replaced))
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if h := readFixture(t, r, ref); !slices.Equal(textItems(h), []string{"Retried input", "Accepted retry", "Kept replacement"}) || h.token == cut.token {
		t.Fatalf("removed overlay: %+v", h.items)
	}
}

func TestOverlayPlacesNoticesAndExpiresErrorsAtTheNextInput(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	r := New(Dependencies{Clock: testClock{}})
	file := agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Start")+
		agyParseLine(2, "PLANNER_RESPONSE", "DONE", stamp, "Reply"))
	writeFixture(t, ref, overlayPath, fmt.Sprintf(`{"version":1,"notices":[{"at":1,"created_at":%[1]q,"code":"mode_switched","text":"Mode changed"},{"at":20,"created_at":%[1]q,"code":"mode_switched","text":"At end"}],"errors":[{"after":2,"created_at":%[1]q,"text":"Model unavailable"}]}`, stamp))
	projected := readFixture(t, r, ref)
	if got := textItems(projected); !slices.Equal(got, []string{"Start", "Mode changed", "Reply", "At end", "Model unavailable"}) {
		t.Fatalf("notice and error placement: %v", got)
	}
	last := projected.items[len(projected.items)-1]
	if last.Kind != proto.KindNotice || last.Facets.Notice.Code != proto.NoticeCodeAPIError || projected.turnError != "Model unavailable" || last.Facets.TurnID != "t0" {
		t.Fatalf("native error: %+v", last)
	}
	appendFixture(t, file, agyParseLine(3, "USER_INPUT", "DONE", stamp, "Try again"))
	next := readFixture(t, r, ref)
	if got := textItems(next); !slices.Equal(got, []string{"Start", "Mode changed", "Reply", "Try again", "At end"}) || next.token == projected.token {
		t.Fatalf("error survived additional input: %v", got)
	}
}

func TestOverlayForkUsesTheSourceOnlyUntilTheBranchExports(t *testing.T) {
	t.Parallel()
	r := New(Dependencies{Clock: testClock{}})
	ref := Ref{t.TempDir(), agyTestID}
	source := Ref{ref.Root, otherID}
	agyTestFile(t, source.Root, source.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Start")+
		agyParseLine(1, "PLANNER_RESPONSE", "DONE", stamp, "Prefix")+agyParseLine(2, "USER_INPUT", "DONE", stamp, "Beyond the fork"))
	writeFixture(t, ref, overlayPath, fmt.Sprintf(`{"version":1,"fork":{"source":%q,"through":1}}`, source.Conversation))
	forked := readFixture(t, r, ref)
	if got := textItems(forked); !slices.Equal(got, []string{"Start", "Prefix"}) || forked.seq != 2 || forked.items[1].ID != readFixture(t, r, source).items[1].ID {
		t.Fatalf("borrowed branch: %v", got)
	}
	if readFixture(t, r, ref) != forked {
		t.Fatal("unchanged fork did not retain its projection")
	}
	agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Native branch"))
	if h := readFixture(t, r, ref); !slices.Equal(textItems(h), []string{"Native branch"}) || h.token == forked.token {
		t.Fatalf("export did not take over: %v", textItems(h))
	}
}

func TestReaderIgnoresInvalidAndSymlinkedOverlays(t *testing.T) {
	t.Parallel()
	ref := Ref{t.TempDir(), agyTestID}
	r := New(Dependencies{Clock: testClock{}})
	agyTestFile(t, ref.Root, ref.Conversation, agyParseLine(0, "USER_INPUT", "DONE", stamp, "Kept"))
	base := readFixture(t, r, ref)
	for _, bad := range []string{"{}", `{"version":2}`, `{"version":1,"hidden":null}`, `{"version":1,"fork":{"source":"../other","through":1}}`,
		fmt.Sprintf(`{"version":1,"hidden":[{"step_index":0,"created_at":%q}],"cut":{"limit":3}}`, stamp), "\xff"} {
		writeFixture(t, ref, overlayPath, bad)
		if h := readFixture(t, r, ref); h.token != base.token || !slices.Equal(textItems(h), []string{"Kept"}) {
			t.Fatalf("invalid overlay was applied: %s", bad)
		}
	}
	path := filepath.Join(ref.Root, "brain", ref.Conversation, overlayPath)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	elsewhere := writeFixture(t, Ref{t.TempDir(), otherID}, overlayPath, fmt.Sprintf(`{"version":1,"hidden":[{"step_index":0,"created_at":%q}]}`, stamp))
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if h := readFixture(t, r, ref); h.token != base.token || len(h.items) != 1 {
		t.Fatalf("followed a symlink: %+v", h.items)
	}
}
