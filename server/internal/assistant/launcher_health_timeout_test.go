package assistant

import (
	"context"
)

// stuckHealthStore is a database whose launcher health setting never answers a write.
type stuckHealthStore struct{ *fleetMem }

func (s stuckHealthStore) PutSetting(ctx context.Context, key string, v any) error {
	if key == launchersHealthKey {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.fleetMem.PutSetting(ctx, key, v)
}
