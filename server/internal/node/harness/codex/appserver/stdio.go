package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/sunfayao/persona/server/internal/node/harness/codex"
)

// Stdio is a short-lived, initialized app-server client used by both native metadata operations and model/list.
// Requests are sequential and bounded; it never resumes a desktop-owned thread or sends its model turns.
type Stdio struct {
	ctx    context.Context
	cancel context.CancelFunc
	cmd    *exec.Cmd
	in     io.WriteCloser
	lines  *bufio.Scanner
	next   int
}

func (c *Client) StartStdio(ctx context.Context, timeout time.Duration, prepare func(context.Context) (*exec.Cmd, error)) (*Stdio, error) {
	ctx, cancel := c.clock.WithTimeout(ctx, timeout)
	cmd, err := prepare(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	in, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = in.Close()
		return nil, err
	}
	stopGroup := cmd.Cancel
	cmd.Cancel = sync.OnceValue(func() error {
		// A CLI launcher can fork a child that keeps both pipes open. Close our ends even if the group is gone.
		_ = in.Close()
		_ = out.Close()
		if err := stopGroup(); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	})
	if err := cmd.Start(); err != nil {
		cancel()
		_ = in.Close()
		_ = out.Close()
		return nil, err
	}
	lines := bufio.NewScanner(out)
	lines.Buffer(make([]byte, 64<<10), 64<<20)
	s := &Stdio{ctx: ctx, cancel: cancel, cmd: cmd, in: in, lines: lines}
	if err := s.Call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": codex.DesktopClientName, "title": "Persona", "version": "1"},
		"capabilities": map[string]any{"experimentalApi": true}, // project/* requires this capability
	}, nil); err != nil {
		s.Close()
		return nil, err
	}
	if _, err := in.Write([]byte("{\"method\":\"initialized\"}\n")); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func (s *Stdio) Close() {
	s.cancel()
	_ = s.cmd.Cancel()
	_ = s.cmd.Wait()
}

// Call skips notifications and other responses until this request's answer. Canceling a call ends the short exchange.
func (s *Stdio) Call(ctx context.Context, method string, params, into any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, s.cancel)
	defer stop()
	s.next++
	id := s.next
	b, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := s.in.Write(append(b, '\n')); err != nil {
		return err
	}
	for s.lines.Scan() {
		var m struct {
			ID     *int            `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(s.lines.Bytes(), &m) != nil || m.ID == nil || *m.ID != id {
			continue
		}
		if m.Error != nil {
			return &StdioError{Method: method, Code: m.Error.Code, Message: m.Error.Message}
		}
		if into != nil {
			if err := json.Unmarshal(m.Result, into); err != nil {
				return fmt.Errorf("%s: invalid response: %w", method, err)
			}
		}
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if err := s.lines.Err(); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	return fmt.Errorf("%s: app-server closed before answering", method)
}

type StdioError struct {
	Method  string
	Code    int
	Message string
}

func (e *StdioError) Error() string { return fmt.Sprintf("%s: %s", e.Method, e.Message) }
