package signalrule

import (
	"slices"
	"strings"
	"unicode/utf8"
)

// Text and path rules that tell whether two pieces of work are the same work (duplicate hints of task_plan).

// IssueRefs are the issue / PR numbers s names: #123, issues/123, pull/123 (three digits or more: not a list's #1).
func IssueRefs(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		var rest string
		switch {
		case s[i] == '#':
			rest = s[i+1:]
		case strings.HasPrefix(s[i:], "issues/"):
			rest = s[i+len("issues/"):]
		case strings.HasPrefix(s[i:], "pull/"):
			rest = s[i+len("pull/"):]
		default:
			continue
		}
		n := 0
		for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
			n++
		}
		if n >= 3 && n <= 7 {
			ref := "#" + rest[:n]
			if !slices.Contains(out, ref) {
				out = append(out, ref)
			}
		}
	}
	return out
}

func SharedRef(a, b []string) string {
	for _, x := range a {
		if slices.Contains(b, x) {
			return x
		}
	}
	return ""
}

// DirsOverlap: one directory is the other or inside it.
func DirsOverlap(a, b string) bool {
	a, b = strings.TrimRight(a, "/"), strings.TrimRight(b, "/")
	if a == "" || b == "" {
		return false
	}
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// TitleSimilarity is how alike two short titles are: the Dice coefficient of their character bigrams (spaces and
// punctuation dropped, letters folded), 0 to 1.
func TitleSimilarity(a, b string) float64 {
	ga, gb := bigrams(a), bigrams(b)
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	common := 0
	left := map[string]int{}
	for _, g := range gb {
		left[g]++
	}
	for _, g := range ga {
		if left[g] > 0 {
			left[g]--
			common++
		}
	}
	return 2 * float64(common) / float64(len(ga)+len(gb))
}

func bigrams(s string) []string {
	var r []rune
	for _, c := range strings.ToLower(s) {
		if c == ' ' || c == '\t' || utf8.RuneLen(c) == 1 && !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') ||
			strings.ContainsRune("，。、：；！？「」（）《》【】·…—", c) { // i18n-ignore: CJK punctuation a title is compared without
			continue
		}
		r = append(r, c)
	}
	out := make([]string, 0, len(r))
	for i := 0; i+1 < len(r); i++ {
		out = append(out, string(r[i:i+2]))
	}
	return out
}
