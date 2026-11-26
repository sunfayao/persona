package ports

import (
	"context"
	"time"

	"github.com/sunfayao/persona/server/internal/processtable"
)

// Clock supplies the sweep cadence, the time of the last scan and the probe deadline.
type Clock interface {
	Now() time.Time
	After(time.Duration) <-chan time.Time
	Ticker(time.Duration) Ticker
}

type Ticker interface {
	Ticks() <-chan time.Time
	Stop()
}

type wallClock struct{}

func (wallClock) Now() time.Time                         { return time.Now() }
func (wallClock) After(d time.Duration) <-chan time.Time { return time.After(d) }
func (wallClock) Ticker(d time.Duration) Ticker          { return wallTicker{time.NewTicker(d)} }

type wallTicker struct{ *time.Ticker }

func (t wallTicker) Ticks() <-chan time.Time { return t.C }

// Config describes the live panes to scan and how to publish port changes.
type Config struct {
	Panes         func() map[string]bool
	PaneProcesses func(context.Context, string) ([]int, error)
	PanesOfPID    func(int) []string
	Notify        func()
	Logf          func(string, ...any)
	Processes     func(context.Context) (map[int]processtable.Process, error)
	Clock         Clock
}

func New(config Config) *Service {
	if config.Clock == nil {
		config.Clock = wallClock{}
	}
	if config.Processes == nil {
		config.Processes = processtable.List
	}
	if config.Logf == nil {
		config.Logf = func(string, ...any) {}
	}
	return &Service{config: config, kick: make(chan struct{}, 1)}
}
