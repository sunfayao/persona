package agy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

type agyStepUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadTokens          int64 `json:"cache_read_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheWriteTokens         int64 `json:"cache_write_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheWrite1hTokens       int64 `json:"cache_write_1h_tokens"`
	ModelContextWindow       int64 `json:"model_context_window"`
}

type agyStep struct {
	Index                    *int            `json:"step_index"`
	Type                     string          `json:"type"`
	Source                   string          `json:"source"`
	Status                   string          `json:"status"`
	CreatedAt                string          `json:"created_at"`
	UpdatedAt                string          `json:"updated_at"`
	Content                  string          `json:"content"`
	ClientMsgID              string          `json:"client_msg_id"`
	Thinking                 string          `json:"thinking"`
	Error                    string          `json:"error"`
	Model                    string          `json:"model"`
	InputTokens              int64           `json:"input_tokens"`
	OutputTokens             int64           `json:"output_tokens"`
	CacheReadTokens          int64           `json:"cache_read_tokens"`
	CacheReadInputTokens     int64           `json:"cache_read_input_tokens"`
	CacheWriteTokens         int64           `json:"cache_write_tokens"`
	CacheCreationInputTokens int64           `json:"cache_creation_input_tokens"`
	CacheWrite1hTokens       int64           `json:"cache_write_1h_tokens"`
	ModelContextWindow       int64           `json:"model_context_window"`
	Usage                    *agyStepUsage   `json:"usage"`
	Media                    json.RawMessage `json:"media"`
	ToolCalls                []*nativeCall   `json:"tool_calls"`
	seq                      int
	key                      string
	branch                   string
	record                   [32]byte
	rendered                 [32]byte
	backgroundStart          string
}

type agyStepMemo struct {
	seq      int
	key      string
	rendered [32]byte
	running  bool
}

type agyRevision struct {
	length                  int64
	hash                    [32]byte
	branch, native, rewrite string
	steps                   []agyStepMemo
}

func isAgyStepRunning(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "RUNNING", "PENDING", "IN_PROGRESS", "QUEUED":
		return true
	default:
		return false
	}
}

// renderedHash hashes only the fields projected into transcript items so
// status-only transitions (such as RUNNING -> DONE) or unrendered metadata
// changes do not masquerade as history rewrites.
func (s agyStep) renderedHash() [32]byte {
	h := sha256.New()
	write := func(v string) {
		var lenBuf [8]byte
		n := len(v)
		for i := range lenBuf {
			lenBuf[i] = byte(n >> (i * 8))
		}
		_, _ = h.Write(lenBuf[:])
		_, _ = h.Write([]byte(v))
	}
	write(s.Type)
	write(s.Source)
	write(s.Content)
	write(s.ClientMsgID)
	write(s.Thinking)
	write(s.Error)
	if s.Status == "ERROR" {
		write("ERROR")
	} else {
		write("")
	}
	write(string(bytes.TrimSpace(s.Media)))
	for _, tc := range s.ToolCalls {
		if tc == nil {
			write("")
			continue
		}
		write(tc.Name)
		write(string(bytes.TrimSpace(tc.Args)))
	}
	var sum [32]byte
	h.Sum(sum[:0])
	return sum
}

// agyStepsRewritten reports whether a prefix change or file truncation altered
// settled history, rather than normal RUNNING -> DONE progress or non-rendered
// metadata updates.
func agyStepsRewritten(oldSteps, newSteps []agyStepMemo) bool {
	if len(newSteps) < len(oldSteps) {
		return true
	}
	bySeq := make(map[int]agyStepMemo, len(newSteps))
	for _, s := range newSteps {
		bySeq[s.seq] = s
	}
	for _, prev := range oldSteps {
		cur, ok := bySeq[prev.seq]
		if !ok {
			return true
		}
		if cur.key != prev.key {
			return true
		}
		if cur.rendered != prev.rendered && !prev.running {
			return true
		}
	}
	return false
}

func agyDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("%x", sum[:10])
}

