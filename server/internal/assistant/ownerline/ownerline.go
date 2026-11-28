// Package ownerline finds the line of a final reply that asks the owner to act: an unambiguous offer or hand-off
// ("you need to", "would you like me to", the Chinese equivalents). It is pure text analysis: it takes the reply and
// keeps no state, and imports neither the assistant root nor a sibling domain.
package ownerline

import (
	"strings"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/lazyre"
)

const ownerSentenceEnds = ".!?;。！？；" // i18n-ignore: sentence punctuation for parser markers, not display copy

// ownerNoAction reports a complete no-action predicate after an owner marker. The sentence or clause must end there:
// "do nothing else but approve" is still an action, while an independent requirement after a period is handled by the
// next marker occurrence rather than being swallowed with this one.
func ownerNoAction(rest string) bool {
	rest = strings.TrimSpace(rest)
	for _, phrase := range []string{"do nothing else", "do nothing"} {
		if !strings.HasPrefix(rest, phrase) {
			continue
		}
		tail := strings.TrimSpace(rest[len(phrase):])
		terminator, _ := utf8.DecodeRuneInString(tail)
		if tail == "" || strings.ContainsRune(ownerSentenceEnds, terminator) {
			return true
		}
	}
	return false
}

// ownerContrastOffsets returns unquoted, word-boundary `but` conjunctions in [at,end). A comma before one is
// optional. Keeping these boundaries occurrence-local lets a condition govern only its own clause.
func ownerContrastOffsets(text, lower string, at, end int) []int {
	var out []int
	for from := at; from < end; {
		i := strings.Index(lower[from:end], "but")
		if i < 0 {
			break
		}
		i += from
		left := i > at && (lower[i-1] == ' ' || lower[i-1] == '\t' || lower[i-1] == ',')
		right := i+3 < end && (lower[i+3] == ' ' || lower[i+3] == '\t')
		clause := left && right && ownerButStartsClause(text, lower, at, i, end)
		if clause && !ownerMarkerQuoted(lower, i) {
			out = append(out, i)
		}
		from = i + 3
	}
	return out
}

// ownerButStartsClause distinguishes a genuine contrast from an exception phrase. An immediately following owner
// marker has no words in this slice and is a clause by construction. Otherwise `but` is a contrast by default, except
// for an indefinite-pronoun exception phrase or the "nothing … but …" idiom; a finite verb at the end of a
// determiner-led subject keeps that subject a clause, while a verb-like noun modifier does not create one.
func ownerButStartsClause(text, lower string, at, i, end int) bool {
	suffix := strings.TrimSpace(lower[i+3 : end])
	if suffix == "" {
		return true
	}
	before := strings.Fields(lower[at:i])
	local := ""
	if len(before) > 0 {
		index := len(before) - 1
		local = strings.Trim(before[index], ",:")
		if local == "else" && index > 0 {
			index--
			local = strings.Trim(before[index], ",:")
		}
		if local == "one" && index > 0 && strings.Trim(before[index-1], ",:") == "no" {
			local = "no one"
		}
	}
	switch local {
	case "nothing", "anything", "everything", "something",
		"everyone", "anyone", "someone", "everybody", "anybody", "somebody", "nobody", "none", "noone", "no one", "all", "both":
		// A local exception phrase; a finite clause below can still make it a contrast.
	default:
		return true
	}
	words := strings.Fields(suffix)
	if len(words) == 0 {
		return true
	}
	properSubject := false
	if len(text) == len(lower) { // equal byte lengths keep the case-folded offsets aligned
		original := strings.Fields(strings.TrimSpace(text[i+3 : end]))
		properSubject = len(original) == len(words) && len(original) > 0 && original[0] != strings.ToLower(original[0])
	}
	return ownerFiniteClauseShape(words, properSubject)
}

