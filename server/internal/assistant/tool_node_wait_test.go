package assistant

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
)

// The error says when the computer's node last reported, as the resource sample has it.
func TestNodeSilentErrorSaysTheLastReport(t *testing.T) {
	s, _ := newQuietService(t)
	d := &resourceDevs{fleetDevs: newFleetDevs(), samples: map[string]*proto.MetricSample{
		"mini": {T: time.Now().Add(-27 * time.Minute).Unix()},
	}}
	d.add("mini", "Mac mini")
	s.devs = d
	err := s.nodeSilent("mini", DeviceSessionItems, context.DeadlineExceeded)
	if got := err.Error(); !strings.Contains(got, "the node on Mac mini did not answer within 20s") || !strings.Contains(got, "last report was 27 min ago") {
		t.Fatalf("error: %q", got)
	}
	if !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, rpc.ErrNotSent) || !sendUnknown(err) {
		t.Fatalf("kind: %v", err)
	}
	if sendUnknown(s.nodeSilent("mini", DeviceSessionSend, rpc.ErrNotSent)) {
		t.Fatal("a request that never went out counts as one that may have arrived")
	}
	if got := s.nodeSilent("gone", DeviceSessionItems, context.DeadlineExceeded).Error(); !strings.Contains(got, "no report from it is known") {
		t.Fatalf("error without a sample: %q", got)
	}
}

// Only a model's tool call is bounded, only for the methods listed, and never past a deadline its own code chose (a
// send that resumes a closed agent waits minutes).
func TestToolNodeContextBounds(t *testing.T) {
	plain := context.Background()
	if _, cancel, bounded := toolNodeContext(plain, DeviceSessionItems); bounded {
		cancel()
		t.Fatal("a call that is not a tool's was bounded")
	}
	tool := asToolCall(plain)
	if _, cancel, bounded := toolNodeContext(tool, DeviceSessionStart); bounded {
		cancel()
		t.Fatal("an agent's start was bounded")
	}
	ctx, cancel, bounded := toolNodeContext(tool, DeviceSessionItems)
	defer cancel()
	if d, ok := ctx.Deadline(); !bounded || !ok || time.Until(d) > toolNodeWait[DeviceSessionItems] {
		t.Fatalf("session.items: bounded=%v deadline=%v", bounded, d)
	}
	long, lcancel := context.WithTimeout(tool, 5*time.Minute)
	defer lcancel()
	if got, c, bounded := toolNodeContext(long, DeviceSessionSend); bounded || got != long {
		c()
		t.Fatal("a tool's own longer deadline was cut short")
	}
}