// agyFold is parseAgySteps' state after the complete lines it has read: the steps kept and the branch and native
// revisions so far. Each line folds in on its own, so lines appended later go on from it (reader.go).
type agyFold struct {
	conversation   string
	steps          []agyStep
	branch, native string
}

// add folds raw's lines in, raw starting at a line.
func (f *agyFold) add(raw []byte) {
	steps, rev := f.steps, agyRevision{branch: f.branch, native: f.native}
	conversation := f.conversation
	defer func() { f.steps, f.branch, f.native = steps, rev.branch, rev.native }()
	line := bytes.TrimSpace(raw)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var step agyStep
	if json.Unmarshal(line, &step) != nil {
		return
	}
	step.record = sha256.Sum256(line)
	step.rendered = step.renderedHash()
	if step.Index != nil && *step.Index >= 0 && *step.Index < math.MaxInt-1 {
		step.seq = *step.Index + 1
	} else if len(steps) > 0 {
		step.seq = steps[len(steps)-1].seq + 1
	} else {
		step.seq = 1
	}
	at := len(steps)
	if len(steps) > 0 && step.seq <= steps[len(steps)-1].seq {
		last := steps[len(steps)-1]
		// A step whose sequence nothing retained has is a late arrival, not a rewind: parallel tool calls are
		// exported as they finish, so a call that fails at once lands before its sibling's result. It takes its
		// place in order, keeps the branch (every item ID) and marks the content as changed.
		floor := len(steps) - 1
		for floor >= 0 && steps[floor].seq > step.seq {
			floor--
		}
		updatesRunning := false
		if floor >= 0 {
			prev := steps[floor]
			if prev.seq == step.seq && step.Type == prev.Type && prev.CreatedAt == step.CreatedAt {
				updatesRunning = isAgyStepRunning(prev.Status)
				if prev.Type == "GENERIC" {
					step.backgroundStart = prev.backgroundStart
					if step.backgroundStart == "" && updatesRunning && agyBackgroundNotice(prev.Content) != "" {
						step.backgroundStart = prev.Content
					}
				}
			}
		}
		if step.seq < last.seq && (floor < 0 || steps[floor].seq != step.seq) {
			at = floor + 1
			rev.native = fmt.Sprintf("%x", step.record[:10])
		} else if step.seq < last.seq && (updatesRunning || step.backgroundStart != "") {
			// An earlier RUNNING step (such as a background tool call) finished after later steps were logged.
			// Update it in place without treating it as a branch rewind or discarding later steps.
			step.branch = steps[floor].branch
			step.key = steps[floor].key
			steps[floor] = step
			return
		} else {
			if step.seq < last.seq || step.Type != last.Type || step.CreatedAt != last.CreatedAt {
				rev.branch = agyDigest(rev.branch + "\n" + string(line))
				rev.native = ""
			} else if !isAgyStepRunning(last.Status) && step.rendered != last.rendered {
				rev.native = fmt.Sprintf("%x", step.record[:10])
			}
			for len(steps) > 0 && steps[len(steps)-1].seq >= step.seq {
				steps = steps[:len(steps)-1]
			}
			at = len(steps)
		}
	}
	// Include the conversation and branch so IDs survive completions but not rewinds.
	step.branch = rev.branch
	step.key = stepKey(conversation, step)
	steps = slices.Insert(steps, at, step)
}

// revision is the fold's agyRevision for the length bytes it read, whose SHA-256 is hash.
func (f *agyFold) revision(length int64, hash [32]byte) agyRevision {
	rev := agyRevision{length: length, hash: hash, branch: f.branch, native: f.native, steps: make([]agyStepMemo, len(f.steps))}
	for i, s := range f.steps {
		rev.steps[i] = agyStepMemo{
			seq:      s.seq,
			key:      s.key,
			rendered: s.rendered,
			running:  isAgyStepRunning(s.Status),
		}
	}
	return rev
}

func stepKey(conversation string, step agyStep) string {
	return strconv.Itoa(step.seq) + "@" + agyDigest(conversation+"\n"+step.CreatedAt+"\n"+step.branch)
}
