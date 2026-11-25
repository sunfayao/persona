package db

import (
	"context"
	"testing"
	"time"
)

func TestMeshGroupAndConnectors(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `TRUNCATE mesh_group, connector_credentials`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := d.MeshGroup(ctx, "hub-self"); err != nil || ok {
		t.Fatalf("empty group: ok=%v err=%v", ok, err)
	}
	if err := d.SaveMeshGroup(ctx, "hub-self", "hub-b", 1); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveMeshGroup(ctx, "hub-self", "hub-a", 2); err != nil {
		t.Fatal(err)
	}
	if c, term, ok, err := d.MeshGroup(ctx, "hub-self"); err != nil || !ok || c != "hub-a" || term != 2 {
		t.Fatalf("group %q %d %v %v", c, term, ok, err)
	}
	// Another hub on the same database (a restored backup, a shared test database) has no group from it.
	if _, _, ok, err := d.MeshGroup(ctx, "hub-other"); err != nil || ok {
		t.Fatalf("another hub's group leaked: ok=%v err=%v", ok, err)
	}
	if _, err := d.pool.Exec(ctx, `TRUNCATE mesh_group`); err != nil { // leave nothing for other tests on this database
		t.Fatal(err)
	}

	if c, err := d.ConnectorCredential(ctx, "example-workstation"); err != nil || c != nil {
		t.Fatalf("no credential yet: %+v %v", c, err)
	}
	if err := d.SaveConnectorCredential(ctx, ConnectorCredential{MachineID: "example-workstation", FP: "fp1", Token: "sealed1", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveConnectorCredential(ctx, ConnectorCredential{MachineID: "example-workstation", FP: "fp2", Token: "sealed2", Version: "v2"}); err != nil {
		t.Fatal(err)
	}
	if c, err := d.ConnectorCredential(ctx, "example-workstation"); err != nil || c == nil || c.FP != "fp2" || c.Token != "sealed2" || c.Version != "v2" {
		t.Fatalf("replaced credential %+v %v", c, err)
	}
	if ok, err := d.DeleteConnectorCredential(ctx, "example-workstation"); err != nil || !ok {
		t.Fatalf("delete %v %v", ok, err)
	}
	if ok, _ := d.DeleteConnectorCredential(ctx, "example-workstation"); ok {
		t.Fatal("deleted twice")
	}
}

func TestAppTokensMoveToTheCenter(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id LIKE 'dev_ct_%'`); err != nil {
		t.Fatal(err)
	}
	for _, x := range []struct {
		t    DeviceToken
		hash string
	}{
		{DeviceToken{ID: "dev_ct_phone", Name: "iPhone", Kind: "phone", Role: "full"}, "ct-hash-phone"},
		{DeviceToken{ID: "dev_ct_watch", Name: "Watch", Kind: "watch", Role: "answer"}, "ct-hash-watch"},
		{DeviceToken{ID: "dev_ct_link", Name: "link", Kind: "link", Role: "link", MachineID: "mac"}, "ct-hash-link"},
		{DeviceToken{ID: "dev_ct_gone", Name: "old", Kind: "phone", Role: "full"}, "ct-hash-gone"},
	} {
		if err := d.CreateDeviceToken(ctx, x.t, x.hash); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.RevokeDeviceToken(ctx, "dev_ct_gone"); err != nil {
		t.Fatal(err)
	}
	list, err := d.AppTokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine []ExportedToken
	for _, e := range list {
		if len(e.ID) > 7 && e.ID[:7] == "dev_ct_" {
			mine = append(mine, e)
		}
	}
	if len(mine) != 2 || mine[0].Hash == "" {
		t.Fatalf("exported %+v (want the phone and the watch, with hashes)", mine)
	}
	// Taken over elsewhere: here, the same rows are already there (nothing new); under new ids, they are added.
	if n, err := d.ImportDeviceTokens(ctx, mine); err != nil || n != 0 {
		t.Fatalf("import of known ones: %d %v", n, err)
	}
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id IN ('dev_ct_phone', 'dev_ct_watch')`); err != nil {
		t.Fatal(err)
	}
	if n, err := d.ImportDeviceTokens(ctx, mine); err != nil || n != 2 {
		t.Fatalf("import: %d %v", n, err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "ct-hash-watch"); tok == nil || tok.Role != "answer" || tok.ID != "dev_ct_watch" {
		t.Fatalf("imported %+v", tok)
	}
}

// A revocation that arrives while the credential's import is still open waits for it and revokes it.
func TestRevocationDuringImportWins(t *testing.T) {
	d := open(t)
	ctx := context.Background()
	if _, err := d.pool.Exec(ctx, `DELETE FROM device_tokens WHERE id = 'dev_rv_race'`); err != nil {
		t.Fatal(err)
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO device_tokens (id, name, kind, role, token_hash) VALUES ('dev_rv_race', 'p', 'phone', 'full', 'rv-hash-race')`); err != nil {
		t.Fatal(err)
	}
	type result struct {
		ids []string
		err error
	}
	done := make(chan result, 1)
	go func() {
		ids, err := d.ApplyRevokedTokens(ctx, []ExportedToken{{DeviceToken: DeviceToken{ID: "dev_rv_race", Name: "p", Kind: "phone", Role: "full"}, Hash: "rv-hash-race"}})
		done <- result{ids, err}
	}()
	time.Sleep(200 * time.Millisecond) // it is waiting on the open insert by now
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.err != nil {
		t.Fatal(r.err)
	}
	if tok, _ := d.DeviceTokenByHash(ctx, "rv-hash-race"); tok != nil {
		t.Fatalf("the credential came through its revocation: %+v (newly revoked: %v)", tok, r.ids)
	}
}
