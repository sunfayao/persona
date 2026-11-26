package appserver

import (
	"context"
	"encoding/json"
	"net"
	"time"

	"github.com/coder/websocket"
)

// Clock supplies client deadlines and the refresh cadence.
type Clock interface {
	Now() time.Time
	Sleep(time.Duration)
	WithTimeout(context.Context, time.Duration) (context.Context, context.CancelFunc)
	Ticker(time.Duration) Ticker
}

type Ticker interface {
	Ticks() <-chan time.Time
	Stop()
}

type wallClock struct{}

func (wallClock) Now() time.Time        { return time.Now() }
func (wallClock) Sleep(d time.Duration) { time.Sleep(d) }

func (wallClock) WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
func (wallClock) Ticker(d time.Duration) Ticker { return wallTicker{time.NewTicker(d)} }

type wallTicker struct{ *time.Ticker }

func (t wallTicker) Ticks() <-chan time.Time { return t.C }

type Config struct {
	Clock       Clock
	DialContext func(context.Context, string, string) (net.Conn, error)
}

type Client struct {
	clock    Clock
	dialUnix func(context.Context, string, string) (net.Conn, error)
	dial     func(context.Context, string, string) (net.Conn, error)
}

func New(config Config) *Client {
	if config.Clock == nil {
		config.Clock = wallClock{}
	}
	dialUnix := config.DialContext
	if dialUnix == nil {
		dialUnix = (&net.Dialer{}).DialContext
	}
	return &Client{clock: config.Clock, dialUnix: dialUnix, dial: config.DialContext}
}

// Socket is the private WebSocket transport of a shared app-server.
type Socket struct {
	client *Client
	ctx    context.Context
	socket *websocket.Conn
}

func (c *Client) Dial(ctx context.Context, endpoint string) (*Socket, error) {
	target, client, err := c.Endpoint(endpoint)
	if err != nil {
		return nil, err
	}
	dctx, cancel := c.clock.WithTimeout(ctx, 10*time.Second)
	socket, response, err := websocket.Dial(dctx, target, &websocket.DialOptions{HTTPClient: client})
	cancel()
	if response != nil && response.Body != nil {
		response.Body.Close()
	}
	if err != nil {
		return nil, err
	}
	socket.SetReadLimit(64 << 20)
	return &Socket{client: c, ctx: ctx, socket: socket}, nil
}

func (s *Socket) Bind(ended <-chan struct{}, notify func(Message)) *Wire {
	return s.client.Bind(func(b []byte) error {
		ctx, cancel := s.client.clock.WithTimeout(s.ctx, 10*time.Second)
		defer cancel()
		return s.socket.Write(ctx, websocket.MessageText, b)
	}, ended, notify)
}

func (s *Socket) Read(ctx context.Context, receive func(Message)) {
	for {
		_, b, err := s.socket.Read(ctx)
		if err != nil {
			return
		}
		var m Message
		if json.Unmarshal(b, &m) == nil {
			receive(m)
		}
	}
}

func (s *Socket) CloseNow() { _ = s.socket.CloseNow() }

// Watch runs the initial and periodic work for a connected app-server.
func (c *Client) Watch(ctx context.Context, every time.Duration, beforeWait func(), refresh func() error) {
	tick := c.clock.Ticker(every)
	defer tick.Stop()
	for {
		beforeWait()
		select {
		case <-ctx.Done():
			return
		case <-tick.Ticks():
			if refresh() != nil {
				return
			}
		}
	}
}
