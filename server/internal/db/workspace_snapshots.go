package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// ErrInvalidWorkspaceSnapshot is a workspace snapshot the store refuses to keep.
var ErrInvalidWorkspaceSnapshot = errors.New("invalid workspace snapshot")

const maxWorkspaceMachineIDBytes = 1024

type WorkspaceSnapshotRow struct {
	MachineID string
	Data      json.RawMessage
	UpdatedAt time.Time
}

func (d *DB) ListWorkspaceSnapshots(ctx context.Context) ([]WorkspaceSnapshotRow, error) {
	rows, err := d.pool.Query(ctx, `SELECT machine_id, data, updated_at FROM workspace_snapshots ORDER BY machine_id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (WorkspaceSnapshotRow, error) {
		var s WorkspaceSnapshotRow
		err := r.Scan(&s.MachineID, &s.Data, &s.UpdatedAt)
		return s, err
	})
}

func (d *DB) SaveWorkspaceSnapshot(ctx context.Context, machineID string, data json.RawMessage) error {
	if strings.TrimSpace(machineID) == "" || strings.TrimSpace(machineID) != machineID || len(machineID) > maxWorkspaceMachineIDBytes || !utf8.ValidString(machineID) || strings.ContainsFunc(machineID, unicode.IsControl) || len(data) > 4<<20 || !json.Valid(data) {
		return fmt.Errorf("%w", ErrInvalidWorkspaceSnapshot)
	}
	_, err := d.pool.Exec(ctx, `INSERT INTO workspace_snapshots (machine_id, data) VALUES ($1, $2)
  ON CONFLICT (machine_id) DO UPDATE SET data = EXCLUDED.data, updated_at = now()`, machineID, []byte(data))
	return err
}
