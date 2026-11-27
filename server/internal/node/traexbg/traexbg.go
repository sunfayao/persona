// Package traexbg reads the background commands a TRAE CLI conversation still runs.
//
// TRAE CLI keeps two records of a command it runs in the background (Bash with run_in_background, or one it moved
// to the background when it ran past its wait):
//   - its rollout gets an item_completed event for a CommandExecution item with status "backgrounded", naming the
//     task (process_id), its command and its description; foreground commands never get that status;
//   - <rollout>.artifacts/background-tasks/<kind>-<task id>.snapshot.json holds the task's status, which the CLI
//     rewrites from "running" to "completed", "failed" or "interrupted" when it ends. Every command gets a snapshot,
//     foreground ones too, so a running snapshot counts only once the rollout says its command went to the background.
//
// This is what the CLI's own status line counts ("1 shell running"). Its screen shows nothing of it once the turn is
// over, so the node reads these files instead.
package traexbg

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

const (
	// maxAge: a snapshot still running after this long belongs to a CLI that went away without ending it.
	maxAge = 24 * time.Hour
	// scanReach: how much of a rollout's end the first look reads for backgrounded commands. A command backgrounded
	// before that (a long turn full of large tool output) is not listed.
	scanReach = 32 << 20
	// maxLine: rollout lines longer than this are skipped unread; a backgrounded command's line has no output.
	maxLine = 1 << 20
	// maxSnapshot: a snapshot file is a few hundred bytes.
	maxSnapshot = 64 << 10
	// idle: a rollout not asked about for this long is forgotten.
	idle = time.Hour
)

// Clock gives the time.
type Clock interface{ Now() time.Time }

type systemClock struct{}

func (systemClock) Now() time.Time { return time.Now() }

// Dependencies of a Reader.
type Dependencies struct {
	Clock Clock // nil: the system clock
}

// Reader follows the rollouts of the conversations it is asked about.
type Reader struct {
	clock    Clock
	mu       sync.Mutex
	rollouts map[string]*rollout
}

// New returns a Reader.
func New(deps Dependencies) *Reader {
	if deps.Clock == nil {
		deps.Clock = systemClock{}
	}
	return &Reader{clock: deps.Clock, rollouts: map[string]*rollout{}}
}

type started struct {
	description, command string
	at                   time.Time
}

type snapshotFile struct {
	size  int64
	mtime time.Time
	snap  snapshot
}

// rollout is what is known of one rollout and its artifacts.
type rollout struct {
	mu       sync.Mutex
	used     time.Time
	off      int64              // the rollout is scanned up to here, at a line's start
	size     int64              // its size at that scan
	scanned  bool               // off is set
	started  map[string]started // task id → its backgrounded command
	snaps    map[string]snapshotFile
	finished map[string]bool // snapshot file names whose task has ended: never read again
}