// ownerFiniteClauseShape looks for a predicate after the subject, rather than treating every inflected token as a
// verb. The subject head survives intervening modifiers and may be a pronoun or a nominal. Non-finite be forms do not
// establish a predicate, and final verbs follow the same agreement rules as verbs with an object.
func ownerFiniteClauseShape(words []string, properSubject bool) bool {
	if len(words) == 0 {
		return false
	}
	start := 0
	subject := words[0]
	trackHead := true
	bareNominal := false
	switch words[0] {
	case "the", "a", "an":
		if len(words) < 3 {
			return false
		}
		subject = words[1]
		start = 2
	case "i", "you", "we", "he", "she", "they", "it", "this", "that", "these", "those":
		trackHead = false
		start = 1
	default:
		if len(words) < 2 {
			return false
		}
		bareNominal = true
		start = 1
	}
	for i := start; i < len(words); i++ {
		word := words[i]
		if i < len(words)-1 {
			if !ownerPostmodifiedNoun(words, i, subject) && ownerLexicalVerb(word) && ownerPredicateAgrees(subject, word) &&
				(!bareNominal || properSubject || (ownerBarePluralSubject(subject) && !ownerThirdPersonInflection(word))) {
				return true
			}
			if trackHead && !ownerAdverb(word) && !ownerNonFiniteModifier(word) {
				subject = word
			}
		}
		if i == len(words)-1 {
			// A lowercase bare nominal needs a base-form predicate to prove a plural-subject clause
			// ("owners need approval"). Capitalized proper nouns also accept an inflected predicate.
			if ownerFinalIntransitive(word) && ownerPredicateAgrees(subject, word) &&
				(!bareNominal || properSubject || (ownerBarePluralSubject(subject) && !ownerThirdPersonInflection(word))) {
				return true
			}
			if ownerBaseIntransitive(word) && ownerBaseSubject(subject) &&
				(!bareNominal || properSubject || ownerBarePluralSubject(subject)) {
				return true
			}
		}
	}
	return false
}

func ownerAdverb(word string) bool {
	switch word {
	case "still", "also", "already", "just", "now", "then", "often", "never", "always", "sometimes", "usually":
		return true
	}
	return strings.HasSuffix(word, "ly")
}

func ownerNonFiniteModifier(word string) bool {
	switch word {
	case "be", "been", "being":
		return true
	}
	return strings.HasSuffix(word, "ed") || strings.HasSuffix(word, "ing")
}

// ownerPostmodifiedNoun keeps an s-final noun head with a following prepositional modifier in the noun phrase.
func ownerPostmodifiedNoun(words []string, i int, subject string) bool {
	if !ownerPluralNoun(subject) || i+1 >= len(words) {
		return false
	}
	switch words[i+1] {
	case "in", "on", "at", "for", "with", "without", "under", "over", "before", "after", "during", "by", "from", "to", "of":
		return true
	}
	return false
}

func ownerLexicalVerb(word string) bool {
	if ownerFiniteInflection(word) {
		return true
	}
	switch word {
	case "need", "wait", "fail", "work", "remain", "stay", "go", "come", "run", "stop", "start", "finish",
		"complete", "continue", "approve", "reject", "roll", "notify", "check", "verify", "decide", "determine",
		"review", "ask", "inspect", "assess", "require", "block", "allow", "depend", "expect", "include", "contain",
		"show", "say", "tell", "report", "return", "look", "seem", "appear", "make", "take", "use", "get", "give",
		"want", "read", "succeed", "deploy", "merge", "publish", "ship", "restart", "retry", "cancel", "delete",
		"create", "write", "update", "edit", "fix", "build", "test", "execute", "install", "remove", "send", "save",
		"commit", "push", "pull", "open", "close":
		return true
	}
	return false
}

func ownerFinalIntransitive(word string) bool {
	switch word {
	case "am", "is", "are", "was", "were",
		"do", "does", "did", "have", "has", "had", "can", "could", "will", "would", "shall", "should", "may", "might", "must",
		"waits", "fails", "remains", "stays", "goes", "comes", "runs", "stops", "starts", "finishes", "completes",
		"continues", "rolls", "depends", "looks", "seems", "appears", "succeeds":
		return true
	}
	return false
}

