package transcript

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"hash/crc32"
	"slices"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// IndexedSource reads a consistent transaction of a native timeline and reports its storage revision.
type IndexedSource struct {
	Revision func() string
	Read     func() ([]CodexRecord, error)
}

type indexedHistory struct {
	identity  string
	token     string
	source    IndexedSource
	revision  string
	read      bool
	digests   [][32]byte
	full      map[string]proto.Item
	pending   []CodexRecord
	pendingAt string
	failedAt  time.Time
	failedRev string
	failure   error
}

var ErrEmptyIndexedHistory = errors.New("indexed history has no readable records")

func (h *indexedHistory) records(revision string) ([]CodexRecord, error) {
	if h.pending != nil && revision == h.pendingAt {
		return h.pending, nil
	}
	if h.failure != nil && revision == h.failedRev && time.Since(h.failedAt) < 5*time.Minute {
		return nil, h.failure
	}
	records, err := h.source.Read()
	h.failedAt, h.failedRev, h.failure = time.Now(), revision, err
	return records, err
}

// NewIndexedFile provides the transcript paging interface for a source without an on-disk JSONL file.
func NewIndexedFile(identity string, source IndexedSource, sources Sources) *File {
	return &File{
		Path: identity, sources: sources, firstHash: crc32.ChecksumIEEE([]byte(identity)),
		indexed: &indexedHistory{identity: identity, source: source}, meta: Meta{Codex: true, RolloutTools: sources.RolloutTools},
	}
}

// Indexed reports whether the source is an indexed timeline.
func (f *File) Indexed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.indexed != nil
}

// BindIndexed moves an open rollout reader onto SQLite when the CLI migrates its history.
func (f *File) BindIndexed(identity string, source IndexedSource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.indexed != nil && f.Path == identity {
		f.indexed.source = source
		return f.indexed.prefetch(f.lines)
	}
	if f.migration == nil || f.migration.identity != identity {
		f.migration = &indexedHistory{identity: identity}
	}
	next := f.migration
	next.source = source
	if err := next.prefetch(0); err != nil {
		return err
	}
	f.Path, f.firstHash = identity, crc32.ChecksumIEEE([]byte(identity))
	f.indexed, f.migration = next, nil
	f.gen++
	f.offset, f.lines, f.items, f.refs, f.tree = 0, 0, nil, nil, nil
	f.base, f.win, f.partial, f.partialMeta = 0, nil, nil, false
	f.meta = Meta{Codex: true, RolloutTools: f.sources.RolloutTools}
	return nil
}

// PrefetchIndexed validates and caches the next SQLite snapshot for RefreshSpan.
func (f *File) PrefetchIndexed() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.indexed.prefetch(f.lines)
}

func (state *indexedHistory) prefetch(lines int) error {
	revision := state.source.Revision()
	if state.read && revision == state.revision {
		if lines == 0 {
			return ErrEmptyIndexedHistory
		}
		return nil
	}
	records, err := state.records(revision)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return ErrEmptyIndexedHistory
	}
	state.pending, state.pendingAt = records, revision
	return nil
}

func (f *File) refreshIndexed() ([]proto.Item, Span, error) {
	state := f.indexed
	revision := state.source.Revision()
	if state.read && revision == state.revision {
		return nil, Span{History: f.history(), From: f.lines, To: f.lines}, nil
	}
	records, err := state.records(revision)
	if err != nil {
		return nil, Span{}, err
	}
	state.pending = nil
	digests := make([][32]byte, len(records))
	for i, record := range records {
		hash := sha256.New()
		_, _ = hash.Write([]byte(record.Type))
		_, _ = hash.Write(record.Timestamp.AppendFormat(nil, "2006-01-02T15:04:05.999999999Z07:00"))
		_, _ = hash.Write(record.Payload)
		copy(digests[i][:], hash.Sum(nil))
	}
	replaced := len(digests) < len(state.digests) || !slices.Equal(digests[:len(state.digests)], state.digests)
	if !state.read || replaced {
		// Derive a cold cursor from the whole snapshot. An unchanged reopened history keeps
		// its cursor; an unknown revision forces a recent page instead of skipping a rewind.
		hash := sha256.New()
		_, _ = hash.Write([]byte(state.identity))
		for _, digest := range digests {
			_, _ = hash.Write(digest[:])
		}
		state.token = "indexed-" + hex.EncodeToString(hash.Sum(nil)[:16])
	}
	if replaced {
		f.gen++
		f.lines, f.items = 0, nil
		f.meta = Meta{Codex: true, RolloutTools: f.sources.RolloutTools}
		state.full = nil
	}
	from := f.lines
	var fresh []proto.Item
	for _, record := range records[from:] {
		f.lines++
		f.meta.facetAgent = f.sources.Agent
		items := finishParsedItems(parseCodexRecord(f.lines, record, &f.meta), record.Payload, &f.meta)
		for _, item := range items {
			sent, _ := light(item, true)
			kept, cut := light(item, false)
			fresh = append(fresh, sent)
			f.items = append(f.items, kept)
			if cut {
				if state.full == nil {
					state.full = make(map[string]proto.Item)
				}
				state.full[item.ID] = item
			}
		}
	}
	state.revision, state.digests, state.read = revision, digests, true
	return fresh, Span{History: f.history(), From: from, To: f.lines}, nil
}

// ProjectCodexRecord maps a completed record for a live overlay without writing a transcript.
func ProjectCodexRecord(seq int, record CodexRecord, meta *Meta) []proto.Item {
	meta.Codex = true
	return finishParsedItems(parseCodexRecord(seq, record, meta), record.Payload, meta)
}
