package main

import (
	"strings"
	"testing"
)

func TestSubcommandHelp(t *testing.T) {
	for sub, want := range map[string]string{"install": "--no-node", "node": "personad-node", "mode": "两种模式的配对", "status": "退出码 0，否则 1",
		"log": "persona-link logs ", "address": "address remove"} {
		got := subcommandHelp(sub)
		if !strings.Contains(got, want) || got == usage() || strings.Contains(got, "persona-link pair ") {
			t.Errorf("%s: %q", sub, got)
		}
	}
	if got := subcommandHelp("supervise"); got != usage() {
		t.Errorf("supervise has no entry, wants the whole usage: %q", got)
	}
}
