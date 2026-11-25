package extmgr

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/nwverse/persona/server/internal/assistantwire"
	"github.com/nwverse/persona/server/internal/extension"
)

// Extension context for the prompts the manager builds itself (docs/extensions/extensions.md "Assistant content from
// the data directory"): the Inbox's triage and drafts (inbox_context). Read whenever the prompt is built, so it restarts nothing and reaches every runtime alike; cut at a
// line end past its limit rather than refused, as there is no older version to keep.

const (
	// inboxContextMost is the most of the Inbox file that is used, per extension.
	inboxContextMost = 1 << 10
	// neverQuoteMost is the most of a never_quote file that is used.
	neverQuoteMost = 64 << 10
	// contextReadMost is the largest context file read at all (it is cut after cleaning); a larger one is too_large.
	contextReadMost = 256 << 10
	// neverQuoteShortest is the fewest characters a never_quote sentence must have after normalizing: shorter ones
	// ("OK", "thanks") would stop ordinary drafts.
	neverQuoteShortest = 6
)

// clipLines is s cut to at most most bytes at a line end (at a character when its first line is longer), and whether
// it was cut.
func clipLines(s string, most int) (string, bool) {
	if len(s) <= most {
		return s, false
	}
	cut := s[:most]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	} else {
		for len(cut) > 0 && !utf8.ValidString(cut) {
			cut = cut[:len(cut)-1]
		}
	}
	return strings.TrimSpace(cut), true
}

// readContextText is rel of the data directory dir, cleaned (cleanDataText) and cut at most bytes: "" when there is
// none.
func readContextText(dir, rel string, most int) (string, bool, int, error) {
	raw, err := readDataFile(dir, rel, contextReadMost)
	if err != nil {
		return "", false, len(raw), err
	}
	text, err := cleanDataText(raw)
	if err != nil {
		return "", false, len(raw), err
	}
	text, cut := clipLines(text, most)
	return text, cut, len(raw), nil
}

// inboxContext are the enabled extensions' texts for the Inbox, and the sentences a draft must not repeat (every
// extension's together, without duplicates).
func (ps *extensions) inboxContext() assistantwire.ExtensionContextResult {
	out := assistantwire.ExtensionContextResult{Blocks: []assistantwire.ExtensionContextBlock{}}
	if ps == nil {
		return out
	}
	seen := map[string]bool{}
	for _, p := range ps.reg.List() {
		if !p.Record.Enabled || p.Problem != "" {
			continue
		}
		dir := ps.reg.DataDir(p.Record.ID)
		for _, it := range p.Manifest.DataPrompts(extension.PromptInboxContext) {
			text, _, _, err := readContextText(dir, it.File, inboxContextMost)
			if err != nil {
				ps.dataLog(dataKey(p.Record.ID, it.ID), fmt.Sprintf("plugin %s: inbox context %s: not used, %v", p.Record.ID, it.File, err))
			} else if text != "" {
				out.Blocks = append(out.Blocks, assistantwire.ExtensionContextBlock{Extension: p.Record.ID, Name: p.Full.Name, Text: text})
			}
			if it.NeverQuote == "" {
				continue
			}
			lines, _, _, err := readNeverQuote(dir, it.NeverQuote)
			if err != nil {
				ps.dataLog(dataKey(p.Record.ID, it.ID+"/never_quote"), fmt.Sprintf("plugin %s: never_quote %s: not used, %v", p.Record.ID, it.NeverQuote, err))
			}
			for _, l := range lines {
				if !seen[l] {
					seen[l] = true
					out.NeverQuote = append(out.NeverQuote, l)
				}
			}
		}
	}
	return out
}

// readNeverQuote are the sentences of a never_quote file, one a line, trimmed, without empty ones; whether the file
// was cut at neverQuoteMost, and its size.
func readNeverQuote(dir, rel string) ([]string, bool, int, error) {
	text, cut, size, err := readContextText(dir, rel, neverQuoteMost)
	if err != nil {
		return nil, false, size, err
	}
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if l = strings.TrimSpace(l); l != "" && !slices.Contains(out, l) {
			out = append(out, l)
		}
	}
	return out, cut, size, nil
}

// contextViews are the status of p's Inbox context (it is on): what a prompt built now gets.
func (ps *extensions) contextViews(p *extension.Extension) []AssistantDataView {
	var out []AssistantDataView
	dir := ps.reg.DataDir(p.Record.ID)
	for _, it := range p.Manifest.DataPrompts(extension.PromptInboxContext) {
		v := AssistantDataView{Contribution: it.ID, Kind: extension.PromptInboxContext, Path: it.File, Limit: inboxContextMost, State: dataStateNone}
		text, truncated, size, err := readContextText(dir, it.File, inboxContextMost)
		v.Bytes = size
		switch {
		case err != nil:
			v.Reason, v.Detail = dataErrorCode(err), err.Error()
		case text != "":
			v.State, v.Hash, v.Truncated = dataStateAdopted, textDigest(text), truncated
			if truncated {
				v.Detail = "cut at the limit"
			}
		}
		if it.NeverQuote != "" {
			lines, truncated, _, err := readNeverQuote(dir, it.NeverQuote)
			v.NeverQuote = len(lines)
			if err != nil {
				v.Detail = joinDetail(v.Detail, "never_quote: "+err.Error())
			} else if truncated {
				v.Truncated, v.Detail = true, joinDetail(v.Detail, "never_quote cut at the limit")
			}
		}
		out = append(out, v)
	}
	return out
}

func joinDetail(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}
