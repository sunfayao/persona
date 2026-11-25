package assistant

import (
	"context"
)

// stuckHealthStore is a database whose launcher health setting never answers a write.
type stuckHealthStore struct{ *fleetMem }
