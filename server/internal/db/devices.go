package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- devices ----

type Device struct {
	DeviceID     string `json:"device_id"`
	CredentialID string `json:"-"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	// Locale: the interface language it registered with (zh-Hans / en / ja, docs/protocol/i18n.md); "" is the user's.
	Locale string `json:"locale,omitempty"`
	// Credential: the device token (device_tokens.id) that wrote its settings, set by the hub, never from the request;
	// empty for the pairing token. Revoking it forgets the device (hub: Pusher.signedOut).
	Credential string    `json:"-"`
	UpdatedAt  time.Time `json:"updated_at"`
	// Prefs is the device's notification settings as JSON (hub decodes it); empty = defaults. Only SetDevicePrefs
	// writes it.
	Prefs json.RawMessage `json:"-"`
}

func (d *DB) Devices(ctx context.Context) ([]Device, error) {
	rows, err := d.pool.Query(ctx, `SELECT device_id, name, kind, credential, updated_at, prefs, credential_id, locale FROM devices`)
	if err != nil {
		return nil, err
	}
	return collectDevices(rows)
}

func collectDevices(rows pgx.Rows) ([]Device, error) {
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Device, error) {
		var v Device
		var prefs []byte
		err := r.Scan(&v.DeviceID, &v.Name, &v.Kind, &v.Credential, &v.UpdatedAt, &prefs, &v.CredentialID, &v.Locale)
		if len(prefs) > 0 {
			v.Prefs = prefs
		}
		return v, err
	})
}

// SetDevicePrefs stores a device's notification settings, creating the device's row when it has none (a browser's).
// credential is the device token that wrote them: a row without one takes it, so revoking that token (Pusher.signedOut)
// also removes a row only prefs ever made; registering sets it as usual.
func (d *DB) SetDevicePrefs(ctx context.Context, id, credential string, prefs json.RawMessage) error {
	_, err := d.pool.Exec(ctx, `INSERT INTO devices (device_id, name, kind, apns_token, apns_env, topic, updated_at, prefs, credential)
		VALUES ($1, '', '', '', '', '', now(), $2, $3)
		ON CONFLICT (device_id) DO UPDATE SET prefs = $2,
			credential = CASE WHEN devices.credential = '' THEN EXCLUDED.credential ELSE devices.credential END`, id, []byte(prefs), credential)
	// A revoked credential's device id is fenced.
	return pushRegistrationConflict(err)
}

// DeleteDevice forgets a device (its credential was revoked).
func (d *DB) DeleteDevice(ctx context.Context, id string) error {
	return pgx.BeginFunc(ctx, d.pool, func(tx pgx.Tx) error {
		if err := lockPushRegistrations(ctx, tx); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM devices WHERE device_id=$1 FOR UPDATE`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT set_config('persona.push_retire',$1,true)`, id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `DELETE FROM devices WHERE device_id=$1`, id)
		return err
	})
}
