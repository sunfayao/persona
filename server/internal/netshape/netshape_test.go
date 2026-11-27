package netshape

import (
	"errors"
	"io"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// closeBoth closes both ends when the test ends.
func closeBoth(t *testing.T, a, b net.Conn) {
	t.Cleanup(func() { a.Close(); b.Close() })
}

// A small write on an idle link is read the delay later, each direction its own.
func TestLatencyIsTheDelay(t *testing.T) {
	a, b := Pipe(Config{Rate: 30_000, Delay: 100 * time.Millisecond}, Config{Rate: 30_000, Delay: 30 * time.Millisecond})
	closeBoth(t, a, b)
	for _, c := range []struct {
		from, to net.Conn
		delay    time.Duration
	}{{a, b, 100 * time.Millisecond}, {b, a, 30 * time.Millisecond}, {a, b, 100 * time.Millisecond}} {
		start := time.Now()
		if _, err := c.from.Write([]byte("0123456789abcdef")); err != nil {
			t.Fatal(err)
		}
		buf := make([]byte, 16)
		if _, err := io.ReadFull(c.to, buf); err != nil {
			t.Fatal(err)
		}
		if took := time.Since(start); took < c.delay || took > c.delay+40*time.Millisecond { // timers on a busy machine
			t.Fatalf("16 bytes took %s, want %s", took, c.delay)
		}
	}
}

// With nobody reading, writes stop once the queue, what is on the way and the reader's share are full; reading lets
// them go on.
func TestWriteBlocksWhenTheQueueIsFull(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const queue = 16 << 10
		a, b := Pipe(Config{Rate: 1_000_000, Delay: 10 * time.Millisecond, Queue: queue}, Config{})
		var written atomic.Int64
		done := make(chan error, 1)
		go func() {
			piece := make([]byte, 1<<10)
			for {
				n, err := a.Write(piece)
				written.Add(int64(n))
				if err != nil {
					done <- err
					return
				}
			}
		}()
		time.Sleep(time.Second) // sleep-ok: synctest's fake clock, no real wait
		synctest.Wait()
		stalled := written.Load()
		time.Sleep(time.Second) // sleep-ok: synctest's fake clock, no real wait
		synctest.Wait()
		// The queue and the reader's share, what is on the way (rate × delay) and a piece: well under 64 KiB.
		if now := written.Load(); now != stalled || stalled < 2*queue || stalled > 64<<10 {
			t.Fatalf("wrote %d then %d bytes with nobody reading, want a stop between %d and %d", stalled, now, 2*queue, 64<<10)
		}
		if _, err := io.ReadFull(b, make([]byte, 32<<10)); err != nil {
			t.Fatal(err)
		}
		synctest.Wait()
		if written.Load() == stalled {
			t.Fatal("reading did not let the writer go on")
		}
		a.Close()
		b.Close()
		if err := <-done; !errors.Is(err, net.ErrClosed) {
			t.Fatalf("blocked write ended with %v, want net.ErrClosed", err)
		}
	})
}

func TestClose(t *testing.T) {
	cfg := Config{Rate: 30_000, Delay: 50 * time.Millisecond}

	t.Run("delivers what was written, then EOF", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, b := Pipe(cfg, cfg)
			defer b.Close()
			if _, err := a.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Write([]byte("more")); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("write after Close: %v, want net.ErrClosed", err)
			}
			if _, err := a.Read(make([]byte, 1)); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("read after Close: %v, want net.ErrClosed", err)
			}
			if err := a.Close(); !errors.Is(err, net.ErrClosed) {
				t.Fatalf("second Close: %v, want net.ErrClosed", err)
			}
			start := time.Now()
			got, err := io.ReadAll(b)
			if err != nil || string(got) != "hello" || time.Since(start) < cfg.Delay {
				t.Fatalf("read %q, %v after %s; want hello, EOF after the delay", got, err, time.Since(start))
			}
			if _, err := b.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("write to a closed end: %v, want io.ErrClosedPipe", err)
			}
		})
	})

	t.Run("ends a blocked read", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, b := Pipe(cfg, cfg)
			defer a.Close()
			got := make(chan error, 1)
			go func() {
				_, err := b.Read(make([]byte, 10))
				got <- err
			}()
			synctest.Wait()
			b.Close()
			if err := <-got; !errors.Is(err, net.ErrClosed) {
				t.Fatalf("read ended with %v, want net.ErrClosed", err)
			}
		})
	})

	for _, end := range []string{"writer", "reader"} {
		t.Run("ends a blocked write when the "+end+" closes", func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				a, b := Pipe(Config{Rate: 1_000, Queue: 1 << 10}, cfg)
				got := make(chan error, 1)
				go func() {
					_, err := a.Write(make([]byte, 1<<20))
					got <- err
				}()
				synctest.Wait()
				want := net.ErrClosed
				if end == "writer" {
					a.Close()
					defer b.Close()
				} else {
					b.Close()
					defer a.Close()
					want = io.ErrClosedPipe
				}
				if err := <-got; !errors.Is(err, want) {
					t.Fatalf("write ended with %v, want %v", err, want)
				}
			})
		})
	}

	t.Run("deadlines", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			a, b := Pipe(Config{Rate: 1_000, Queue: 1 << 10}, cfg)
			defer a.Close()
			defer b.Close()
			start := time.Now()
			b.SetReadDeadline(start.Add(50 * time.Millisecond))
			if _, err := b.Read(make([]byte, 1)); !errors.Is(err, os.ErrDeadlineExceeded) || time.Since(start) != 50*time.Millisecond {
				t.Fatalf("read with a deadline: %v after %s", err, time.Since(start))
			}
			a.SetWriteDeadline(time.Now().Add(50 * time.Millisecond))
			// The queue, then what the bottleneck sent in 50 ms.
			if n, err := a.Write(make([]byte, 1<<20)); !errors.Is(err, os.ErrDeadlineExceeded) || n < 1<<10 || n > 1<<10+100 {
				t.Fatalf("write with a deadline: %d bytes, %v", n, err)
			}
		})
	})
}

// Closing both ends stops every goroutine a Pipe started, blocked writes included: synctest.Test fails on any left
// in its bubble.
func TestCloseStopsTheGoroutines(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		for range 10 {
			a, b := Pipe(Config{Rate: 1_000_000, Delay: time.Millisecond}, Config{Delay: time.Millisecond})
			go a.Write(make([]byte, 1<<20)) // left blocked: nobody reads b
			if _, err := b.Write([]byte("x")); err != nil {
				t.Fatal(err)
			}
			if _, err := a.Read(make([]byte, 1)); err != nil {
				t.Fatal(err)
			}
			synctest.Wait()
			a.Close()
			b.Close()
		}
	})
}
