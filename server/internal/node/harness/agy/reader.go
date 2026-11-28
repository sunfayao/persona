// Package agy reads Antigravity brain exports and projects them onto Persona's turn model.
package agy

import (
	"bufio"
	"container/list"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/turnmodel"
)

const TranscriptPath = ".system_generated/logs/transcript_full.jsonl"

var conversationID = lazyre.New(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Ref names a native conversation under a data root chosen by the node, never supplied by the runtime.
type Ref struct {
	Root, Conversation string
}

func (r Ref) open() (*os.Root, error) {
	if r.Root == "" || !conversationID.MatchString(r.Conversation) {
		return nil, os.ErrNotExist
	}
	root, err := os.OpenRoot(r.Root)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	path := filepath.Join("brain", r.Conversation)
	entry, err := root.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !entry.IsDir() {
		return nil, os.ErrNotExist
	}
	brain, err := root.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	info, err := brain.Stat(".")
	if err != nil || !os.SameFile(entry, info) {
		brain.Close()
		return nil, os.ErrNotExist
	}
	return brain, nil
}

type Clock interface{ Now() time.Time }

type Dependencies struct{ Clock Clock }

type Reader struct {
	clock Clock
	mu    sync.Mutex
	cache map[Ref]*list.Element
	lru   list.List
}

func New(deps Dependencies) *Reader {
	return &Reader{clock: deps.Clock, cache: make(map[Ref]*list.Element)}
}

func (ref Ref) Valid() bool {
	brain, err := ref.open()
	if err != nil {
		return false
	}
	brain.Close()
	return true
}

// Projection is immutable and shared by every page and watch of a generation.
// Callers must not mutate returned items or the data they reference.
type Projection struct {
	token, turnError   string
	turnErrorAfterWork bool
	summary            string
	summarySeq         int
	seq                int
	items              []proto.Item
	usage              agyTranscriptUsage
	row                turnmodel.Snapshot
	outputs            map[string]int
	media              map[string]agyMedia
	stepHashes         map[int][32]byte
	source             Ref
	file               os.FileInfo
	rev                int64
	kind               Kind
	invokesChild       bool
	children           map[string]bool
	firstAt            time.Time
}

func (h *Projection) History() string     { return h.token }
func (h *Projection) Seq() int            { return h.seq }
func (h *Projection) Items() []proto.Item { return h.items }
func (h *Projection) ModTime() time.Time {
	if h.file == nil {
		return time.Time{}
	}
	return h.file.ModTime()
}

// Follows reports whether h can supply send evidence after before's native cursor.
func (h *Projection) Follows(before *Projection) bool {
	return before.file == nil || h.file != nil && os.SameFile(h.file, before.file) && h.file.Size() >= before.file.Size() && h.seq >= before.seq
}

func (h *Projection) WithItems(items []proto.Item) *Projection {
	copy := *h
	copy.items = items
	return &copy
}

// Updates returns changed items at or before since on the same history, such as finished running calls.
func (h *Projection) Updates(previous *Projection, since int) []proto.Item {
	if previous == nil || previous.token != h.token || previous == h {
		return nil
	}
	var out []proto.Item
	for _, it := range h.items[:AfterIndex(h.items, since)] {
		if sum, ok := h.stepHashes[it.Seq]; ok && sum != previous.stepHashes[it.Seq] {
			out = append(out, it)
		}
	}
	return out
}

func (r *Reader) Placeholder(id, text string) *Projection {
	return &Projection{token: "agy-pane:" + id, seq: 1, items: []proto.Item{{
		ID: "agy#0", Seq: 1, Role: "system", Kind: proto.KindNotice, Text: text, TS: r.clock.Now().UTC(),
	}}}
}

func empty(ref Ref) *Projection {
	return &Projection{token: "agy-native-v1:" + ref.Conversation, source: ref}
}

type generation struct {
	ref    Ref
	file   os.FileInfo
	length int64 // Complete lines; an unfinished line is read again once the file grows.
	hash   [32]byte
	fold   agyFold
	rev    agyRevision

	overlayFile os.FileInfo
	overlay     overlay
	fork        *generation
	effective   []agyStep
	projection  *Projection
}

func (r *Reader) current(ref Ref) *generation {
	r.mu.Lock()
	defer r.mu.Unlock()
	if e := r.cache[ref]; e != nil {
		r.lru.MoveToFront(e)
		return e.Value.(*generation)
	}
	return nil
}

// commit compares generations, never file metadata. An older read racing a publication must retry from the winner.
func (r *Reader) commit(ref Ref, before, next *generation) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	e := r.cache[ref]
	if e == nil && before != nil || e != nil && e.Value != before {
		return false
	}
	if e != nil {
		e.Value = next
		r.lru.MoveToFront(e)
	} else {
		r.cache[ref] = r.lru.PushFront(next)
		// Keep both companions' recent lists and watched panes in the working set.
		if r.lru.Len() > 256 {
			last := r.lru.Back()
			delete(r.cache, last.Value.(*generation).ref)
			r.lru.Remove(last)
		}
	}
	return true
}

