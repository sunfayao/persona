package db

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// The ledger of what was written as the user (migrations/0420_sent_as_user.sql).

// SentAsUser is one write that went out through a connector's gate.
type SentAsUser struct {
	ID        int64     `json:"id"`
	At        time.Time `json:"at"`
	Connector string    `json:"connector"`
	Action    string    `json:"action"`
	// Target is where it went, by connector; Origin the conversation that asked; LetThrough how the gate let it
	// through (all JSON objects).
	Target     json.RawMessage `json:"target"`
	Text       string          `json:"text"`
	Summary    string          `json:"summary"`
	Origin     json.RawMessage `json:"origin"`
	LetThrough json.RawMessage `json:"let_through"`
	MessageRef string          `json:"message_ref,omitempty"`
	RecalledAt *time.Time      `json:"recalled_at,omitempty"`
}

// RecordSentAsUser adds a row (At: now when zero) and returns its id. An empty JSON field is an empty object.
func (d *DB) RecordSentAsUser(ctx context.Context, s SentAsUser) (int64, error) {
	if s.At.IsZero() {
		s.At = time.Now()
	}
	var id int64
	err := d.pool.QueryRow(ctx, `INSERT INTO sent_as_user (at, connector, action, target, text, summary, origin, let_through, message_ref)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		s.At, s.Connector, s.Action, objectOrEmpty(s.Target), s.Text, s.Summary, objectOrEmpty(s.Origin), objectOrEmpty(s.LetThrough),
		s.MessageRef).Scan(&id)
	return id, err
}

func objectOrEmpty(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte(`{}`)
	}
	return raw
}

// sentAsUserColumns are a row's columns in the order scanSentAsUser reads them.
const sentAsUserColumns = `id, at, connector, action, target, text, summary, origin, let_through, message_ref, recalled_at`

func scanSentAsUser(r pgx.Row) (SentAsUser, error) {
	var s SentAsUser
	var target, origin, through []byte
	err := r.Scan(&s.ID, &s.At, &s.Connector, &s.Action, &target, &s.Text, &s.Summary, &origin, &through, &s.MessageRef, &s.RecalledAt)
	s.Target, s.Origin, s.LetThrough = target, origin, through
	return s, err
}

const sentAsUsersForArchiveSQL = `SELECT ` + sentAsUserColumns + ` FROM sent_as_user
	WHERE connector = $1 AND at >= $2 AND at < $3 ORDER BY at, id`

// SentAsUsersForArchive reads one connector's ledger in [since, until), oldest first.
// Archive matching needs every candidate, including recalled messages still present in the archive.
func (d *DB) SentAsUsersForArchive(ctx context.Context, connector string, since, until time.Time) ([]SentAsUser, error) {
	rows, err := d.pool.Query(ctx, sentAsUsersForArchiveSQL, connector, since, until)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SentAsUser, error) { return scanSentAsUser(r) })
}

// PruneSentAsUser deletes every row written before before and says how many there were.
func (d *DB) PruneSentAsUser(ctx context.Context, before time.Time) (int64, error) {
	tag, err := d.pool.Exec(ctx, `DELETE FROM sent_as_user WHERE at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
