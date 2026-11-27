package hub

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestShutdownTotalBudget(t *testing.T) {
	budget := newShutdownBudget(&Config{})
	budget.timeout = 40 * time.Millisecond
	defer budget.close()
	ctx, cancel := context.WithCancel(context.Background())
	blocked := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		<-budget.started
		cancel()
	}()
	err := budget.run(ctx, func() error {
		defer close(finished)
		budget.start()
		<-blocked // Simulate cleanup that ignores cancellation.
		return nil
	})
	close(blocked)
	<-finished
	if err == nil || !strings.Contains(err.Error(), "total budget") {
		t.Fatalf("unbounded cleanup was not reported: %v", err)
	}
}
