package usagewindows

import (
	"context"
	"time"
)

// Probe gives each attempt its own deadline and retries once, only when attempt reports a transient failure.
func Probe(ctx context.Context, timeout, backoff time.Duration, attempt func(context.Context) bool) {
	for i := 0; i < 2 && ctx.Err() == nil; i++ {
		if i > 0 {
			timer := time.NewTimer(backoff)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return
			}
		}
		read, cancel := context.WithTimeout(ctx, timeout)
		retry := attempt(read)
		cancel()
		if !retry {
			return
		}
	}
}
