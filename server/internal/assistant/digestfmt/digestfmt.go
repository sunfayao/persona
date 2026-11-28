package digestfmt

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sunfayao/persona/server/internal/prompts"
)

const (
	// BlockBytes is about how long one to-do's block may be: its last words are cut to fit.
	BlockBytes = 4096
	// Budget is how long a whole message may be before the last words of every block are cut shorter (criteria are
	// never cut, so a message of many to-dos can still be longer).
	Budget = 4900
)

// Pieces is what a reader was told of one to-do: a short digest of each part of its block, by part.
type Pieces map[string]string

// Input is one message to render.
type Input struct {
	Role    string // prompts.SuperviseForMain or prompts.SuperviseForLeader
	Entries []prompts.SuperviseEntry
	// Frame is the message around the blocks (its level, round and routing lines); Entries and Blocks are not read.
	Frame prompts.Supervise
	// Told is what the reader has been told by earlier messages, by to-do id (Book.Told); nil tells every block whole.
	Told map[string]Pieces
	// Closed lists to-dos that ended since their signal was raised, with how ("accepted", "cancelled", "closed"): an
	// empty word drops the to-do from the message, a word reduces it to one line.
	Closed map[string]string
}

// Output is the message and what it tells.
type Output struct {
	Text string
	// Told is what the reader will have been told of each to-do once Text is in (whole blocks, whether or not Text
	// repeated all of them): Book.Offer keeps it for Book.Delivered.
	Told map[string]Pieces
	// Whole is every block of the message with all its parts, for a reader that lost what it had been told.
	Whole []prompts.SuperviseBlock
}

// Render renders in.
func Render(in Input) Output {
	in.Entries = Sorted(slices.Clone(in.Entries))
	whole := prompts.SuperviseBlocks(in.Entries, in.Role, in.Frame.Fresh)
	out := Output{Told: map[string]Pieces{}}
	pieces := map[string]Pieces{}
	for _, b := range whole {
		if _, closed := in.Closed[b.ID]; closed {
			continue
		}
		pieces[b.ID] = piecesOf(b)
		out.Told[b.ID] = pieces[b.ID]
		out.Whole = append(out.Whole, b)
	}
	for i := range out.Whole {
		out.Whole[i].N = i + 1
	}
	// What the reader needs, then the words of the Agents cut shorter while a block or the message is too long.
	show := func(words int) []prompts.SuperviseBlock {
		entries := in.Entries
		if words >= 0 {
			entries = slices.Clone(in.Entries)
			for i := range entries {
				entries[i].Last, entries[i].Said = tail(entries[i].Last, words), tail(entries[i].Said, words)
			}
		}
		var blocks []prompts.SuperviseBlock
		for _, b := range prompts.SuperviseBlocks(entries, in.Role, in.Frame.Fresh) {
			if word, closed := in.Closed[b.ID]; closed {
				if word != "" {
					b.Gone, b.Signals, b.Criteria, b.Last = word, nil, nil, ""
					blocks = append(blocks, b)
				}
				continue
			}
			if prev, ok := in.Told[b.ID]; ok && !in.Frame.Fresh && b.ID != "" {
				b = reduce(b, pieces[b.ID], prev)
			}
			b.Cut = words >= 0 && (len(b.Last) < len(longest(in.Entries, b.ID).Last) || b.Cut)
			blocks = append(blocks, b)
		}
		for i := range blocks {
			blocks[i].N = i + 1
		}
		return blocks
	}
	blocks := show(-1)
	for i := range blocks {
		if over := len(prompts.RenderSuperviseBlock(blocks[i])) - BlockBytes; over > 0 && blocks[i].Last != "" {
			blocks[i].Last, blocks[i].Cut = tail(blocks[i].Last, len(blocks[i].Last)-over-cutLine), true
		}
	}
	out.Text = render(in, blocks)
	for _, words := range []int{400, 300, 200, 150, 100, 60, 0} {
		if len(out.Text) <= Budget {
			break
		}
		blocks = show(words)
		for i := range blocks {
			if over := len(prompts.RenderSuperviseBlock(blocks[i])) - BlockBytes; over > 0 && blocks[i].Last != "" {
				blocks[i].Last = tail(blocks[i].Last, len(blocks[i].Last)-over-cutLine)
			}
		}
		out.Text = render(in, blocks)
	}
	return out
}

// longest is the entry of to-do id that has the longest last words, to tell whether they were cut.
func longest(entries []prompts.SuperviseEntry, id string) prompts.SuperviseEntry {
	var best prompts.SuperviseEntry
	for _, e := range entries {
		if e.ID == id && len(e.Last) >= len(best.Last) {
			best = e
		}
	}
	return best
}

func render(in Input, blocks []prompts.SuperviseBlock) string {
	if in.Role == prompts.SuperviseForLeader {
		return prompts.RenderWorkerSuperviseBlocks(blocks)
	}
	f := in.Frame
	f.Entries, f.Blocks = nil, blocks
	if f.Blocks == nil {
		f.Blocks = []prompts.SuperviseBlock{}
	}
	return prompts.RenderSupervise(f)
}

// cutLine is about what the line that says where the rest is adds to a cut block.
const cutLine = 100

// tail is the last n bytes of s (none for n <= 0) from a character's start, after an ellipsis; s when it is not longer.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 0 {
		return ""
	}
	t := s[len(s)-n:]
	for len(t) > 1 && !utf8.RuneStart(t[0]) {
		t = t[1:]
	}
	return "…" + strings.TrimSpace(t)
}

