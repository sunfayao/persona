// Package criteria is the pure logic of a supervised task's acceptance criteria (done_when): cleaning and validating
// them, numbering them, matching a review check to one by number or text, and applying add/drop/edit changes. It
// works on plain values and imports neither the assistant root nor a sibling domain.
package criteria

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/textfmt"
)

const (
	// Max is the most runes a criterion may have: one checkable result. Procedure and context belong in the prompt.
	Max = 200
	// MostCount is the most criteria a task keeps.
	MostCount = 6
)

// Clean is a list of criteria as stored: trimmed, one line each, no empty or repeated ones, at most MostCount. A
// longer criterion is clipped: that is for text already stored and for lists that are not criteria (out_of_bounds);
// new criteria go through TooLong first.
func Clean(list []string) []string {
	out := []string{}
	for _, c := range list {
		c = textfmt.Clip(textfmt.OneLine(c), Max)
		if c != "" && !slices.Contains(out, c) && len(out) < MostCount {
			out = append(out, c)
		}
	}
	return out
}

// Fit is a criterion's text as a check may carry it: one line, clipped to Max runes, except that a stored criterion
// that was clipped by an older build (Max runes and the clip mark) is already a whole criterion and keeps its mark.
func Fit(s string) string {
	s = textfmt.OneLine(s)
	if strings.HasSuffix(s, "…") && utf8.RuneCountInString(s) == Max+1 {
		return s
	}
	return textfmt.Clip(s, Max)
}

// TooLong refuses a new criterion longer than Max runes (after one-lining) and says which one. what names the field.
// A stored criterion that an older build clipped (Max runes and the clip mark, see Fit) is not new and passes.
func TooLong(what string, list []string) error { return TooLongNew(what, list, nil) }

// TooLongNew is TooLong for a whole list sent back: an item that is already in current, unchanged, is not new.
func TooLongNew(what string, list, current []string) error {
	for i, c := range list {
		if slices.Contains(current, c) {
			continue
		}
		if n := utf8.RuneCountInString(textfmt.OneLine(c)); n > Max && Fit(c) != textfmt.OneLine(c) {
			return fmt.Errorf("%s item %d is %d characters: a criterion is one checkable result of at most %d characters; put procedure "+
				"and context in the prompt, not in the criteria. Nothing was written", what, i+1, n, Max)
		}
	}
	return nil
}

// Same reports whether got is want, spacing aside.
func Same(got, want string) bool {
	return strings.Join(strings.Fields(got), " ") == strings.Join(strings.Fields(want), " ")
}

// Numbered lists criteria one per line with their one-based numbers.
func Numbered(list []string) string {
	var b strings.Builder
	for i, c := range list {
		if i > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "%d. %s", i+1, c)
	}
	return b.String()
}

// Resolve is the criterion a review check names by its one-based number, its text, or both (which must agree). The
// text is returned as the list keeps it; a check with neither is the empty text.
func Resolve(doneWhen []string, number int, text string) (string, error) {
	text = Fit(text)
	if number == 0 {
		return text, nil
	}
	if number < 1 || number > len(doneWhen) {
		return "", fmt.Errorf("check number %d: done_when has %d criteria, numbered from 1; nothing was written", number, len(doneWhen))
	}
	if text != "" && !Same(text, Fit(doneWhen[number-1])) {
		return "", fmt.Errorf("check number %d is done_when %q but its criterion text names another one; give the number or the text, not two different ones; nothing was written", number, doneWhen[number-1])
	}
	return doneWhen[number-1], nil
}

// Diff is what a whole-list replacement adds to old and takes out of it, by exact text.
func Diff(old, next []string) (added, dropped []string) {
	for _, x := range next {
		if !slices.Contains(old, x) {
			added = append(added, x)
		}
	}
	for _, x := range old {
		if !slices.Contains(next, x) {
			dropped = append(dropped, x)
		}
	}
	return added, dropped
}

// Edit replaces criterion Number's text.
type Edit struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// Ops are the incremental changes task_scope makes to a list.
type Ops struct {
	Add  []string
	Drop []int
	Edit []Edit
}

// Empty reports whether no operation is given.
func (o Ops) Empty() bool { return len(o.Add)+len(o.Drop)+len(o.Edit) == 0 }

// Change is one edited criterion.
type Change struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Applied is the list after Apply and what changed in it.
type Applied struct {
	Next, Added, Dropped []string
	Edited               []Change
}

// Apply makes the changes of o to cur. An edit keeps the criterion's position and is never a drop; a drop removes it;
// an added criterion goes last. The result must hold 1 to MostCount distinct criteria of at most Max characters.
func Apply(cur []string, o Ops) (Applied, error) {
	if err := TooLong("add", o.Add); err != nil {
		return Applied{}, err
	}
	texts := make([]string, len(o.Edit))
	for i, e := range o.Edit {
		texts[i] = e.Text
	}
	if err := TooLong("edit", texts); err != nil {
		return Applied{}, err
	}
	touched := map[int]bool{}
	for _, n := range append(slices.Clone(o.Drop), editNumbers(o.Edit)...) {
		switch {
		case n < 1 || n > len(cur):
			return Applied{}, fmt.Errorf("number %d: done_when has %d criteria, numbered from 1", n, len(cur))
		case touched[n]:
			return Applied{}, fmt.Errorf("number %d appears twice in drop and edit", n)
		}
		touched[n] = true
	}
	var a Applied
	to := map[int]string{}
	for _, e := range o.Edit {
		t := textfmt.OneLine(e.Text)
		if t == "" {
			return Applied{}, fmt.Errorf("edit number %d: give the new text", e.Number)
		}
		to[e.Number] = t
	}
	for i, c := range cur {
		switch t, edited := to[i+1]; {
		case slices.Contains(o.Drop, i+1):
			a.Dropped = append(a.Dropped, c)
		case edited && t != c:
			a.Next, a.Edited = append(a.Next, t), append(a.Edited, Change{From: c, To: t})
		default:
			a.Next = append(a.Next, c)
		}
	}
	for _, c := range o.Add {
		if c = textfmt.OneLine(c); c != "" {
			a.Next, a.Added = append(a.Next, c), append(a.Added, c)
		}
	}
	seen := map[string]bool{}
	for _, c := range a.Next {
		if seen[c] {
			return Applied{}, fmt.Errorf("criterion %q would appear twice", c)
		}
		seen[c] = true
	}
	switch {
	case len(a.Next) == 0:
		return Applied{}, errors.New("a task is never left with no criteria")
	case len(a.Next) > MostCount:
		return Applied{}, fmt.Errorf("a task has at most %d criteria; the result would have %d", MostCount, len(a.Next))
	}
	return a, nil
}

func editNumbers(edits []Edit) []int {
	out := make([]int, len(edits))
	for i, e := range edits {
		out[i] = e.Number
	}
	return out
}
