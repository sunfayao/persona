package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/coder/websocket"

	"github.com/nwverse/persona/server/internal/extwire"
)

// A transcription contribution's job socket: the
// connection extension_transcribe.go dials and reads.

// The times of a job; tests shorten them.
var (
	// jobReadyWait: a job answers ready within this, else it counts as upstream_unavailable.
	jobReadyWait = 30 * time.Second
	// jobDoneWait: a job sends done within this of its end.
	jobDoneWait = 5 * time.Minute
	// replayAhead is how much audio a replay sends ahead of the job's ack.
	replayAhead = time.Minute
)

const (
	// audioRate is the samples a second of the PCM16 mono audio a job takes.
	audioRate = 16000
	// replayFrame is how much audio one binary frame of a replay carries (one second, under the 64 KiB a frame may be).
	replayFrame = audioRate * 2
)

// jobWriteWait bounds one frame written to a job.
const jobWriteWait = 10 * time.Second

// errNoTranscribers: this process reaches no transcription contribution (no extensions layer).
var errNoTranscribers = errors.New("no transcription contribution in this process")

// jobConn is one job socket.
type jobConn struct {
	c    *websocket.Conn
	done func()
}

// dialJob opens a job of contribution ref.
func (s *Service) dialJob(ctx context.Context, ref string) (*jobConn, error) {
	if s.trans == nil {
		return nil, errNoTranscribers
	}
	c, done, err := s.trans.TranscriptionDial(ctx, ref)
	if err != nil {
		return nil, err
	}
	if done == nil {
		done = func() {}
	}
	return &jobConn{c: c, done: done}, nil
}

// send writes text frame f.
func (j *jobConn) send(ctx context.Context, f extwire.TranscriptionFrame) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, jobWriteWait)
	defer cancel()
	return j.c.Write(ctx, websocket.MessageText, raw)
}

// sendAudio writes a binary frame of PCM16 audio.
func (j *jobConn) sendAudio(ctx context.Context, pcm []byte) error {
	ctx, cancel := context.WithTimeout(ctx, jobWriteWait)
	defer cancel()
	return j.c.Write(ctx, websocket.MessageBinary, pcm)
}

// read is the job's next text frame (binary frames from it are skipped; a text frame that is not JSON is an error).
func (j *jobConn) read(ctx context.Context) (extwire.TranscriptionFrame, error) {
	for {
		typ, raw, err := j.c.Read(ctx)
		if err != nil {
			return extwire.TranscriptionFrame{}, err
		}
		if typ != websocket.MessageText {
			continue
		}
		var f extwire.TranscriptionFrame
		if err := json.Unmarshal(raw, &f); err != nil {
			return f, fmt.Errorf("a frame that is not JSON: %w", err)
		}
		return f, nil
	}
}

// awaitReady reads until the job's ready; a fatal error before it is a jobError.
func (j *jobConn) awaitReady(ctx context.Context) (extwire.TranscriptionFrame, error) {
	for {
		f, err := j.read(ctx)
		if err != nil {
			return f, err
		}
		switch f.Type {
		case extwire.TFReady:
			return f, nil
		case extwire.TFError:
			if f.Fatal {
				return f, &jobError{code: f.Code}
			}
		case extwire.TFDone:
			return f, &jobError{code: extwire.TEUpstreamFailed}
		}
	}
}

// close ends the job socket and the extension's open request.
func (j *jobConn) close() {
	_ = j.c.Close(websocket.StatusNormalClosure, "")
	j.done()
}

// jobError is a job's fatal error.
type jobError struct{ code string }

func (e *jobError) Error() string { return "transcription job: " + e.code }
