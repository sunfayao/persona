package node

import (
	"context"
	"time"
)

func (n *Node) notifyCodex() {
	if n.codices == nil {
		return
	}
	select {
	case n.codices.kick <- struct{}{}:
	default:
	}
}

func (n *Node) publishCodex(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-n.codices.kick:
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		for len(n.codices.kick) > 0 {
			<-n.codices.kick
		}
		n.notifySessions()
	}
}