func ownerBaseIntransitive(word string) bool {
	switch word {
	case "wait", "fail", "remain", "stay", "go", "come", "run", "stop", "start", "finish", "complete", "continue",
		"roll", "depend", "look", "seem", "appear", "succeed":
		return true
	}
	return false
}

func ownerPluralNoun(word string) bool {
	return strings.HasSuffix(word, "s") && !strings.HasSuffix(word, "ss") && word != "this" && word != "is"
}

// ownerBaseSubject reports a subject that takes a base-form lexical verb: a plural noun or one of the pronouns that
// uses base-form agreement. Singular nouns and this/that/it take an inflected verb instead.
func ownerBaseSubject(word string) bool {
	switch word {
	case "i", "you", "we", "they", "these", "those":
		return true
	}
	return ownerPluralNoun(word)
}

func ownerBasePronoun(word string) bool {
	switch word {
	case "i", "you", "we", "they", "these", "those":
		return true
	}
	return false
}

// ownerBarePluralSubject is deliberately narrower than ownerBaseSubject: in a bare exception phrase, an -ous
// adjective or a possessive pronoun is a modifier, not evidence of a plural clause subject.
func ownerBarePluralSubject(word string) bool {
	if strings.HasSuffix(word, "ous") {
		return false
	}
	switch word {
	case "his", "its", "ours", "yours", "theirs", "hers":
		return false
	}
	return ownerPluralNoun(word)
}

func ownerAuxiliary(word string) bool {
	switch word {
	case "am", "is", "are", "was", "were",
		"do", "does", "did", "have", "has", "had", "can", "could", "will", "would", "shall", "should", "may", "might", "must":
		return true
	}
	return false
}

// ownerPredicateAgrees enforces pronoun agreement strictly. Nominal number is only evidence for a base-form verb;
// an inflected verb instead disambiguates singular s-final nouns such as analysis, status and series.
func ownerPredicateAgrees(subject, verb string) bool {
	if ownerAuxiliary(verb) {
		switch verb {
		case "was":
			return subject == "i" || !ownerBasePronoun(subject)
		case "is", "does", "has":
			return !ownerBasePronoun(subject)
		case "are", "were", "do", "have":
			return ownerBaseSubject(subject)
		default:
			return true
		}
	}
	if ownerFiniteInflection(verb) {
		return !ownerBasePronoun(subject)
	}
	return ownerBaseSubject(subject)
}

func ownerFiniteInflection(word string) bool {
	switch word {
	case "am", "is", "are", "was", "were",
		"do", "does", "did", "have", "has", "had",
		"can", "could", "will", "would", "shall", "should", "may", "might", "must",
		"needs", "waits", "fails", "works", "remains", "stays", "goes", "comes", "runs", "stops", "starts",
		"finishes", "completes", "continues", "approves", "rejects", "rolls", "notifies", "checks", "verifies",
		"decides", "determines", "reviews", "asks", "inspects", "assesses", "requires", "blocks", "allows",
		"depends", "expects", "includes", "contains", "shows", "says", "tells", "reports", "returns", "looks",
		"seems", "appears", "makes", "takes", "uses", "gets", "gives", "wants", "reads", "succeeds":
		return true
	}
	return false
}

// ownerThirdPersonInflection excludes base auxiliaries such as have/do/are, which also occur in the finite-verb list.
func ownerThirdPersonInflection(word string) bool {
	switch word {
	case "is", "does", "has":
		return true
	}
	return ownerFiniteInflection(word) && !ownerAuxiliary(word)
}

// ownerMarkerAfterContrast reports an owner marker immediately following an unquoted `but`, with optional whitespace.
func ownerMarkerAfterContrast(text, lower string, at int) bool {
	contrasts := ownerContrastOffsets(text, lower, 0, at)
	return len(contrasts) > 0 && strings.TrimSpace(lower[contrasts[len(contrasts)-1]+3:at]) == ""
}