type snapshot struct {
	TaskID    string    `json:"task_id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Running lists the background commands of the conversation whose rollout is path that still run, oldest first;
// nil when there are none or path is empty.
func (r *Reader) Running(path string) []proto.BackgroundTask {
	if path == "" {
		return nil
	}
	now := r.clock.Now()
	r.mu.Lock()
	for p, ro := range r.rollouts {
		if now.Sub(ro.used) > idle {
			delete(r.rollouts, p)
		}
	}
	ro := r.rollouts[path]
	if ro == nil {
		ro = &rollout{started: map[string]started{}, snaps: map[string]snapshotFile{}, finished: map[string]bool{}}
		r.rollouts[path] = ro
	}
	ro.used = now
	r.mu.Unlock()

	ro.mu.Lock()
	defer ro.mu.Unlock()
	dir := filepath.Join(strings.TrimSuffix(path, ".jsonl")+".artifacts", "background-tasks")
	running := ro.runningSnapshots(dir, now)
	if len(running) == 0 {
		return nil
	}
	ro.scan(path)
	var out []proto.BackgroundTask
	for _, s := range running {
		st, ok := ro.started[s.snap.TaskID]
		if !ok || s.snap.Kind != "shell" {
			continue // a foreground command, or one the scan did not reach
		}
		at := st.at
		if at.IsZero() {
			at = s.snap.UpdatedAt
		}
		out = append(out, proto.BackgroundTask{
			ID: s.snap.TaskID, Kind: "shell", Description: st.description, Command: st.command,
			OutputFile: filepath.Join(dir, s.snap.Kind+"-"+s.snap.TaskID+".output.txt"), StartedAt: at.UTC(),
		})
	}
	slices.SortFunc(out, func(a, b proto.BackgroundTask) int {
		if c := a.StartedAt.Compare(b.StartedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out
}

// runningSnapshots reads the snapshots that have not ended yet and returns those that say running.
func (ro *rollout) runningSnapshots(dir string, now time.Time) []snapshotFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		ro.snaps, ro.finished = map[string]snapshotFile{}, map[string]bool{} // none yet, or the conversation is gone
		return nil
	}
	var out []snapshotFile
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".snapshot.json") || ro.finished[name] || !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil || info.Size() > maxSnapshot {
			continue
		}
		f, ok := ro.snaps[name]
		if !ok || f.size != info.Size() || !f.mtime.Equal(info.ModTime()) {
			b, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			f = snapshotFile{size: info.Size(), mtime: info.ModTime()}
			if json.Unmarshal(b, &f.snap) != nil {
				continue // being written: read again next time
			}
			ro.snaps[name] = f
		}
		if f.snap.Status != "running" {
			ro.finished[name] = true
			delete(ro.snaps, name)
			continue
		}
		if plainID(f.snap.TaskID) && now.Sub(f.snap.UpdatedAt) < maxAge {
			out = append(out, f)
		}
	}
	return out
}

// scan reads what was appended to the rollout since the last scan (on the first, its last scanReach bytes) for
// commands that went to the background.
func (ro *rollout) scan(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return
	}
	size := st.Size()
	if size < ro.size { // rewritten
		ro.scanned, ro.started = false, map[string]started{}
	}
	if !ro.scanned {
		ro.off, ro.scanned = 0, true
		if size > scanReach {
			ro.off = size - scanReach
		}
	}
	ro.size = size
	if size <= ro.off {
		return
	}
	if _, err := f.Seek(ro.off, io.SeekStart); err != nil {
		return
	}
	br := bufio.NewReaderSize(io.LimitReader(f, size-ro.off), 64<<10)
	partial := ro.off > 0 && !ro.atLineStart(f)
	var line []byte
	var read int64 // bytes of the current line so far
	long := false
	for {
		chunk, err := br.ReadSlice('\n')
		read += int64(len(chunk))
		if errors.Is(err, bufio.ErrBufferFull) {
			if !long && len(line)+len(chunk) <= maxLine {
				line = append(line, chunk...)
			} else {
				long, line = true, line[:0]
			}
			continue
		}
		if err != nil { // the last line is not complete yet: read it whole next time
			return
		}
		if !long && !partial {
			ro.note(append(line, chunk...))
		}
		ro.off += read
		read, line, long, partial = 0, line[:0], false, false
	}
}

// atLineStart says whether ro.off starts a line (the byte before it is a newline).
func (ro *rollout) atLineStart(f *os.File) bool {
	b := make([]byte, 1)
	if _, err := f.ReadAt(b, ro.off-1); err != nil {
		return false
	}
	return b[0] == '\n'
}

var (
	commandExecution = []byte(`"CommandExecution"`)
	backgrounded     = []byte(`"status":"backgrounded"`)
)

// note records the command of one rollout line when it went to the background.
func (ro *rollout) note(line []byte) {
	if !bytes.Contains(line, backgrounded) || !bytes.Contains(line, commandExecution) {
		return
	}
	var l struct {
		Timestamp time.Time `json:"timestamp"`
		Payload   struct {
			Type string `json:"type"`
			Item struct {
				Type        string   `json:"type"`
				ProcessID   string   `json:"process_id"`
				Command     []string `json:"command"`
				Status      string   `json:"status"`
				Description string   `json:"description"`
			} `json:"item"`
			StartedAtMs int64 `json:"started_at_ms"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &l) != nil || l.Payload.Type != "item_completed" {
		return
	}
	it := l.Payload.Item
	if it.Type != "CommandExecution" || it.Status != "backgrounded" || it.ProcessID == "" {
		return
	}
	at := l.Timestamp
	if l.Payload.StartedAtMs > 0 {
		at = time.UnixMilli(l.Payload.StartedAtMs)
	}
	ro.started[it.ProcessID] = started{description: strings.TrimSpace(it.Description), command: commandText(it.Command), at: at}
}

// plainID: a task id that can name its output file (the CLI's are numbers).
func plainID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range id {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}

// commandText is the script of a `<shell> -lc <script>` argv, else the argv joined.
func commandText(argv []string) string {
	if len(argv) == 3 && (argv[1] == "-lc" || argv[1] == "-c") {
		return argv[2]
	}
	return strings.Join(argv, " ")
}
