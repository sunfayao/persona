package usagewindows

import (
	"context"
	"testing"
	"time"
)

func TestProbeRetriesTransientFailureOnceAndStopsOnCancellation(t *testing.T) {
	for _, test := range []struct {
		name   string
		retry  bool
		cancel bool
		want   int
	}{
		{name: "transient", retry: true, want: 2},
		{name: "conclusive", want: 1},
		{name: "cancelled", retry: true, cancel: true, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			count := 0
			Probe(ctx, 15*time.Second, 0, func(attempt context.Context) bool {
				count++
				if deadline, ok := attempt.Deadline(); !ok || time.Until(deadline) > 15*time.Second {
					t.Fatalf("attempt deadline = %v, set %v", deadline, ok)
				}
				if test.cancel {
					cancel()
				}
				return test.retry
			})
			if count != test.want {
				t.Errorf("attempts = %d, want %d", count, test.want)
			}
		})
	}
}
