package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// A row is read back as it was written; empty JSON fields come back as empty objects.
func TestSentAsUserRecord(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	at := time.Date(2026, 11, 6, 8, 0, 0, 0, time.UTC)
	id, err := d.RecordSentAsUser(ctx, SentAsUser{At: at, Connector: "github", Action: "github_comment",
		Target: json.RawMessage(`{"ref":"example-org/example-repo#12"}`), Text: "Looks good.", Summary: "comment on example-org/example-repo#12",
		Origin:     json.RawMessage(`{"machine_id":"mac","session_id":"s1"}`),
		LetThrough: json.RawMessage(`{"kind":"card","interaction_id":"ix_1"}`)})
	if err != nil || id == 0 {
		t.Fatalf("record: %d %v", id, err)
	}
	if _, err := d.RecordSentAsUser(ctx, SentAsUser{At: at.Add(time.Minute), Connector: "mail", Action: "mail_send"}); err != nil {
		t.Fatal(err)
	}
	rows, err := d.SentAsUsersForArchive(ctx, "github", at, at.Add(time.Hour))
	if err != nil || len(rows) != 1 || rows[0].ID != id {
		t.Fatalf("rows %+v: %v", rows, err)
	}
	empty, err := d.SentAsUsersForArchive(ctx, "mail", at, at.Add(time.Hour))
	if err != nil || len(empty) != 1 {
		t.Fatalf("mail rows %+v: %v", empty, err)
	}
	if string(empty[0].Target) != `{}` || string(empty[0].Origin) != `{}` || string(empty[0].LetThrough) != `{}` {
		t.Fatalf("empty fields: %s %s %s", empty[0].Target, empty[0].Origin, empty[0].LetThrough)
	}
	got := rows[0]
	var target struct{ Ref string }
	_ = json.Unmarshal(got.Target, &target)
	var through map[string]string
	_ = json.Unmarshal(got.LetThrough, &through)
	if !got.At.Equal(at) || got.Text != "Looks good." || got.Summary == "" || target.Ref != "example-org/example-repo#12" ||
		through["kind"] != "card" || through["interaction_id"] != "ix_1" || got.RecalledAt != nil || got.MessageRef != "" {
		t.Fatalf("read back %+v", got)
	}
}

func TestSentAsUsersForArchive(t *testing.T) {
	d, ctx := open(t), context.Background()
	// This local day starts on the previous UTC date. SQL compares instants, not dates.
	since := time.Date(2026, 11, 6, 0, 0, 0, 0, time.FixedZone("archive", 9*60*60))
	until := since.AddDate(0, 0, 1)
	record := func(connector string, at time.Time, text string) int64 {
		t.Helper()
		id, err := d.RecordSentAsUser(ctx, SentAsUser{At: at, Connector: connector, Action: "send",
			Target: json.RawMessage(`{"chat":"thread"}`), Text: text, MessageRef: "ref-" + text})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	record("example", since.Add(-time.Microsecond), "before")
	record("example", until, "until")
	record("other", since, "other connector")
	first := record("example", since.UTC(), "start")
	second := record("example", since.UTC(), "same time")
	last := record("example", until.Add(-time.Microsecond).UTC(), "end")
	// A complete day can contain hundreds of rows.
	const bulkRows = 201
	for i := bulkRows; i > 0; i-- {
		record("example", since.Add(time.Duration(i)*time.Minute), fmt.Sprint(i))
	}
	recalled := until.Add(time.Hour)
	if _, err := d.pool.Exec(ctx, `UPDATE sent_as_user SET recalled_at = $2 WHERE id = $1`, second, recalled); err != nil {
		t.Fatal(err)
	}
	rows, err := d.SentAsUsersForArchive(ctx, "example", since, until)
	if err != nil || len(rows) != bulkRows+3 {
		t.Fatalf("archive: %d rows, %v", len(rows), err)
	}
	if rows[0].ID != first || rows[1].ID != second || rows[len(rows)-1].ID != last {
		t.Fatalf("boundary and tie order: %+v", rows)
	}
	if rows[1].RecalledAt == nil || !rows[1].RecalledAt.Equal(recalled) || rows[1].MessageRef != "ref-same time" ||
		string(rows[1].Target) != `{"chat": "thread"}` {
		t.Fatalf("lost matching metadata: %+v", rows[1])
	}
	for i, row := range rows {
		if row.Connector != "example" || row.At.Before(since) || !row.At.Before(until) ||
			(i > 0 && row.At.Before(rows[i-1].At)) {
			t.Fatalf("wrong candidate at %d: %+v", i, row)
		}
	}
	for _, bounds := range [][2]time.Time{{since, since}, {until, since}} {
		if rows, err := d.SentAsUsersForArchive(ctx, "example", bounds[0], bounds[1]); err != nil || len(rows) != 0 {
			t.Fatalf("empty interval: %+v %v", rows, err)
		}
	}
	if rows, err := d.SentAsUsersForArchive(ctx, "", since, until); err != nil || len(rows) != 0 {
		t.Fatalf("an empty connector must not read every connector: %+v %v", rows, err)
	}
}

// A prepared query must use both connector and time in its index conditions after pgx starts using a generic plan.
func TestSentAsUsersForArchiveIndexed(t *testing.T) {
	d, ctx := open(t), context.Background()
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO sent_as_user (connector, action, at)
		SELECT 'example-' || (g % 40), 'send', '2026-11-06T00:00:00Z'::timestamptz - g * interval '1 minute'
		FROM generate_series(1, 6000) g`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `ANALYZE sent_as_user`); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `PREPARE archive_sends(text, timestamptz, timestamptz) AS `+sentAsUsersForArchiveSQL); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = tx.Exec(ctx, `DEALLOCATE archive_sends`) }()
	for _, mode := range []string{"force_custom_plan", "force_generic_plan"} {
		if _, err := tx.Exec(ctx, `SET LOCAL plan_cache_mode = `+mode); err != nil {
			t.Fatal(err)
		}
		rows, err := tx.Query(ctx, `EXPLAIN EXECUTE archive_sends('example-7', '2026-11-05T00:00:00Z', '2026-11-06T00:00:00Z')`)
		if err != nil {
			t.Fatal(err)
		}
		lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			t.Fatal(err)
		}
		plan := strings.Join(lines, "\n")
		if !strings.Contains(plan, "sent_as_user_connector_at") || strings.Contains(plan, "Seq Scan") {
			t.Fatalf("%s did not use the archive index:\n%s", mode, plan)
		}
	}
}

// Prune deletes what is older than the cut and nothing else.
func TestSentAsUserPrune(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	now := time.Now()
	for _, age := range []time.Duration{100 * 24 * time.Hour, 91 * 24 * time.Hour, 89 * 24 * time.Hour, time.Hour} {
		if _, err := d.RecordSentAsUser(ctx, SentAsUser{At: now.Add(-age), Connector: "github", Action: "github_comment", Text: age.String()}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := d.PruneSentAsUser(ctx, now.Add(-90*24*time.Hour))
	if err != nil || n != 2 {
		t.Fatalf("pruned %d %v", n, err)
	}
	rows, err := d.SentAsUsersForArchive(ctx, "github", now.Add(-100*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("left %+v", rows)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
