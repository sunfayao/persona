package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/link"
)

func TestStatusReportsSSHRouteAndErrors(t *testing.T) {
	f := newFakeEnv(t)
	f.pairHere(t)
	if _, err := link.SetSSH(f.configPath(), &link.SSH{Server: "user@example.com", Target: "127.0.0.1:18662", Always: true}); err != nil {
		t.Fatal(err)
	}
	release, err := link.LockRun(f.configPath())
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	since := f.now()
	f.saveStatus(t, link.Status{
		State:   "connected",
		Mode:    link.ModeActive,
		Machine: "pi",
		HubName: "home",
		Address: "user@example.com",
		Kind:    link.KindSSH,
		Since:   &since,
		Node:    "online",
	}, 4242)

	f.out.Reset()
	if err := status(f.env, nil); err != nil {
		t.Fatalf("status text: %v", err)
	}
	txt := f.out.String()
	for _, want := range []string{
		"链路：已连接 · SSH user@example.com",
		"  · SSH user@example.com → 127.0.0.1:18662（在用，总是走 SSH）",
	} {
		if !strings.Contains(txt, want) {
			t.Fatalf("status output missing %q:\n%s", want, txt)
		}
	}

	f.out.Reset()
	if err := status(f.env, []string{"--json"}); err != nil {
		t.Fatalf("status --json: %v", err)
	}
	var rep report
	if err := json.Unmarshal(f.out.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.SSH == nil || rep.SSH.Server != "user@example.com" || rep.Link == nil || rep.Link.Kind != link.KindSSH {
		t.Fatalf("status json = %+v", rep)
	}
}
