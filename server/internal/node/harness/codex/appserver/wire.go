package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Wire correlates app-server requests with responses and passes other messages to the owner.
type Wire struct {
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan reply
	closed  bool
	write   func([]byte) error
	ended   <-chan struct{}
	notify  func(Message)
}

func (c *Client) Bind(write func([]byte) error, ended <-chan struct{}, notify func(Message)) *Wire {
	return &Wire{write: write, ended: ended, notify: notify, pending: make(map[int64]chan reply)}
}

func (w *Wire) Stop() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
}

func (w *Wire) Closed() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.closed
}

type reply struct {
	Result json.RawMessage
	Err    *RPCError
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("codex: %s (%d)", e.Message, e.Code) }

// Message is any line from the app-server: a response (id + result / error), a notification (method + params)
// or a request to us (id + method + params).
type Message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *RPCError       `json:"error"`
}

func (c *Wire) Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return c.write(b)
}

// call sends a request and waits for its response (or the process ending, or ctx).
func (c *Wire) Call(ctx context.Context, method string, params any, out any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("codex app-server exited")
	}
	c.nextID++
	id := c.nextID
	ch := make(chan reply, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()
	if err := c.Send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case r := <-ch:
		if r.Err != nil {
			return r.Err
		}
		if out != nil && len(r.Result) > 0 {
			return json.Unmarshal(r.Result, out)
		}
		return nil
	case <-c.ended:
		return errors.New("codex app-server exited")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Wire) Reply(id json.RawMessage, result any) {
	_ = c.Send(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func (c *Wire) ReplyError(id json.RawMessage, code int, msg string) {
	_ = c.Send(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

func (c *Wire) Receive(m Message) {
	if m.Method == "" && len(m.ID) > 0 {
		var id int64
		if json.Unmarshal(m.ID, &id) != nil {
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		c.mu.Unlock()
		if ch != nil {
			select {
			case ch <- reply{Result: m.Result, Err: m.Error}:
			default:
			}
		}
		return
	}
	c.notify(m)
}
func ReadMessages(r io.Reader, limit int, receive func(Message)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), limit)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 || line[0] != '{' { // an interactive shell may print banners
			continue
		}
		var m Message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		receive(m)
	}
	return sc.Err()
}
