package db

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrPushRegistrationConflict = errors.New("push registration ownership changed or identity was superseded")

const pushRegistrationLock = int64(0x706572736f6e61)

func lockPushRegistrations(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, pushRegistrationLock)
	return err
}

// pushRegistrationConflict maps the device fence's refusals (migrations 0435 and 0541: a revoked credential's device
// row) to ErrPushRegistrationConflict, so callers can tell them apart from a database that is down.
func pushRegistrationConflict(err error) error {
	var pgerr *pgconn.PgError
	if errors.As(err, &pgerr) && pgerr.Code == "23514" && (pgerr.Message == "push registration was superseded" || pgerr.Message == "push registration ownership changed") {
		return ErrPushRegistrationConflict
	}
	return err
}

func devicesIn(ctx context.Context, tx pgx.Tx) ([]Device, error) {
	rows, err := tx.Query(ctx, `SELECT device_id,name,kind,credential,updated_at,prefs,credential_id,locale FROM devices ORDER BY registered_at DESC, device_id DESC FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	return collectDevices(rows)
}

// retireDevice deletes a revoked credential's device row and fences its id: the device-row triggers (migrations
// 0435 and 0541) accept the delete only from here, and refuse the id afterwards.
func retireDevice(ctx context.Context, tx pgx.Tx, old Device) error {
	owner := old.CredentialID
	if owner == "" {
		owner = old.Credential
	}
	if _, err := tx.Exec(ctx, `INSERT INTO push_device_retirements(device_id,canonical_id,credential_id,reason,push_key_hash,target_hash,topic,apns_env) VALUES($1,'',$2,'revoked','','','','')
		ON CONFLICT(device_id) DO UPDATE SET reason=EXCLUDED.reason`, old.DeviceID, owner); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_retire',$1,true)`, old.DeviceID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `DELETE FROM devices WHERE device_id=$1`, old.DeviceID)
	return err
}