// ownerWithoutQuotes blanks quoted text while preserving unquoted punctuation and condition words.
func ownerWithoutQuotes(text string) string {
	straight, curly, corner := false, 0, 0
	return strings.Map(func(r rune) rune {
		switch r {
		case '"':
			straight = !straight
			return ' '
		case '“':
			curly++
			return ' '
		case '”':
			if curly > 0 {
				curly--
			}
			return ' '
		case '「':
			corner++
			return ' '
		case '」':
			if corner > 0 {
				corner--
			}
			return ' '
		}
		if straight || curly > 0 || corner > 0 {
			return ' '
		}
		return r
	}, text)
}

// ownerDecimalPoint reports a full stop between two ASCII digits, as in a version or decimal number.
func ownerDecimalPoint(text string, at int) bool {
	return at > 0 && at+1 < len(text) && text[at] == '.' &&
		text[at-1] >= '0' && text[at-1] <= '9' && text[at+1] >= '0' && text[at+1] <= '9'
}

// ownerPreviousSentenceEnd returns the last sentence terminator before the clause, ignoring decimal points.
func ownerPreviousSentenceEnd(text string) int {
	for i := len(text); i > 0; {
		r, size := utf8.DecodeLastRuneInString(text[:i])
		i -= size
		if strings.ContainsRune(ownerSentenceEnds, r) && !ownerDecimalPoint(text, i) {
			return i
		}
	}
	return -1
}

// ownerSentenceEnd returns the first unquoted sentence terminator at or after at, preserving original byte offsets.
func ownerSentenceEnd(lower string, at int) int {
	straight, curly, corner := false, 0, 0
	for i := at; i < len(lower); {
		r, size := utf8.DecodeRuneInString(lower[i:])
		switch r {
		case '"':
			straight = !straight
		case '“':
			curly++
		case '”':
			if curly > 0 {
				curly--
			}
		case '「':
			corner++
		case '」':
			if corner > 0 {
				corner--
			}
		default:
			if !straight && curly == 0 && corner == 0 && strings.ContainsRune(ownerSentenceEnds, r) && !ownerDecimalPoint(lower, i) {
				return i
			}
		}
		i += size
	}
	return len(lower)
}

var (
	ownerMarkdownListPrefix = lazyre.New(`^\s*(?:[-+*]\s+|\d{1,9}[.)]\s+)`)
	ownerMarkdownEmphasis   = strings.NewReplacer("**", "", "__", "", "*", "", "_", "")
)

// ownerMarkdownLine removes presentational list and emphasis markers before the same sentence-boundary analysis runs.
// It does not strip block quotes: those quote somebody else's words rather than presenting a new owner requirement.
func ownerMarkdownLine(text string) string {
	text = ownerMarkdownListPrefix.ReplaceAllString(strings.TrimSpace(text), "")
	return ownerMarkdownEmphasis.Replace(text)
}

