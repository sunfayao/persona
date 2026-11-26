package node

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nwverse/persona/server/internal/assistantwire"
)

// The space_state_update tool replaces a track's state block. Each atomic write keeps the previous file in
// .space/history/. The node reads the file without following a symlink.

func init() { nodeRPC(assistantwire.DeviceAssistantSpaceMD, (*Node).assistantSpaceMD) }

// The markers of a SPACE.md's blocks Persona manages: <!-- persona:managed:<name>:begin --> … :end -->. state is the
// only one so far (prompts/assistant/space_md.md).
const (
	spaceStateBegin = "<!-- persona:managed:state:begin -->"
	spaceStateEnd   = "<!-- persona:managed:state:end -->"
	managedMarker   = "<!-- persona:managed:"
)

// spaceHistoryKeep is how many earlier SPACE.md files .space/history/ keeps.
const spaceHistoryKeep = 20

// spaceMDMu makes each SPACE.md call's read and write one step: two writers never base on the same file.
var spaceMDMu sync.Mutex

// spaceNow is the clock a backup is named by; tests set it.
var spaceNow = time.Now

// errSpaceMDTooLarge: the SPACE.md is over spaceMDLimit; it is edited on the computer, not here.
var errSpaceMDTooLarge = errors.New("SPACE.md is larger than 64 KB")

// assistantSpaceMD answers assistant.space_md for the track directory c names.
func (n *Node) assistantSpaceMD(_ context.Context, c assistantwire.SpaceMDCall) (assistantwire.SpaceMD, error) {
	if c.Dir == "" || strings.Contains(c.Dir, "..") {
		return assistantwire.SpaceMD{}, errors.New("bad assistant.space_md params")
	}
	dir, err := n.preparedDir(c.Dir)
	if err != nil {
		return assistantwire.SpaceMD{}, err
	}
	return spaceMDCall(dir, c)
}

// spaceMDCall does c in track directory dir.
func spaceMDCall(dir string, c assistantwire.SpaceMDCall) (assistantwire.SpaceMD, error) {
	if err := checkSpaceDir(dir, c.Slug); err != nil {
		return assistantwire.SpaceMD{}, err
	}
	switch fi, err := os.Lstat(dir); {
	case errors.Is(err, os.ErrNotExist):
		return assistantwire.SpaceMD{Refused: assistantwire.SpaceMDMissing}, nil
	case err != nil:
		return assistantwire.SpaceMD{}, err
	case !fi.IsDir():
		return assistantwire.SpaceMD{}, fmt.Errorf("%s: not a folder", dir)
	}
	spaceMDMu.Lock()
	defer spaceMDMu.Unlock()
	path := filepath.Join(dir, "SPACE.md")
	old, err := readSpaceMDWhole(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return assistantwire.SpaceMD{Refused: assistantwire.SpaceMDMissing}, nil
	case errors.Is(err, errSpaceMDTooLarge):
		return assistantwire.SpaceMD{Refused: assistantwire.SpaceMDTooLarge}, nil
	case err != nil:
		return assistantwire.SpaceMD{}, err
	}
	cur := spaceMDOf(old)
	refused := func(why string) (assistantwire.SpaceMD, error) {
		cur.Refused = why
		return cur, nil
	}
	var next string
	switch c.Op {
	case assistantwire.SpaceMDPutState:
		state := strings.TrimRight(c.State, "\r\n")
		switch {
		case len(state) > assistantwire.SpaceMDStateMax:
			return refused(assistantwire.SpaceMDTooLarge)
		case strings.Contains(state, managedMarker):
			return refused(assistantwire.SpaceMDManaged)
		}
		inner, end, ok := stateBlock(old)
		if !ok {
			return refused(assistantwire.SpaceMDNoState)
		}
		if state != "" {
			state += "\n"
		}
		next = old[:inner] + "\n" + state + old[end:]
		if len(next) > spaceMDLimit {
			return refused(assistantwire.SpaceMDTooLarge)
		}
	default:
		return assistantwire.SpaceMD{}, fmt.Errorf("assistant.space_md: unknown op %q", c.Op)
	}
	if next == old {
		return cur, nil // nothing changes: no write, no backup
	}
	if err := backupSpaceMD(dir, old); err != nil {
		return assistantwire.SpaceMD{}, err
	}
	if err := writeFileAtomic(path, []byte(next)); err != nil {
		return assistantwire.SpaceMD{}, err
	}
	return spaceMDOf(next), nil
}

// readSpaceMDWhole reads a track's SPACE.md whole, as readSpaceMD does (a plain file, never through a link), or
// errSpaceMDTooLarge.
func readSpaceMDWhole(path string) (string, error) {
	f, err := openNoBlock(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%s: not a plain file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, spaceMDLimit+1))
	if err != nil {
		return "", err
	}
	if len(b) > spaceMDLimit {
		return "", errSpaceMDTooLarge
	}
	return string(b), nil
}

// spaceMDOf is content as the manager is answered it.
func spaceMDOf(content string) assistantwire.SpaceMD {
	out := assistantwire.SpaceMD{Content: content, Digest: digestOf(content)}
	if inner, end, ok := stateBlock(content); ok {
		out.State = strings.TrimSuffix(strings.TrimPrefix(content[inner:end], "\n"), "\n")
	}
	return out
}

// stateBlock finds s's state block: the offset just after its begin marker and the offset of its end marker, ok only
// when s has exactly one of each, in that order.
func stateBlock(s string) (inner, end int, ok bool) {
	if strings.Count(s, spaceStateBegin) != 1 || strings.Count(s, spaceStateEnd) != 1 {
		return 0, 0, false
	}
	inner = strings.Index(s, spaceStateBegin) + len(spaceStateBegin)
	end = strings.Index(s, spaceStateEnd)
	return inner, end, end >= inner
}

// backupSpaceMD keeps old in dir's .space/history/ as SPACE-<UTC time>.md, then the latest spaceHistoryKeep only.
func backupSpaceMD(dir, old string) error {
	hist := filepath.Join(dir, ".space", "history")
	if err := os.MkdirAll(hist, 0o700); err != nil {
		return err
	}
	for _, p := range []string{filepath.Join(dir, ".space"), hist} {
		if fi, err := os.Lstat(p); err != nil || !fi.IsDir() {
			return fmt.Errorf("%s: not a folder", p)
		}
	}
	t := spaceNow().UTC()
	var path string
	for {
		path = filepath.Join(hist, "SPACE-"+t.Format("20060102T150405.000000000Z")+".md")
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
			break
		}
		t = t.Add(time.Nanosecond) // the names sort in time order: never two of one name
	}
	if err := writeFileAtomic(path, []byte(old)); err != nil {
		return err
	}
	entries, err := os.ReadDir(hist)
	if err != nil {
		return err
	}
	var kept []string
	for _, e := range entries { // by name, so oldest first
		if name := e.Name(); e.Type().IsRegular() && strings.HasPrefix(name, "SPACE-") && strings.HasSuffix(name, ".md") {
			kept = append(kept, name)
		}
	}
	for _, name := range kept[:max(0, len(kept)-spaceHistoryKeep)] {
		if err := os.Remove(filepath.Join(hist, name)); err != nil {
			return err
		}
	}
	return nil
}
