package ownerfence

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/sunfayao/persona/server/internal/prompts"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/textfmt"
)

type QuestionEnd struct {
	OK      bool   `json:"ok"`
	Status  string `json:"status,omitempty"`
	Answer  string `json:"answer,omitempty"`
	ByOwner bool   `json:"by_owner,omitempty"`
}

func QuestionAnswerText(r proto.Resolution) string {
	if len(r.Answers) == 0 {
		return textfmt.Clip(r.Text, 1200)
	}
	keys := make([]string, 0, len(r.Answers))
	for k := range r.Answers {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	values := make([]string, 0, len(keys))
	for _, k := range keys {
		values = append(values, r.Answers[k])
	}
	return textfmt.Clip(strings.Join(values, "\n"), 1200)
}

// InteractionAnswers checks a question's answers against the questions themselves: every question answered, by the
// wording it was asked, with options it offers (an option written out with its description counts as its label,
// optionLabel). A multi-select takes several labels and becomes the app's own form, "A, B". Free text is refused: the
// user answers that in the app, or through agent_send.
func InteractionAnswers(ix proto.Interaction, args json.RawMessage) (map[string]string, error) {
	var raw map[string]json.RawMessage
	if len(args) == 0 || json.Unmarshal(args, &raw) != nil {
		return nil, fmt.Errorf("answers is required: an object of question → option label")
	}
	asked := map[string]bool{}
	for _, q := range ix.Questions {
		asked[q.Question] = true
	}
	for k := range raw {
		if !asked[k] {
			return nil, fmt.Errorf("%q is not a question this interaction asks; it asks: %s", k, askedQuestions(ix))
		}
	}
	answers := make(map[string]string, len(ix.Questions))
	for _, q := range ix.Questions {
		v, ok := raw[q.Question]
		if !ok {
			return nil, fmt.Errorf("every question needs an answer, %q has none (options: %s)", q.Question, OptionLabels(q))
		}
		var one string
		var many []string
		if json.Unmarshal(v, &many) != nil {
			if json.Unmarshal(v, &one) != nil {
				return nil, fmt.Errorf("%q takes an option label or an array of them (options: %s)", q.Question, OptionLabels(q))
			}
			if !q.MultiSelect {
				answers[q.Question] = optionLabel(q, one)
			} else {
				many = []string{one}
			}
		}
		if many != nil {
			if !q.MultiSelect && len(many) > 1 {
				return nil, fmt.Errorf("%q takes one option; pick one of: %s", q.Question, OptionLabels(q))
			}
			for i, a := range many {
				many[i] = optionLabel(q, a)
			}
			answers[q.Question] = strings.Join(many, ", ")
		}
		if err := CheckOptions(q, answers[q.Question]); err != nil {
			return nil, err
		}
	}
	return answers, nil
}

// checkOptions refuses an answer whose labels are not the question's options, listing them.
func CheckOptions(q proto.Question, answer string) error {
	offered := map[string]bool{}
	for _, o := range q.Options {
		offered[o.Label] = true
	}
	picked := 0
	for _, label := range strings.Split(answer, ", ") {
		if !offered[label] {
			return fmt.Errorf("%q is not one of %q's options: %s", label, q.Question, OptionLabels(q))
		}
		picked++
	}
	if picked == 0 {
		return fmt.Errorf("%q needs an option: %s", q.Question, OptionLabels(q))
	}
	if !q.MultiSelect && picked > 1 {
		return fmt.Errorf("%q takes one option; pick one of: %s", q.Question, OptionLabels(q))
	}
	return nil
}

// optionLabel is the label of the option answer names: answer itself when it is a label. The delegation input lists
// an option as "label：description" (prompts/agents/delegate_input.md), and a model sometimes copies the whole line
// (#3929), so an answer that writes out exactly one option, its label and then its own description, is that option's
// label too: with "：", ":" or a dash between them, or the description in brackets, and spaces or quotes around the
// label or the whole answer. Anything else, the model's own words after a label or text that reads as two options,
// comes back as it is, for checkOptions to refuse.
func optionLabel(q proto.Question, answer string) string {
	for _, o := range q.Options {
		if o.Label == answer {
			return answer
		}
	}
	label := ""
	for _, o := range q.Options {
		if !writesOut(answer, o) {
			continue
		}
		if label != "" && label != o.Label {
			return answer
		}
		label = o.Label
	}
	if label == "" {
		return answer
	}
	return label
}

// labelSeparators may come between an option's label and its description; a longer one before its prefix.
var labelSeparators = []string{"——", "--", "：", ":", "—", "–", "－", "-"} // i18n-ignore: punctuation matched in a model's answer

// descBrackets may hold the description after an option's label.
var descBrackets = [][2]string{{"（", "）"}, {"(", ")"}} // i18n-ignore: punctuation matched in a model's answer

// labelQuotes may wrap an option's label, or a whole answer.
var labelQuotes = [][2]string{{`"`, `"`}, {"'", "'"}, {"`", "`"}, {"“", "”"}, {"‘", "’"}, {"「", "」"}, {"『", "』"}} // i18n-ignore: punctuation matched in a model's answer

// writesOut: answer is option o written out, its label alone or followed by its own description.
func writesOut(answer string, o proto.QuestionOption) bool {
	label := strings.TrimSpace(o.Label)
	if label == "" {
		return false
	}
	s := unquoted(answer)
	if s == label {
		return true
	}
	desc := plainDescription(o.Description)
	if desc == "" {
		return false
	}
	rest, ok := cutLabel(s, label)
	if !ok {
		return false
	}
	rest = strings.TrimSpace(rest)
	for _, sep := range labelSeparators {
		if d, ok := strings.CutPrefix(rest, sep); ok {
			return plainDescription(d) == desc
		}
	}
	for _, b := range descBrackets {
		if d, ok := strings.CutPrefix(rest, b[0]); ok {
			d, ok = strings.CutSuffix(d, b[1])
			return ok && plainDescription(d) == desc
		}
	}
	return false
}

// unquoted is s without the spaces and quotes around it.
func unquoted(s string) string {
	s = strings.TrimSpace(s)
	for again := true; again; {
		again = false
		for _, p := range labelQuotes {
			if len(s) > len(p[0])+len(p[1]) && strings.HasPrefix(s, p[0]) && strings.HasSuffix(s, p[1]) {
				s, again = strings.TrimSpace(s[len(p[0]):len(s)-len(p[1])]), true
				break
			}
		}
	}
	return s
}

// cutLabel is what follows label at the start of s, the label bare or in quotes.
func cutLabel(s, label string) (string, bool) {
	if rest, ok := strings.CutPrefix(s, label); ok {
		return rest, true
	}
	for _, p := range labelQuotes {
		if rest, ok := strings.CutPrefix(s, p[0]+label+p[1]); ok {
			return rest, true
		}
	}
	return "", false
}

// plainDescription is an option's description as compared: its words one space apart, no full stop at the end.
func plainDescription(s string) string {
	return strings.TrimRight(strings.Join(strings.Fields(s), " "), "。.") // i18n-ignore: punctuation matched in a model's answer
}

func OptionLabels(q proto.Question) string {
	labels := make([]string, len(q.Options))
	for i, o := range q.Options {
		labels[i] = o.Label
	}
	return strings.Join(labels, ", ")
}

func askedQuestions(ix proto.Interaction) string {
	qs := make([]string, len(ix.Questions))
	for i, q := range ix.Questions {
		qs[i] = q.Question
	}
	return strings.Join(qs, "; ")
}

func (e QuestionEnd) ToolError(id string) error {
	if e.Status == "resolved" && e.Answer != "" {
		return errors.New(prompts.RenderInteractionAlreadyAnswered(prompts.OwnerInputFence{Text: e.Answer, Owner: e.ByOwner}))
	}
	return interactionEnded(id, e.Status)
}

// interactionEnded says how an interaction that is no longer pending ended (status, from the hub's history), or that
// there is none.
func interactionEnded(id, status string) error {
	switch status {
	case "resolved":
		return fmt.Errorf("interaction %s was already answered", id)
	case "dismissed":
		return fmt.Errorf("interaction %s was dismissed", id)
	case "expired":
		return fmt.Errorf("interaction %s expired", id)
	case "cancelled":
		return fmt.Errorf("interaction %s was cancelled", id)
	}
	return fmt.Errorf("no pending interaction %s (use agents_list)", id)
}
