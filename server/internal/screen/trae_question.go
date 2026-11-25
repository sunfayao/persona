package screen

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"

	"github.com/nwverse/persona/server/internal/lazyre"
)

// TraeQuestionRev identifies an answer that must be checked and sent through TRAE's own question overlay.
const TraeQuestionRev = "trae-question:"

// TraeQuestion is one visible page of TRAE CLI's AskUserQuestion. A recognized page without options in Dialog
// belongs in the terminal. FreeText is only for local readback, never projected into Dialog.
type TraeQuestion struct {
	Dialog             Dialog
	Index, Total, Left int
	SubmitsAll         bool
	FreeText           string
}

var (
	traeQuestionHead = lazyre.New(`^\s*Question (\d+)/(\d+) \((\d+) unanswered\)\s*$`)
	traeQuestionRow  = lazyre.New(`^(\s*)([›❯>]\s*)?(\[[ x✓✔]\]\s*)?(\d+)\.\s+(\S.*)$`)
)

// ReadTraeQuestion recognizes the native blocking overlay. A whole page needs a verified footer, consecutively
// numbered choices, a visible cursor and the native free-text option. A clipped or unsupported page still gives
// callers an overlay to hand back to the terminal.
func ReadTraeQuestion(text string) *TraeQuestion {
	raw := strings.Split(strings.ReplaceAll(text, "\r", ""), "\n")
	for i := range raw {
		raw[i] = strings.TrimRight(Plain(raw[i]), " ")
	}
	head, footer := -1, -1
	for i := len(raw) - 1; i >= 0; i-- {
		if footer == -1 && strings.TrimSpace(raw[i]) != "" {
			footer = i
		}
		if traeQuestionHead.MatchString(raw[i]) {
			head = i
			break
		}
	}
	if head == -1 {
		return nil
	}
	match := traeQuestionHead.FindStringSubmatch(raw[head])
	q := &TraeQuestion{}
	q.Dialog.Header = strings.TrimSpace(raw[head])
	q.Index, _ = strconv.Atoi(match[1])
	q.Total, _ = strconv.Atoi(match[2])
	q.Left, _ = strconv.Atoi(match[3])

	// The help row can wrap. Its first line has distinct native guidance, unlike an earlier heading in chat output.
	hints := ""
	at := footer
	identifiedFooter := false
	for i := footer; i > head && i > footer-4; i-- {
		hints = strings.Join(strings.Fields(raw[i]+" "+hints), " ")
		if strings.Contains(hints, "enter submit") && (strings.Contains(hints, "add notes") || strings.Contains(hints, "clear answer") || strings.Contains(hints, "clear notes")) {
			at = i
			identifiedFooter = true
			break
		}
	}
	if !identifiedFooter && !strings.Contains(hints, "clear answer") && !strings.Contains(hints, "add notes") {
		// When the footer was cut, only a native pane boundary just above the header distinguishes the
		// incomplete overlay from a line that merely quotes a previous question.
		if head == 0 || !strings.HasPrefix(strings.TrimSpace(raw[head-1]), "────") {
			return nil
		}
	}
	q.SubmitsAll = identifiedFooter && strings.Contains(hints, "enter submit all")
	canAdvance := identifiedFooter && strings.Contains(hints, "enter submit answer") && q.Index < q.Total
	canSubmit := q.SubmitsAll && q.Left == 1

	first := head + 1
	var question []string
	for ; first < at && !traeQuestionRow.MatchString(raw[first]); first++ {
		if line := strings.TrimSpace(raw[first]); line != "" {
			question = append(question, line)
		}
	}
	q.Dialog.Question = joinWrapped(question)

	var opts []Option
	lastRaw := ""
	valid := q.Dialog.Question != "" && q.Index > 0 && q.Total >= q.Index && q.Left > 0 && q.Left <= q.Total &&
		(canAdvance || canSubmit) && !strings.Contains(hints, "clear notes")
	for i := first; i < at; i++ {
		row := strings.TrimSpace(raw[i])
		if row == "" {
			continue
		}
		m := traeQuestionRow.FindStringSubmatch(raw[i])
		if m == nil {
			if len(opts) == 0 || raw[i] == row {
				valid = false
				break
			}
			o := &opts[len(opts)-1]
			if o.Detail != "" {
				o.Detail = joinWrapped([]string{o.Detail, row})
			} else {
				o.Label = joinWrapped([]string{o.Label, row})
			}
			continue
		}
		option := Option{Key: m[4], Label: m[5]}
		lastRaw = m[5]
		if m[3] != "" {
			valid = false // a native checkbox needs space to toggle before submitting
		}
		if cols := codexColumnsRE.FindStringSubmatch(option.Label); cols != nil {
			option.Label, option.Detail = cols[1], cols[2]
		}
		if option.Key != strconv.Itoa(len(opts)+1) {
			valid = false
		}
		if m[2] != "" {
			if q.Dialog.Cursor != "" {
				valid = false
			}
			q.Dialog.Cursor = option.Key
		}
		opts = append(opts, option)
	}
	if len(opts) >= 3 {
		last := &opts[len(opts)-1]
		switch {
		case strings.TrimSuffix(last.Label, ".") == "Type your answer":
		case last.Key == q.Dialog.Cursor && strings.Contains(hints, "clear answer"):
			q.FreeText = lastRaw
		default:
			valid = false
		}
		last.Kind, last.Label, last.Detail = KindTypeSomething, "Type your answer", ""
	} else {
		valid = false
	}
	if valid && q.Dialog.Cursor != "" {
		q.Dialog.Options = opts
	}
	// Cursor moves and free text do not change the question. Exact option text, descriptions, and the action do.
	fingerprint := fmt.Sprintf("%s\x00%s\x00%t\x00%v", q.Dialog.Header, q.Dialog.Question, q.SubmitsAll, q.Dialog.Options)
	q.Dialog.Rev = TraeQuestionRev + fmt.Sprintf("%x", sha256.Sum256([]byte(fingerprint)))[:16]
	return q
}