// directOwnerMarker reports whether any unquoted occurrence of marker starts a direct requirement at the line or a
// sentence/clause boundary. Subordinate and narrative uses ("if …", "evaluates whether …") are not owner actions;
// skipping one such occurrence must not suppress a later direct one. A comma inside that same if-sentence does not end
// its conditional scope; a new sentence or an independent "otherwise" clause does (#5732).
func directOwnerMarker(text, marker string) bool {
	text = ownerMarkdownLine(text)
	lower := strings.ToLower(text)
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], marker)
		if i < 0 {
			return false
		}
		i += from
		before := strings.TrimSpace(lower[:i])
		afterContrast := ownerMarkerAfterContrast(text, lower, i)
		direct := before == "" || before == "otherwise" || strings.HasSuffix(before, " otherwise") || afterContrast
		terminatorBefore := strings.TrimRight(before, "\"”」") // i18n-ignore: parser delimiters, not display copy; a closing quote does not hide the sentence's own terminator
		terminator, _ := utf8.DecodeLastRuneInString(terminatorBefore)
		direct = direct || strings.ContainsRune(ownerSentenceEnds, terminator)
		if afterContrast {
			clause := ownerWithoutQuotes(before)
			end := ownerPreviousSentenceEnd(clause)
			// A contrast does not close a leading if-sentence. A trailing condition, by contrast, owns only its own
			// clause, so the marker after its `but` remains an independent requirement.
			direct = !ownerConditionalOpen(strings.TrimSpace(clause[end+1:]))
		} else if strings.HasSuffix(before, ",") || strings.HasSuffix(before, ":") {
			clause := ownerWithoutQuotes(before)
			end := ownerPreviousSentenceEnd(clause)
			conditional := strings.TrimSpace(clause[end+1:])
			// Commas and colons introduce guidance without closing an "if" elsewhere in this sentence. Only a
			// sentence boundary or an explicit "otherwise" clause makes the next requirement independent.
			openConditional := ownerConditionalOpen(conditional)
			direct = !openConditional || strings.HasSuffix(clause, "otherwise,") || strings.HasSuffix(clause, "otherwise:")
		}
		rest := i + len(marker)
		if direct && !ownerNoAction(lower[rest:]) && !ownerTrailingConditional(text, lower, rest) && !ownerMarkerQuoted(lower, i) {
			return true
		}
		from = i + len(marker)
	}
	return false
}

// ownerConditionalOpen reports a clause-level "if" or "only if" whose condition still owns the requirement. An
// introductory colon or short discourse marker may precede it; a narrative verb's nested if-question ("checks if",
// "checks only if") may not suppress the following direct clause. "Even if" is different: its main clause applies
// regardless of the condition, so a later owner requirement stays direct.
func ownerConditionalOpen(clause string) bool {
	for from := 0; from < len(clause); {
		i := strings.Index(clause[from:], "if ")
		if i < 0 {
			return false
		}
		i += from
		prefix := strings.TrimSpace(clause[:i])
		boundary := prefix == "" || prefix == "only" || strings.HasSuffix(prefix, ":") || strings.HasSuffix(prefix, ": only")
		intro := strings.TrimSpace(strings.TrimSuffix(prefix, ","))
		switch intro {
		case "so", "then", "therefore", "and", "but", "now", "next":
			boundary = true
		}
		if boundary && !strings.HasSuffix(prefix, "even") {
			return true
		}
		from = i + 2
	}
	return false
}

// ownerTrailingConditional reports an if/only-if condition attached to the requirement starting at at. Its scope ends
// at the sentence boundary, so it cannot hide a later independent requirement. An if-question that is the
// requirement's own object ("check if …") is not a condition, and "even if" leaves the requirement unconditional;
// either owns later coordinated if-clauses. Quoted if-clauses are history, not conditions on an unquoted requirement.
func ownerTrailingConditional(text, lower string, at int) bool {
	end := ownerSentenceEnd(lower, at)
	const marker = " if "
	for from := at; from < end; {
		i := strings.Index(lower[from:end], marker)
		if i < 0 {
			return false
		}
		i += from
		predicateText := lower[at:i]
		predicate := strings.TrimSpace(predicateText)
		conditionEnd := end
		if contrasts := ownerContrastOffsets(text, lower, i+len(marker), end); len(contrasts) > 0 {
			conditionEnd = contrasts[0]
		}
		condition := strings.TrimSpace(lower[i+len(marker) : conditionEnd])
		if predicate != "" && condition != "" && !ownerMarkerQuoted(lower, i) {
			contrasts := ownerContrastOffsets(text, lower, at, i)
			if len(contrasts) > 0 {
				last := contrasts[len(contrasts)-1]
				if len(contrasts) > 1 || strings.TrimSpace(lower[last+3:i]) != "only" {
					return false // the condition belongs to a contrasted clause, not an earlier requirement
				}
			}
			words := strings.Fields(predicate)
			last := words[len(words)-1]
			if last == "even" {
				return false
			}
			if ownerIfQuestionPredicate(predicate) {
				return false
			}
			return true
		}
		from = i + len(marker)
	}
	return false
}

