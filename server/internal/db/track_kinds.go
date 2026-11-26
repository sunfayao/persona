package db

import (
	"context"
	"time"
)

// The user's track kinds (#13472, migrations/manager/20261123045348_assistant_track_kinds.sql): the built-in kinds
// are the manager's, never rows.

// TrackKind is one row of assistant_track_kinds.
type TrackKind struct {
	Slug  string
	Title string
	// Config is the kind's config (a JSON object), checked by the manager.
	Config    []byte
	Builtin   bool
	Revision  int64
	CreatedAt time.Time
}

// TrackKinds are the user's kinds, by slug.
func (d *DB) TrackKinds(ctx context.Context) ([]TrackKind, error) {
	rows, err := d.pool.Query(ctx, `SELECT slug, title, config, builtin, revision, created_at FROM assistant_track_kinds ORDER BY slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrackKind
	for rows.Next() {
		var k TrackKind
		if err := rows.Scan(&k.Slug, &k.Title, &k.Config, &k.Builtin, &k.Revision, &k.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}
