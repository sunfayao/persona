package readsize

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestFitTailKeepsNewestWholeBlocks(t *testing.T) {
	t.Parallel()
	blocks := []string{strings.Repeat("a", 40) + "\n", strings.Repeat("b", 40) + "\n", strings.Repeat("c", 40) + "\n"}
	kept, omitted := FitTail(blocks, 100)
	if kept != blocks[1]+blocks[2] || omitted != 41 {
		t.Fatalf("kept %q omitted %d", kept, omitted)
	}
	if kept, omitted = FitTail(blocks, 1000); kept != strings.Join(blocks, "") || omitted != 0 {
		t.Fatalf("everything fits: %d %d", len(kept), omitted)
	}
}

func TestFitTailCutsOnlyAnOversizedNewestBlockAtARuneBoundary(t *testing.T) {
	t.Parallel()
	big := strings.Repeat("好", 100)
	kept, omitted := FitTail([]string{"old\n", big}, 50)
	if len(kept) > 50 || !utf8.ValidString(kept) || !strings.HasSuffix(big, kept) || omitted != 4+len(big)-len(kept) {
		t.Fatalf("kept %d bytes %q omitted %d", len(kept), kept, omitted)
	}
}

func TestTranscriptDefaultIsSmallAndExplicitTurnsAreNot(t *testing.T) {
	t.Parallel()
	var items []proto.Item
	for i := 0; i < 20; i++ {
		items = append(items, proto.Item{Kind: proto.KindText, Role: "user", Text: "question " + strings.Repeat("q", 1500)},
			proto.Item{Kind: proto.KindText, Role: "assistant", Text: "answer " + strings.Repeat("a", 1500)})
	}
	small := Transcript("Agent on box", items, 0, nil)
	if len(small) > TranscriptPreview+200 || !strings.HasPrefix(small, "[") || !strings.Contains(small, "agent_read with turns=2") ||
		!strings.Contains(small, "Agent on box\n\n") || !strings.HasSuffix(small, "a\n\n") {
		t.Fatalf("default read: %d bytes, starts %.80q", len(small), small)
	}
	big := Transcript("Agent on box", items, 2, nil)
	if len(big) < 30000 || len(big) > 40100 {
		t.Fatalf("explicit turns read: %d bytes", len(big))
	}
}

func TestTurns(t *testing.T) {
	t.Parallel()
	for in, want := range map[int]int{-1: 2, 0: 2, 1: 1, 7: 7, 50: 10} {
		if got := Turns(in); got != want {
			t.Errorf("Turns(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestFitRowsDropsIdleThenShortensThenActiveTailButNeverPinned(t *testing.T) {
	t.Parallel()
	row := func(id string, pinned bool) Row {
		return Row{Full: id + strings.Repeat("x", 90) + "\n", Short: id + "\n", Pinned: pinned}
	}
	groups := []Group{
		{Sticky: true, Rows: []Row{row("n1", true), row("w1", false), row("w2", false)}},
		{Rows: []Row{row("i1", false), row("i2", false)}},
	}
	text, left := FitRows(1000, groups)
	if left[0] != 0 || left[1] != 0 || len(text[1]) != 2 {
		t.Fatalf("everything fits: %v", left)
	}
	text, left = FitRows(380, groups)
	if left[1] != 1 || left[0] != 0 || len(text[0]) != 3 || len(text[1]) != 1 || len(text[0][2]) < 90 {
		t.Fatalf("idle tail goes first: %v %v", left, text)
	}
	text, left = FitRows(200, groups)
	if left[1] != 2 || left[0] != 0 || len(text[0][0]) < 90 || text[0][2] != "w2\n" {
		t.Fatalf("then the last rows are shortened: %v %v", left, text)
	}
	text, left = FitRows(5, groups)
	if left[1] != 2 || left[0] != 2 || len(text[0]) != 1 || !strings.HasPrefix(text[0][0], "n1") {
		t.Fatalf("pinned row dropped: %v %v", left, text)
	}
}
