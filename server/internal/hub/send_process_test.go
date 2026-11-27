package hub

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/sunfayao/persona/server/internal/db"
	"github.com/sunfayao/persona/server/internal/db/dbtest"
)

func TestSendOnceProcessHelper(t *testing.T) {
	phase, outcome := os.Getenv("PERSONA_SEND_PHASE"), os.Getenv("PERSONA_SEND_OUTCOME")
	if phase == "" {
		t.Skip("subprocess helper")
	}
	ctx := context.Background()
	database, err := db.Open(ctx, os.Getenv(dbtest.Env), db.HubSet)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	h := newTestHub(t)
	h.store = database
	started := make(chan struct{})
	release := make(chan struct{})
	defer close(release)
	peer := pipeNode(t, h, "box", func(context.Context) error {
		f, err := os.OpenFile(os.Getenv("PERSONA_SEND_EFFECTS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString("typed\n")
		_ = f.Close()
		if err != nil {
			return err
		}
		if outcome == "sending" {
			os.Exit(23)
		}
		if outcome == "unknown" {
			close(started)
			<-release
			return io.ErrUnexpectedEOF
		}
		return nil
	})
	// Break the RPC after the side effect but before its acknowledgement.
	if phase == "first" && outcome == "unknown" {
		go func() { <-started; peer.Close(io.ErrUnexpectedEOF) }()
	}
	err = h.sendOnce(ctx, h.byID["box"], "session.send", map[string]any{"session_id": "s1", "client_msg_id": "model-retry:s1:1:1", "text": "Continue"}, "")
	if outcome == "delivered" && err != nil {
		t.Fatal(err)
	}
	if outcome != "delivered" && err == nil {
		t.Fatal("uncertain delivery reported success")
	}
	state, claimErr := database.ClaimSend(ctx, "model-retry:s1:1:1", "box", "s1")
	if claimErr != nil || state != outcome {
		t.Fatalf("receipt=%q want %q: %v", state, outcome, claimErr)
	}
}