// ownerIfQuestionPredicate reports a predicate made only of interrogative verbs and their modifiers. The last
// non-modifier must be a verb whose if-clause is its object; an object noun ("add a test", "check the test") is not
// such a verb. Modifiers may appear before or after the governing verb.
func ownerIfQuestionPredicate(predicate string) bool {
	words := strings.Fields(predicate)
	if len(words) == 0 {
		return false
	}
	verb := func(word string) bool {
		switch word {
		case "check", "decide", "determine", "verify", "confirm", "evaluate", "test", "see", "review", "ask", "inspect", "assess":
			return true
		}
		return false
	}
	modifier := func(word string) bool {
		switch word {
		case "and", "or", "also", "carefully", "quickly", "first", "then", "now", "just", "please", "again", "manually", "explicitly", "thoroughly", "immediately":
			return true
		}
		return false
	}
	lastVerb := false
	for i := len(words) - 1; i >= 0; i-- {
		if modifier(words[i]) {
			continue
		}
		if !verb(words[i]) {
			return false
		}
		lastVerb = true
		break
	}
	if !lastVerb {
		return false
	}
	for _, word := range words {
		if !verb(word) && !modifier(word) {
			return false
		}
	}
	return true
}

// ownerOfferMarker reports an unquoted, unresolved occurrence of an English offer/question. Resolved narration is
// scoped to its own sentence, so a genuine later offer remains an owner action (#5732).
func ownerOfferMarker(lower, marker string) bool {
	for from := 0; from < len(lower); {
		i := strings.Index(lower[from:], marker)
		if i < 0 {
			return false
		}
		i += from
		start := strings.LastIndexAny(lower[:i], ownerSentenceEnds) + 1
		end := len(lower)
		if j := strings.IndexAny(lower[i+len(marker):], ownerSentenceEnds); j >= 0 {
			end = i + len(marker) + j
		}
		sentence := lower[start:end]
		resolved := false
		for _, phrase := range []string{
			"i answered", "i've answered", "i have answered", "already answered", "was answered", "has been answered",
			"already resolved", "was resolved", "has been resolved", "no further action is needed", "no further action needed",
			"nothing further is needed",
		} {
			resolved = resolved || strings.Contains(sentence, phrase)
		}
		if !resolved && !ownerMarkerQuoted(lower, i) {
			return true
		}
		from = i + len(marker)
	}
	return false
}

// ownerChineseOfferMarker reports an unquoted, unnegated, unresolved occurrence of a Chinese choice question.
// Each occurrence needs its own question mark. Negation and resolved narration belong only to that occurrence, so they
// cannot hide a later question or turn an earlier negated occurrence into a request (#5732).
func ownerChineseOfferMarker(text, marker string, negations []string) bool {
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], marker)
		if i < 0 {
			return false
		}
		i += from
		prefix := text[:i]
		negated := false
		for _, n := range negations {
			lead := strings.TrimSuffix(n, marker)
			if lead != n && strings.HasSuffix(prefix, lead) {
				negated = true
				break
			}
		}
		start := strings.LastIndexAny(prefix, "。！？；.!?;，,：:") + 1 // i18n-ignore: clause punctuation for parser markers
		resolved := false
		for _, phrase := range []string{
			"我回答了", "我已回答", "已经回答", "已回答", "已经解决", "已解决", // i18n-ignore: parser markers, not display copy
			"无需进一步操作", "不需要进一步操作", "不用进一步操作", "不必进一步操作", // i18n-ignore: parser markers, not display copy
		} { // i18n-ignore: parser markers, not display copy
			resolved = resolved || strings.Contains(prefix[start:], phrase)
		}
		// A resolution after the question must be its immediate, explicitly related explanation. Neither another
		// topic's no-action note nor a later sentence can resolve this occurrence.
		end := len(text)
		if next := strings.Index(text[i+len(marker):], marker); next >= 0 {
			end = i + len(marker) + next
		}
		tail := text[i+len(marker) : end]
		question := strings.IndexAny(tail, "？?") // i18n-ignore: question punctuation for parser markers
		if !resolved && question >= 0 {
			_, width := utf8.DecodeRuneInString(tail[question:])
			following := tail[question+width:]
			if stop := strings.IndexAny(following, "。！？；.!?;"); stop >= 0 { // i18n-ignore: sentence punctuation for parser markers
				following = following[:stop]
			}
			resolved = ownerResolutionSuffix(following)
		}
		if question >= 0 && !negated && !resolved && !ownerMarkerQuoted(text, i) {
			return true
		}
		from = i + len(marker)
	}
	return false
}

