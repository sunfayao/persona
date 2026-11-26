package codex

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"

	"github.com/nwverse/persona/server/internal/proto"
)

// ReadTurnStatus applies completed event lines through end and returns the offset of the next line.
func ReadTurnStatus(path, status string, off, end int64) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return proto.StatusUnknown, off, err
	}
	defer f.Close()
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return proto.StatusUnknown, off, err
	}
	reader := bufio.NewReader(io.LimitReader(f, end-off))
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if err != io.EOF {
				return proto.StatusUnknown, off, err
			}
			break // Retry an unfinished line at the same offset when the rollout grows.
		}
		off += int64(len(line))
		var e struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
			} `json:"payload"`
		}
		if !bytes.Contains(line, []byte(`"event_msg"`)) || json.Unmarshal(line, &e) != nil || e.Type != "event_msg" {
			continue
		}
		switch e.Payload.Type {
		case "task_started":
			status = proto.StatusWorking
		case "task_complete", "turn_aborted":
			status = proto.StatusDone
		}
	}
	return status, off, nil
}
