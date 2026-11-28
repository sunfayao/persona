package agy

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"slices"
	"time"
	"unicode/utf8"
)

const overlayPath = ".persona-overlay.json"
const maxOverlayBytes = 1 << 20

type overlay struct {
	Version int             `json:"version"`
	Cut     *overlayCut     `json:"cut,omitempty"`
	Hidden  []overlayStep   `json:"hidden,omitempty"`
	Notices []overlayNotice `json:"notices,omitempty"`
	Errors  []overlayError  `json:"errors,omitempty"`
	Fork    *overlayFork    `json:"fork,omitempty"`
}

type overlayStep struct {
	Index *int   `json:"step_index"`
	At    string `json:"created_at"`
}

type overlayCut struct {
	Limit *int `json:"limit"`
	First struct {
		overlayStep
		Type string `json:"type"`
	} `json:"first"`
}

type overlayNotice struct {
	At      *int   `json:"at"`
	Created string `json:"created_at"`
	Code    string `json:"code"`
	Text    string `json:"text"`
}

type overlayError struct {
	After   *int   `json:"after"`
	Created string `json:"created_at"`
	Text    string `json:"text"`
}

type overlayFork struct {
	Source  string `json:"source"`
	Through *int   `json:"through"`
}

func readOverlay(brain *os.Root, expected os.FileInfo) overlay {
	if expected.Size() > maxOverlayBytes {
		return overlay{}
	}
	f, err := openRootFile(brain, overlayPath)
	if err != nil {
		return overlay{}
	}
	defer f.Close()
	if actual, err := f.Stat(); err != nil || !sameFile(expected, actual) {
		return overlay{}
	}
	data, err := io.ReadAll(io.LimitReader(f, maxOverlayBytes+1))
	if err != nil || len(data) > maxOverlayBytes || !utf8.Valid(data) {
		return overlay{}
	}
	var ov overlay
	var keys map[string]json.RawMessage
	if json.Unmarshal(data, &ov) != nil || ov.Version != 1 || json.Unmarshal(data, &keys) != nil {
		return overlay{}
	}
	for _, key := range []string{"cut", "hidden", "notices", "errors", "fork"} {
		if string(keys[key]) == "null" {
			return overlay{}
		}
	}
	index := func(n *int) bool { return n != nil && *n >= 0 && *n < math.MaxInt-1 }
	dated := func(at string) bool { _, err := time.Parse(time.RFC3339Nano, at); return err == nil }
	step := func(s overlayStep) bool { return index(s.Index) && dated(s.At) }
	if c := ov.Cut; c != nil && (!index(c.Limit) || !step(c.First.overlayStep) || *c.Limit != *c.First.Index || c.First.Type == "") {
		return overlay{}
	}
	for _, h := range ov.Hidden {
		if !step(h) {
			return overlay{}
		}
	}
	for _, n := range ov.Notices {
		if !index(n.At) || !dated(n.Created) || n.Code != "mode_switched" || n.Text == "" {
			return overlay{}
		}
	}
	for _, e := range ov.Errors {
		if !index(e.After) || !dated(e.Created) || e.Text == "" {
			return overlay{}
		}
	}
	if f := ov.Fork; f != nil && (!conversationID.MatchString(f.Source) || !index(f.Through)) {
		return overlay{}
	}
	return ov
}

// applyOverlay returns a new fold only for applied entries, leaving the native generations intact.
func applyOverlay(steps []agyStep, ov overlay) ([]agyStep, string) {
	applied := overlay{Fork: ov.Fork}
	if c := ov.Cut; c != nil {
		i, found := slices.BinarySearchFunc(steps, *c.Limit+1, compareStep)
		if found && steps[i].Type == c.First.Type && steps[i].CreatedAt == c.First.At {
			steps, applied.Cut = steps[:i], c
		}
	}
	if len(ov.Hidden) > 0 {
		hidden := make(map[int]map[string]bool, len(ov.Hidden))
		for _, h := range ov.Hidden {
			seq := *h.Index + 1
			if hidden[seq] == nil {
				hidden[seq] = make(map[string]bool)
			}
			hidden[seq][h.At] = true
		}
		steps = slices.DeleteFunc(slices.Clone(steps), func(s agyStep) bool {
			if s.Type != "USER_INPUT" || !hidden[s.seq][s.CreatedAt] {
				return false
			}
			applied.Hidden = append(applied.Hidden, overlayStep{Index: new(s.seq - 1), At: s.CreatedAt})
			return true
		})
	}
	lastInput := 0
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].Type == "USER_INPUT" {
			lastInput = steps[i].seq
			break
		}
	}
	if len(ov.Notices)+len(ov.Errors) > 0 {
		type insert struct {
			at   int
			step agyStep
		}
		var inserts []insert
		add := func(i, anchor, ordinal int, typ, at, text string) {
			seq := 1
			if len(steps) > 0 {
				seq = steps[min(i, len(steps)-1)].seq
			}
			key := agyDigest(fmt.Sprintf("%s:%d:%d\n%s\n%s", typ, anchor, ordinal, at, text))
			inserts = append(inserts, insert{i, agyStep{seq: seq, Type: typ, CreatedAt: at, Content: text, key: "overlay-" + key}})
		}
		for ordinal, n := range ov.Notices {
			i, _ := slices.BinarySearchFunc(steps, *n.At+1, compareStep)
			add(i, *n.At, ordinal, "PERSONA_MODE_SWITCH", n.Created, n.Text)
			applied.Notices = append(applied.Notices, n)
		}
		for ordinal, e := range ov.Errors {
			if lastInput > *e.After+1 {
				continue
			}
			i, _ := slices.BinarySearchFunc(steps, *e.After+2, compareStep)
			add(i, *e.After, ordinal, "PERSONA_ERROR", e.Created, e.Text)
			applied.Errors = append(applied.Errors, e)
		}
		if len(inserts) > 0 {
			slices.SortStableFunc(inserts, func(a, b insert) int { return a.at - b.at })
			out := make([]agyStep, 0, len(steps)+len(inserts))
			prev := 0
			for _, insert := range inserts {
				out = append(out, steps[prev:insert.at]...)
				out = append(out, insert.step)
				prev = insert.at
			}
			steps = append(out, steps[prev:]...)
		}
	}
	b, _ := json.Marshal(applied)
	if string(b) == `{"version":0}` {
		return steps, ""
	}
	return steps, agyDigest(string(b))
}
