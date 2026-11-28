// Package readsize fits what a read tool returns into a byte budget small enough for a model's tool-result preview
// (a runtime that shows only the middle of a result above about 10 KB hides the newest part of a transcript).
// It holds pure functions: text and rows in, text and counts out.
package readsize

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

const (
	// TranscriptPreview is what agent_read returns when the caller did not ask for a number of exchanges.
	TranscriptPreview = 8000
	// ListPreview is what agents_list's rows come to when they are not narrowed by the caller.
	ListPreview = 9000
	// FreeText is the budget of one row's conclusion or last-said line.
	FreeText = 120
	// ListReserve is the room agents_list keeps for its group headers and "more" lines.
	ListReserve = 600
	// NoticeMax is the room Notice may take.
	NoticeMax = 140
)

// FitTail keeps the newest blocks (messages, one-line tool calls) that fit budget bytes, oldest first, and never cuts
// inside a block that fits. A newest block that alone is larger than budget is cut from its start, at a rune
// boundary. omitted is how many bytes of the earlier text are left out.
func FitTail(blocks []string, budget int) (kept string, omitted int) {
	total, used, first := 0, 0, len(blocks)
	for _, b := range blocks {
		total += len(b)
	}
	for first > 0 && used+len(blocks[first-1]) <= budget {
		first--
		used += len(blocks[first])
	}
	if first == len(blocks) && len(blocks) > 0 {
		last := blocks[len(blocks)-1]
		start := max(len(last)-budget, 0)
		for start < len(last) && !utf8.RuneStart(last[start]) {
			start++
		}
		return last[start:], total - (len(last) - start)
	}
	return strings.Join(blocks[first:], ""), total - used
}

// Turns is how many recent exchanges a read fetches: 2 when the caller named none, at most 10.
func Turns(n int) int {
	if n <= 0 {
		return 2
	}
	return min(n, 10)
}

// Transcript is a transcript as agent_read gives it: with turns named by the caller, textfmt.Transcript's (up to 40000
// bytes); else whole messages, newest last, within TranscriptPreview, and a first line saying what is left out.
func Transcript(head string, items []proto.Item, turns int, link func(id string) string) string {
	if turns > 0 {
		return textfmt.Transcript(head, items, link)
	}
	kept, omitted := FitTail(textfmt.TranscriptBlocks(items, link), TranscriptPreview-len(head)-NoticeMax)
	out := head + "\n\n" + kept
	if omitted > 0 {
		out = Notice(omitted, "agent_read with turns=2") + out
	}
	return out
}

// Notice is the first line of a cut transcript: how much earlier text there is and which call fetches it.
func Notice(omitted int, call string) string {
	return fmt.Sprintf("[%d earlier bytes not shown; %s returns them (up to 40000 bytes; turns up to 10 reach older exchanges)]\n", omitted, call)
}

// Row is one rendered listing row. Short is a smaller rendering (empty: none); a Pinned row is never dropped.
type Row struct {
	Full, Short string
	Pinned      bool
}

// Group is rows in order of importance. A Sticky group's rows go last: the other groups lose their tails first.
type Group struct {
	Rows   []Row
	Sticky bool
}

// FitRows brings the groups' rows under budget bytes. It drops the tails of the groups that are not Sticky, then
// switches rows to their Short rendering from the end, then drops the tails of the Sticky groups, never a Pinned
// row. left is how many rows each group lost; text is the rendering of the kept rows.
func FitRows(budget int, groups []Group) (text [][]string, left []int) {
	n := make([]int, len(groups))
	short := make([][]bool, len(groups))
	size := 0
	for g, grp := range groups {
		n[g], short[g] = len(grp.Rows), make([]bool, len(grp.Rows))
		for _, r := range grp.Rows {
			size += len(r.Full)
		}
	}
	drop := func(sticky bool) {
		for g := len(groups) - 1; g >= 0 && size > budget; g-- {
			for groups[g].Sticky == sticky && n[g] > 0 && size > budget && !groups[g].Rows[n[g]-1].Pinned {
				n[g]--
				r := groups[g].Rows[n[g]]
				size -= len(r.Full)
				if short[g][n[g]] {
					size += len(r.Full) - len(r.Short)
				}
			}
		}
	}
	drop(false)
	for g := len(groups) - 1; g >= 0 && size > budget; g-- {
		for i := n[g] - 1; i >= 0 && size > budget; i-- {
			if r := groups[g].Rows[i]; r.Short != "" && len(r.Short) < len(r.Full) {
				short[g][i] = true
				size -= len(r.Full) - len(r.Short)
			}
		}
	}
	drop(true)
	text, left = make([][]string, len(groups)), make([]int, len(groups))
	for g, grp := range groups {
		left[g] = len(grp.Rows) - n[g]
		for i := 0; i < n[g]; i++ {
			if short[g][i] {
				text[g] = append(text[g], grp.Rows[i].Short)
			} else {
				text[g] = append(text[g], grp.Rows[i].Full)
			}
		}
	}
	return text, left
}