// ownerChineseConditionalPrefix reports whether the current clause is still conditional. A full stop or semicolon
// starts a new clause; an independent instruction there is not hidden by an earlier condition (#5732).
func ownerChineseConditionalPrefix(prefix string) bool {
	start := 0
	for i, r := range prefix {
		boundary := r == '\n' || strings.ContainsRune(ownerSentenceEnds, r)
		// A decimal point inside a version or number is not a sentence boundary; an ASCII stop between clauses is.
		if r == '.' && i > 0 && i+1 < len(prefix) &&
			prefix[i-1] >= '0' && prefix[i-1] <= '9' && prefix[i+1] >= '0' && prefix[i+1] <= '9' {
			boundary = false
		}
		if boundary {
			start = i + utf8.RuneLen(r)
		}
	}
	clause := prefix[start:]
	for _, word := range []string{"如果", "假如", "倘若", "若"} { // i18n-ignore: parser markers, not display copy
		for end := len(clause); end > 0; {
			i := strings.LastIndex(clause[:end], word)
			if i < 0 {
				break
			}
			if word == "若" && strings.HasPrefix(clause[i+len(word):], "干") { // i18n-ignore: 若干 is a quantifier, not conditional 若
				end = i
				continue
			}
			lead := strings.TrimSpace(clause[:i])
			if lead == "" || strings.HasSuffix(lead, "：") || strings.HasSuffix(lead, ":") || // i18n-ignore: parser punctuation
				strings.HasSuffix(lead, "，") || strings.HasSuffix(lead, ",") { // i18n-ignore: parser punctuation
				return true
			}
			break
		}
	}
	return false
}

// ownerChineseDirectMarker reports an unquoted, unnegated direct instruction. Quoting or negating one occurrence does
// not hide a later genuine requirement, and a historical quoted instruction is not a new owner action (#5732).
func ownerChineseDirectMarker(text, marker string, negations []string) bool {
	for from := 0; from < len(text); {
		i := strings.Index(text[from:], marker)
		if i < 0 {
			return false
		}
		i += from
		prefix := text[:i]
		negated := false
		for _, n := range negations {
			lead := strings.TrimSuffix(n, marker)
			if lead != n && strings.HasSuffix(prefix, lead) {
				negated = true
				break
			}
		}
		if !negated && !strings.HasSuffix(prefix, "是否") && !ownerChineseConditionalPrefix(prefix) && !ownerMarkerQuoted(text, i) { // i18n-ignore: parser marker, not display copy
			return true
		}
		from = i + len(marker)
	}
	return false
}

