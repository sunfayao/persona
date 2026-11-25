package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

// #7391: a relay hop is a request; the first message back, its answer, keeps the hop.

// forgetRate drops the per-pair and per-sender counts, as ten minutes going by does.
func forgetRate(s *Service) {
	s.relays.mu.Lock()
	s.relays.sent = map[[2]string][]time.Time{}
	s.relays.mu.Unlock()
}





