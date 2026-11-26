package appserver

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// The Codex desktop App keeps a local message bus — <CODEX_HOME>/ipc/ipc.sock, on Windows the named pipe
// \\.\pipe\codex-ipc (codex_desktop_unix.go, codex_desktop_windows.go): its windows ask each other to run a turn in a
// thread one of them owns. Persona joins that bus as one more client, so a message from the phone runs in the App
// window that owns the thread — it shows in the App, and no second writer opens the rollout.
//
// Frames are a 4-byte little-endian length and a JSON object. Every request carries the version the App expects for
// its method; without it no client claims the request and the bus answers "no-client-found".

const DesktopNotLoadedMessage = "Codex App 没能打开这个对话：在电脑的 Codex App 里打开它一次再发"

func desktopVersion(method string) int {
	switch method {
	case "thread-owner-discovery", "thread-follower-read-model-settings":
		return 1
	case "thread-follower-start-turn", "thread-follower-update-thread-settings":
		return 2
	case "thread-follower-interrupt-turn":
		return 4
	default:
		return 0
	}
}

type DesktopFrame struct {
	Type            string          `json:"type"`
	RequestID       string          `json:"requestId,omitempty"`
	Method          string          `json:"method,omitempty"`
	ResultType      string          `json:"resultType,omitempty"`
	Error           string          `json:"error,omitempty"`
	HandledByClient string          `json:"handledByClientId,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
	Params          any             `json:"params,omitempty"`
	Version         int             `json:"version,omitempty"`
	TargetClientID  string          `json:"targetClientId,omitempty"`
	Response        any             `json:"response,omitempty"`
}

type Desktop struct {
	client    *Client
	conn      net.Conn
	requestID func() string
}

func (c *Client) DialDesktop(ctx context.Context, dial func(context.Context) (net.Conn, error), requestID func() string) (*Desktop, error) {
	conn, err := dial(ctx)
	if err != nil {
		return nil, err
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	desktop := &Desktop{client: c, conn: conn, requestID: requestID}
	if _, err := desktop.Request("initialize", map[string]any{"clientType": "persona"}, ""); err != nil {
		conn.Close()
		return nil, err
	}
	return desktop, nil
}

func (c *Desktop) Close() { c.conn.Close() }

func (c *Desktop) write(f DesktopFrame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	buf := make([]byte, 4+len(b))
	binary.LittleEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err = c.conn.Write(buf)
	return err
}

func (c *Desktop) read() (DesktopFrame, error) {
	var f DesktopFrame
	var head [4]byte
	if _, err := io.ReadFull(c.conn, head[:]); err != nil {
		return f, err
	}
	n := binary.LittleEndian.Uint32(head[:])
	if n == 0 || n > 64<<20 {
		return f, fmt.Errorf("codex app ipc: bad frame length %d", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(c.conn, b); err != nil {
		return f, err
	}
	return f, json.Unmarshal(b, &f)
}

// Request sends one request and waits for its response; broadcasts are skipped, and the bus asking whether Persona
// handles someone else's request is told no at once (it would otherwise wait out its timeout for every request).
func (c *Desktop) Request(method string, params any, target string) (DesktopFrame, error) {
	id := c.requestID()
	if err := c.write(DesktopFrame{Type: "request", RequestID: id, Method: method, Params: params, Version: desktopVersion(method), TargetClientID: target}); err != nil {
		return DesktopFrame{}, err
	}
	for {
		f, err := c.read()
		if err != nil {
			return f, err
		}
		switch f.Type {
		case "client-discovery-request":
			if err := c.write(DesktopFrame{Type: "client-discovery-response", RequestID: f.RequestID, Response: map[string]any{"canHandle": false}}); err != nil {
				return f, err
			}
		case "response":
			if f.RequestID != id {
				continue
			}
			if f.ResultType != "success" {
				return f, fmt.Errorf("codex app ipc %s: %s", method, f.Error)
			}
			return f, nil
		}
	}
}

// WaitOwner looks up the App window that owns the thread, the App asked to open it when none does: it loads a thread it is
// asked to open (codex://threads/<id>, in front — opened in the background it does not), then its window owns it. An
// opener that cannot ask (no codex:// handler) skips the wait.
func (c *Desktop) WaitOwner(ctx context.Context, thread string, wait time.Duration, open func(string) error) (DesktopFrame, error) {
	owner, err := c.Owner(thread)
	if err != nil && open(thread) == nil {
		deadline := c.client.clock.Now().Add(wait)
		for err != nil && c.client.clock.Now().Before(deadline) && ctx.Err() == nil {
			c.client.clock.Sleep(time.Second)
			owner, err = c.Owner(thread)
		}
	}
	return owner, err
}

// Owner returns the App window that owns the thread.
func (c *Desktop) Owner(thread string) (DesktopFrame, error) {
	f, err := c.Request("thread-owner-discovery", map[string]any{"hostId": "local", "conversationId": thread}, "")
	if err == nil && f.HandledByClient == "" {
		err = errors.New(DesktopNotLoadedMessage)
	}
	return f, err
}