func sameFile(a, b os.FileInfo) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Size() == b.Size() && a.ModTime().Equal(b.ModTime()) && os.SameFile(a, b)
}

func regularStat(brain *os.Root, path string) (os.FileInfo, error) {
	info, err := brain.Lstat(path)
	if errors.Is(err, os.ErrNotExist) || err == nil && !info.Mode().IsRegular() {
		return nil, nil
	}
	return info, err
}

func (r *Reader) Read(ref Ref) (*Projection, error) {
	g, err := r.read(ref, nil)
	if g == nil {
		return empty(ref), err
	}
	return g.projection, err
}

func (r *Reader) read(ref Ref, chain []Ref) (*generation, error) {
	if len(chain) >= 16 || slices.Contains(chain, ref) {
		return nil, os.ErrInvalid
	}
	for range 4 {
		g, err := r.readOnce(ref, chain, r.current(ref))
		if !errors.Is(err, errChanged) {
			return g, err
		}
	}
	return nil, errChanged
}

func (r *Reader) readOnce(ref Ref, chain []Ref, before *generation) (*generation, error) {
	brain, err := ref.open()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer brain.Close()
	info, err := regularStat(brain, TranscriptPath)
	if err != nil {
		return nil, err
	}
	ovFile, _ := regularStat(brain, overlayPath)
	next := &generation{ref: ref, file: info, overlayFile: ovFile}
	unchanged := before != nil && sameFile(info, before.file)
	switch {
	case unchanged:
		next.length, next.hash, next.fold, next.rev = before.length, before.hash, before.fold, before.rev
	case info != nil:
		if err := next.parse(brain, before); err != nil {
			if errors.Is(err, errChanged) || errors.Is(err, os.ErrNotExist) {
				return nil, errChanged
			}
			return nil, err
		}
	}
	sameOverlay := before != nil && sameFile(ovFile, before.overlayFile)
	if sameOverlay {
		next.overlay = before.overlay
	} else if ovFile != nil {
		next.overlay = readOverlay(brain, ovFile)
	}
	ov := next.overlay
	if ov.Fork != nil && len(next.fold.steps) == 0 {
		source := Ref{ref.Root, ov.Fork.Source}
		next.fork, _ = r.read(source, append(chain, ref))
	}
	if unchanged && sameOverlay && next.fork == before.fork {
		return before, nil
	}
	source, sourceInfo := ref, info
	steps := next.fold.steps
	if next.fork != nil && len(next.fork.effective) > 0 {
		source = next.fork.projection.source
		sourceInfo = next.fork.projection.file
		i, _ := slices.BinarySearchFunc(next.fork.effective, *ov.Fork.Through+1, compareStep)
		for i < len(next.fork.effective) && next.fork.effective[i].seq == *ov.Fork.Through+1 {
			i++
		}
		steps = next.fork.effective[:i]
		if len(steps) == 0 {
			ov.Fork = nil
		}
	} else {
		ov.Fork = nil
	}
	effective, ovHash := applyOverlay(steps, ov)
	next.effective = effective
	h := empty(ref)
	h.source, h.file = source, sourceInfo
	h.kind = classify(next.fold.steps, info)
	fallback := time.Time{}
	if sourceInfo != nil {
		fallback = sourceInfo.ModTime().UTC()
	}
	if next.rev.branch != "" || next.rev.native != "" || next.rev.rewrite != "" {
		h.token += ":" + next.rev.branch + ":" + next.rev.native + ":" + next.rev.rewrite
	}
	if ovHash != "" {
		h.token += ":overlay:" + ovHash
		if ov.Fork != nil {
			h.token += ":" + agyDigest(next.fork.projection.token)
		}
	}
	if info != nil || ovHash != "" || before != nil && before.projection.rev != 0 {
		// Also distinct from old companion step counts. The durable store rejects a decreasing
		// revision within one history, so do not use a hash or a file size for this clock.
		micros := fallback.UnixMicro()
		if ovFile != nil {
			micros = max(micros, ovFile.ModTime().UnixMicro())
		}
		h.rev = (1 << 51) + max(0, micros)
		if before != nil && before.projection.token == h.token {
			h.rev = max(h.rev, before.projection.rev+1)
		}
	}
	h.mapSteps(next.effective, fallback)
	next.projection = h
	current, err := regularStat(brain, TranscriptPath)
	if err != nil {
		return nil, err
	}
	currentOverlay, _ := regularStat(brain, overlayPath)
	if sameFile(info, current) && sameFile(ovFile, currentOverlay) && r.commit(ref, before, next) {
		return next, nil
	}
	return nil, errChanged
}