// ownerResolutionSuffix recognizes only an immediate explanation tied to this question, not any later "answered" or
// "no action" words. A following comma/colon lets the explanation continue; "的是上一个问题" is another topic instead.
func ownerResolutionSuffix(clause string) bool {
	for _, phrase := range []string{
		"这个问题已回答", "这个问题已经回答", "该问题已回答", "该问题已经回答", "此问题已回答", "此问题已经回答", // i18n-ignore: parser markers, not display copy
		"这个问题已解决", "这个问题已经解决", "该问题已解决", "该问题已经解决", "此问题已解决", "此问题已经解决", // i18n-ignore: parser markers, not display copy
		"无需进一步操作", "不需要进一步操作", "不用进一步操作", "不必进一步操作", // i18n-ignore: parser markers, not display copy
	} { // i18n-ignore: parser markers, not display copy
		if !strings.HasPrefix(clause, phrase) {
			continue
		}
		rest := clause[len(phrase):]
		if rest == "" || strings.HasPrefix(rest, "，") || strings.HasPrefix(rest, ",") || // i18n-ignore: parser punctuation
			strings.HasPrefix(rest, "：") || strings.HasPrefix(rest, ":") { // i18n-ignore: parser punctuation
			return true
		}
	}
	return false
}

// ownerMarkerQuoted reports whether byte offset at is inside straight double quotes, Chinese curly quotes or corner brackets.
func ownerMarkerQuoted(text string, at int) bool {
	straight, curly, corner := false, 0, 0
	for i, r := range text {
		if i >= at {
			break
		}
		switch r {
		case '"':
			straight = !straight
		case '“':
			curly++
		case '”':
			if curly > 0 {
				curly--
			}
		case '「':
			corner++
		case '」':
			if corner > 0 {
				corner--
			}
		}
	}
	return straight || curly > 0 || corner > 0
}

// ActionLine is a final reply's explicit request for the owner to act. The judge still decides the task as a
// whole; these unambiguous offers and hand-offs are a safety net against calling the work done (#5732). Negated forms
// are informational, not a request.
func ActionLine(text string) string {
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		compact := strings.Join(strings.Fields(ownerMarkdownLine(line)), "")
		for _, p := range []struct {
			marker    string
			negations []string
		}{
			{"需要你本人操作", []string{"不需要你本人操作", "无需你本人操作", "不用你本人操作", "不必你本人操作"}}, // i18n-ignore: parser markers, not display copy
			{"由你发起", []string{"不需要由你发起", "无需由你发起", "不用由你发起", "不必由你发起"}},        // i18n-ignore: parser markers, not display copy
			{"要不要", []string{"不需要你决定要不要", "无需你决定要不要", "不用你决定要不要", "不是要不要"}},    // i18n-ignore: parser markers, not display copy
		} {
			if !strings.Contains(compact, p.marker) {
				continue
			}
			if p.marker == "要不要" && !strings.ContainsAny(line, "？?") { // i18n-ignore: parser markers, not display copy
				continue // evaluating whether to do something is narrative, not asking the owner to choose (#5732)
			}
			if p.marker == "要不要" { // i18n-ignore: parser markers, not display copy
				if ownerChineseOfferMarker(compact, p.marker, p.negations) {
					return line
				}
				continue
			}
			if ownerChineseDirectMarker(compact, p.marker, p.negations) {
				return line
			}
		}
		lower := strings.ToLower(ownerMarkdownLine(line))
		for _, p := range []struct {
			marker    string
			negations []string
		}{
			{"you need to", nil},
			{"you'll need to", nil},
			{"would you like me to", nil},
			{"do you want me to", nil},
			{"should i proceed", nil},
			{"shall i proceed", nil},
		} {
			if !strings.Contains(lower, p.marker) {
				continue
			}
			if p.marker == "you need to" || p.marker == "you'll need to" {
				// Negation is occurrence-local: its own prefix is not direct, and it never hides a later direct requirement.
				if directOwnerMarker(line, p.marker) {
					return line
				}
				continue
			}
			negated := false
			for _, n := range p.negations {
				negated = negated || strings.Contains(lower, n)
			}
			if !negated && ownerOfferMarker(lower, p.marker) {
				return line
			}
		}
	}
	return ""
}
