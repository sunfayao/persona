package agy

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sunfayao/persona/server/internal/lazyre"
	"github.com/sunfayao/persona/server/internal/proto"
)

var (
	exitCode = lazyre.New(`(?m)^Command exited with code (-?\d+)\.[ \t\r]*$`)
	spill    = lazyre.New(`(?im)^.*\boutput was (?:truncated|too long)\b[^\n]*\bfile://[^\s]+`)
	spillURL = lazyre.New(`file://[^\s<>]+`)
)

func commandExit(text string) *int {
	if !strings.Contains(text, "Command exited with code ") {
		return nil
	}
	if m := exitCode.FindStringSubmatch(text); m != nil {
		if code, err := strconv.Atoi(m[1]); err == nil {
			return &code
		}
	}
	return nil
}

func resultText(text string) (string, *int64) {
	var created, completed time.Time
	original := text
	for {
		line, rest, _ := strings.Cut(text, "\n")
		var dst *time.Time
		var stamp string
		if v, ok := strings.CutPrefix(line, "Created At:"); ok {
			dst, stamp = &created, v
		} else if v, ok := strings.CutPrefix(line, "Completed At:"); ok {
			dst, stamp = &completed, v
		}
		if dst == nil {
			break
		}
		if at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(stamp)); err == nil {
			*dst = at
		}
		text = rest
	}
	if len(text) < len(original) {
		// A spilled output preview can quote the native header again after its file link.
		if !created.IsZero() && !completed.IsZero() && strings.Contains(text, "file://") {
			text = strings.Replace(text, "\n"+original[:len(original)-len(text)], "\n", 1)
		}
		text = strings.TrimPrefix(text, "Output:\n")
	}
	var ms *int64
	if !created.IsZero() && !completed.IsZero() && !completed.Before(created) {
		ms = new(completed.Sub(created).Milliseconds())
	}
	return text, ms
}

func mapResult(s agyStep, it *proto.Item) {
	text, ms := resultText(s.Content)
	code := commandExit(text)
	if s.Error != "" {
		text, _ = resultText(s.Error)
		if code == nil {
			code = commandExit(text)
		}
	}
	status := proto.OutcomeOK
	switch {
	case s.Error != "" || s.Status == "ERROR" || s.Status == "INVALID" || code != nil && *code != 0:
		status = proto.OutcomeFailed
	case s.Status == "RUNNING" && agyBackgroundNotice(s.Content) != "":
		status = proto.OutcomeBackground
	}
	it.Text, it.IsError = text, status == proto.OutcomeFailed
	it.Outcome = &proto.ToolOutcome{Status: status, ExitCode: code, DurationMS: ms}
}

const maxSpillBytes = 64 << 20

var ErrNotFound = errors.New("item not found")

func (h *Projection) Item(id string) (proto.Item, error) {
	for _, it := range h.items {
		if it.ID != id {
			continue
		}
		if step, ok := h.outputs[id]; ok {
			if brain, err := h.source.open(); err == nil {
				path := filepath.Join(".system_generated", "steps", strconv.Itoa(step), "output.txt")
				if raw, err := readRegular(brain, path, maxSpillBytes); err == nil {
					it.Text, _ = resultText(string(raw))
					it.FullSize = 0
				}
				brain.Close()
			}
		}
		return it, nil
	}
	return proto.Item{}, ErrNotFound
}

func (h *Projection) Image(id string) (*proto.Image, error) {
	media, ok := h.media[id]
	if !ok {
		return nil, ErrNotFound
	}
	brain, err := h.source.open()
	if err != nil {
		return nil, err
	}
	defer brain.Close()
	return media.image(brain)
}

func readRegular(brain *os.Root, path string, maxBytes int64) ([]byte, error) {
	info, err := regularStat(brain, path)
	if err != nil || info == nil || info.Size() > maxBytes {
		return nil, os.ErrInvalid
	}
	f, err := openRootFile(brain, path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if opened, err := f.Stat(); err != nil || !sameFile(info, opened) {
		return nil, os.ErrInvalid
	}
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if int64(len(data)) > maxBytes {
		return nil, os.ErrInvalid
	}
	return data, err
}

// Only the explicit native spill notice, pointing at this step's output, authorizes an extra file read.
func (h *Projection) addSpill(it *proto.Item, step int) {
	if it.IsError || !strings.Contains(it.Text, "file://") {
		return
	}
	line := spill.FindString(it.Text)
	if line == "" {
		return
	}
	uri := strings.TrimRight(spillURL.FindString(line), ").")
	path := agyImagePath(uri)
	want, err := filepath.Abs(filepath.Join(h.source.Root, "brain", h.source.Conversation, ".system_generated", "steps", fmt.Sprint(step), "output.txt"))
	if err != nil || path != want {
		return
	}
	if h.outputs == nil {
		h.outputs = make(map[string]int)
	}
	if brain, err := h.source.open(); err == nil {
		defer brain.Close()
		rel := filepath.Join(".system_generated", "steps", fmt.Sprint(step), "output.txt")
		if info, err := regularStat(brain, rel); err == nil && info != nil && info.Size() <= maxSpillBytes {
			it.FullSize = int(info.Size())
		}
	}
	h.outputs[it.ID] = step
}