var errChanged = errors.New("conversation export changed during read")

func (g *generation) parse(brain *os.Root, before *generation) error {
	file, err := openRootFile(brain, TranscriptPath)
	if err != nil {
		return err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !sameFile(g.file, opened) {
		return errChanged
	}
	g.fold.conversation = g.ref.Conversation
	hash := sha256.New()
	var appendOnly bool
	if before != nil && before.file != nil && os.SameFile(g.file, before.file) && g.file.Size() > before.file.Size() {
		if _, err := io.CopyN(hash, file, before.length); err != nil {
			return errChanged
		}
		if [32]byte(hash.Sum(nil)) == before.hash {
			g.fold, g.length = before.fold, before.length
			g.fold.steps = slices.Clone(g.fold.steps)
			appendOnly = true
		}
	}
	if !appendOnly {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		hash.Reset()
	}
	lines := bufio.NewReaderSize(io.LimitReader(file, g.file.Size()-g.length), 64<<10)
	for {
		line, err := lines.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			prefix := slices.Clone(line)
			var tail []byte
			tail, err = lines.ReadBytes('\n')
			line = append(prefix, tail...)
		}
		if errors.Is(err, io.EOF) {
			break // Only consume complete records.
		}
		if err != nil {
			return err
		}
		hash.Write(line)
		g.length += int64(len(line))
		g.fold.add(line)
	}
	g.hash = [32]byte(hash.Sum(nil))
	g.rev = g.fold.revision(g.length, g.hash)
	if before != nil && before.rev.branch == g.rev.branch {
		g.rev.rewrite = before.rev.rewrite
		if !appendOnly && agyStepsRewritten(before.rev.steps, g.rev.steps) {
			g.rev.rewrite = fmt.Sprintf("%x", g.hash[:10])
		}
	}
	return nil
}

func compareStep(s agyStep, seq int) int { return s.seq - seq }

type Kind uint8

const (
	Missing Kind = iota
	Unreadable
	Empty
	Normal
	Subagent
)

func classify(steps []agyStep, info os.FileInfo) Kind {
	for _, step := range steps {
		if step.Type == "" && step.Source == "" {
			continue
		}
		switch step.Type {
		case "SYSTEM_MESSAGE", "CORTEX_STEP_TYPE_SYSTEM_MESSAGE":
			return Subagent
		case "USER_INPUT":
			if step.Source != "" && step.Source != "USER_EXPLICIT" && step.Source != "SYSTEM" {
				return Subagent
			}
		}
		return Normal
	}
	if info != nil {
		return Empty
	}
	return Missing
}

func (h *Projection) Kind() Kind                      { return h.kind }
func (h *Projection) InvokesChild() bool              { return h.invokesChild }
func (h *Projection) HasChild(id string) bool         { return h.children[id] }
func (h *Projection) FirstInput() (string, time.Time) { return h.usage.first, h.firstAt }

func (h *Projection) Compaction() (string, int) { return h.summary, h.summarySeq }