func digest(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:6])
}

// piecesOf is the digest of each part of b.
func piecesOf(b prompts.SuperviseBlock) Pieces {
	p := Pieces{}
	seen := map[string]int{}
	for _, g := range b.Signals {
		seen[g.Kind]++
		p["s"+strconv.Itoa(seen[g.Kind])+":"+g.Kind] = digest(g.What, g.Detail)
	}
	for _, c := range b.Criteria {
		p["c"+strconv.Itoa(c.N)] = digest(c.Text, c.State, c.Evidence, c.By)
	}
	p["n"] = strconv.Itoa(len(b.Criteria))
	if b.Last != "" {
		p["l"] = digest(b.Last)
	}
	return p
}

// reduce leaves in b what changed since the reader was told prev: its signals and criteria lines that are new or
// different, its last words when they are new. A block that lost or gained a criterion is told its criteria whole.
func reduce(b prompts.SuperviseBlock, now, prev Pieces) prompts.SuperviseBlock {
	left := false
	seen := map[string]int{}
	for i, g := range b.Signals {
		seen[g.Kind]++
		key := "s" + strconv.Itoa(seen[g.Kind]) + ":" + g.Kind
		if prev[key] == now[key] {
			b.Signals[i].Show, left = false, true
		}
	}
	if prev["n"] == now["n"] {
		shown := 0
		for i, c := range b.Criteria {
			if key := "c" + strconv.Itoa(c.N); prev[key] == now[key] {
				b.Criteria[i].Show, left = false, true
			} else {
				shown++
			}
		}
		b.ShowCriteria = shown > 0
		b.Partial = shown > 0 && shown < len(b.Criteria)
		if len(b.Criteria) == 0 {
			b.ShowCriteria, b.NoCriteria, left = false, false, true
		}
	}
	if b.Last != "" && prev["l"] == now["l"] {
		b.Last, left = "", true
	}
	b.Unchanged = left
	return b
}

// Book is what each conversation has been told, and the messages on their way to it. The zero value is not ready: use
// NewBook.
type Book struct {
	mu      sync.Mutex
	told    map[string]map[string]Pieces // conversation → to-do → pieces
	offers  map[string][]offer           // conversation → messages rendered for it, newest last
	sending map[string]going             // conversation → the message that is going in
}

type going struct {
	text   string
	judged bool
}

type offer struct {
	text  string
	told  map[string]Pieces
	whole []prompts.SuperviseBlock
}

// offersKept is how many rendered messages a conversation's book remembers: one is going in, a few more wait.
const offersKept = 4

func NewBook() *Book {
	return &Book{told: map[string]map[string]Pieces{}, offers: map[string][]offer{}, sending: map[string]going{}}
}

// Told is what conversation was told, to give to Render as Input.Told.
func (b *Book) Told(conversation string) map[string]Pieces {
	b.mu.Lock()
	defer b.mu.Unlock()
	return maps.Clone(b.told[conversation])
}

// Offer notes that text, as rendered, is on its way to conversation: Delivered(text) makes the reader told.
func (b *Book) Offer(conversation, text string, out Output) {
	b.mu.Lock()
	defer b.mu.Unlock()
	list := slices.DeleteFunc(b.offers[conversation], func(o offer) bool { return o.text == text })
	list = append(list, offer{text: text, told: out.Told, whole: out.Whole})
	if len(list) > offersKept {
		list = list[len(list)-offersKept:]
	}
	b.offers[conversation] = list
}

// Sending marks text as the message that is going into conversation now, until Done; judged says it asks the
// conversation to judge a signal.
func (b *Book) Sending(conversation, text string, judged bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sending[conversation] = going{text, judged}
}

// Done ends Sending; delivered says whether the message went in, and then conversation is told what it said.
func (b *Book) Done(conversation, text string, delivered bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.sending[conversation].text == text {
		delete(b.sending, conversation)
	}
	if !delivered {
		return
	}
	i := slices.IndexFunc(b.offers[conversation], func(o offer) bool { return o.text == text })
	if i < 0 {
		return
	}
	if b.told[conversation] == nil {
		b.told[conversation] = map[string]Pieces{}
	}
	maps.Copy(b.told[conversation], b.offers[conversation][i].told)
	b.offers[conversation] = slices.Delete(b.offers[conversation], i, i+1)
}

// Compacted is called when conversation was compacted or lost its context: what it was told is gone. When a message is
// going in at that moment it was rendered for a reader that still had it, so the whole blocks of that message are
// returned for the conversation to be told again with it; the message still counts as told.
func (b *Book) Compacted(conversation string) []prompts.SuperviseBlock {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.told, conversation)
	now, ok := b.sending[conversation]
	if !ok {
		return nil
	}
	for _, o := range b.offers[conversation] {
		if o.text == now.text {
			return slices.Clone(o.whole)
		}
	}
	return nil
}

// Going reports whether a supervision message is going into conversation now, and whether it asks it to judge.
func (b *Book) Going(conversation string) (going, judged bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now, going := b.sending[conversation]
	return going, now.judged
}

// Forget drops everything about conversation (it ended).
func (b *Book) Forget(conversation string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.told, conversation)
	delete(b.offers, conversation)
	delete(b.sending, conversation)
}
