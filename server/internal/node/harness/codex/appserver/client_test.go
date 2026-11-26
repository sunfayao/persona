package appserver

import (
	"errors"
	"testing"
	"time"
)

type watchClock struct {
	wallClock
	ticker *watchTicker
}

func (c watchClock) Ticker(time.Duration) Ticker { return c.ticker }

type watchTicker struct {
	ticks   chan time.Time
	stopped bool
}

func (t *watchTicker) Ticks() <-chan time.Time { return t.ticks }
func (t *watchTicker) Stop()                   { t.stopped = true }

func TestWatchRunsRefreshOnTicksAndStopsOnFailure(t *testing.T) {
	t.Parallel()
	ticker := &watchTicker{ticks: make(chan time.Time, 2)}
	client := New(Config{Clock: watchClock{ticker: ticker}})
	var waits, refreshes int
	client.Watch(t.Context(), 5*time.Second, func() {
		waits++
		ticker.ticks <- time.Time{}
	}, func() error {
		refreshes++
		if refreshes == 2 {
			return errors.New("disconnected")
		}
		return nil
	})
	if waits != 2 || refreshes != 2 || !ticker.stopped {
		t.Fatalf("waits %d, refreshes %d, stopped %t", waits, refreshes, ticker.stopped)
	}
}
