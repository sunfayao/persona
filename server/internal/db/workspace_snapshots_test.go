package db

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestWorkspaceSnapshotsMachineScopedAndIndependent(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE workspace_snapshots`); err != nil {
		t.Fatal(err)
	}
	for machine, data := range map[string]json.RawMessage{"machine-b": json.RawMessage(`{"workspaces":[]}`), "machine-a": json.RawMessage(`{"workspaces":[{"id":"old"}]}`)} {
		if err := d.SaveWorkspaceSnapshot(ctx, machine, data); err != nil {
			t.Fatal(err)
		}
	}
	replacement := json.RawMessage(`{"workspaces":[{"id":"new"}]}`)
	if err := d.SaveWorkspaceSnapshot(ctx, "machine-a", replacement); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []json.RawMessage{nil, json.RawMessage(`{broken`)} {
		if err := d.SaveWorkspaceSnapshot(ctx, "machine-a", bad); err == nil {
			t.Fatal("invalid snapshot accepted")
		}
	}
	if err := d.SaveWorkspaceSnapshot(ctx, "", replacement); err == nil {
		t.Fatal("empty machine accepted")
	}
	rows, err := d.ListWorkspaceSnapshots(ctx)
	if err != nil || len(rows) != 2 || rows[0].MachineID != "machine-a" || rows[1].MachineID != "machine-b" || rows[0].UpdatedAt.IsZero() {
		t.Fatalf("snapshots: %+v %v", rows, err)
	}
	var got, want any
	if err := json.Unmarshal(rows[0].Data, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(replacement, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("replacement: %s", rows[0].Data)
	}
}

// statementLog records the first word of every statement a pool sends, to count a read's round trips.
type statementLog struct {
	mu  sync.Mutex
	sql []string
}

func (l *statementLog) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if words := strings.Fields(data.SQL); len(words) > 0 {
		l.mu.Lock()
		l.sql = append(l.sql, words[0])
		l.mu.Unlock()
	}
	return ctx
}

func (*statementLog) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (l *statementLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := l.sql
	l.sql = nil
	return out
}

// tracedDB is a handle on database url whose pool logs the statements it sends.
func tracedDB(t *testing.T, url string) (*DB, *statementLog) {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	log := &statementLog{}
	cfg.ConnConfig.Tracer = log
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return &DB{pool: pool}, log
}
