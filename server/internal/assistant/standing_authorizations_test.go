package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/prompts"
)

func standingRegistryGrant() StandingAuthorization {
	return StandingAuthorization{MachineID: "box", Directory: "/code/example", Actions: []string{"merge"}, Conditions: []string{"ci_green", "no_conflicts"}, Quote: "Merge the example project when checks pass and there are no conflicts.", Source: "main conversation, user message", Reversible: true}
}
func standingJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStandingAuthorizationRejectsBroadOrMutatedGrants(t *testing.T) {
	for _, dir := range []string{"/", "/code", "~/code/example", "relative/path", "/code/*", "/code/app/../other", "/code/example/"} {
		grant := standingRegistryGrant()
		grant.Directory = dir
		if _, err := prepareStandingChange(StandingAuthorizations{}, StandingAuthorizations{Authorizations: []StandingAuthorization{grant}}, time.Now()); err == nil {
			t.Errorf("broad/ambiguous: %q", dir)
		}
	}
	for _, field := range []string{"machine", "actions", "quote", "source"} {
		grant := standingRegistryGrant()
		switch field {
		case "machine":
			grant.MachineID = ""
		case "actions":
			grant.Actions = []string{"all"}
		case "quote":
			grant.Quote = ""
		case "source":
			grant.Source = ""
		}
		if err := checkStandingGrant(grant); err == nil {
			t.Errorf("invalid %s", field)
		} else if field == "actions" && !strings.Contains(err.Error(), "merge, push, deploy-test, deploy-production, send, delete, spend") {
			// persona_settings' description no longer lists them: the refusal says which there are.
			t.Errorf("the refusal does not name the supported actions: %v", err)
		}
	}
	grant := standingRegistryGrant()
	grant.ID = "sa_existing"
	grant.GrantedAt = time.Now()
	before := StandingAuthorizations{Revision: 1, Authorizations: []StandingAuthorization{grant}}
	grant.Actions = []string{"merge", "deploy-production"}
	if _, err := prepareStandingChange(before, StandingAuthorizations{Revision: 1, Authorizations: []StandingAuthorization{grant}}, time.Now()); err == nil {
		t.Fatal("existing grant silently expanded")
	}
}
func TestGoldenStandingAuthorizationSettingsDescription(t *testing.T) {
	golden(t, "standing_authorizations.settings_description", prompts.Tool("persona_settings").Description)
}

func TestStandingAuthorizationsDirectApplyInMainAndRefusedElsewhere(t *testing.T) {
	ctx := context.Background()
	f := newFakeHub().withMachine()
	s := newServiceOn(t, f, &ownerActionTestStore{})
	if _, err := s.updateSettings(ctx, func(v *Settings) error {
		v.MachineID = "server"
		v.Main = "main-conv"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	payload := StandingAuthorizations{Authorizations: []StandingAuthorization{standingRegistryGrant()}}
	args, _ := json.Marshal(map[string]any{
		"action": "set",
		"key":    standingAuthorizationsKey,
		"value":  payload,
	})
	// Non-main conversation is still refused.
	refused := s.Tool(ctx, ToolCall{Name: "persona_settings", MachineID: "server", SessionID: "other-conv", Arguments: args})
	if !refused.IsError || !strings.Contains(resultText(refused), "main conversation") || len(s.pendingQuestions()) != 0 {
		t.Fatalf("non-main standing authorizations: %+v (pending=%d)", refused, len(s.pendingQuestions()))
	}
	// Main conversation applies directly without an approval card.
	applied := s.Tool(ctx, ToolCall{Name: "persona_settings", MachineID: "server", SessionID: "main-conv", Arguments: args})
	if applied.IsError || len(s.pendingQuestions()) != 0 {
		t.Fatalf("main standing authorizations: %+v (pending=%d)", applied, len(s.pendingQuestions()))
	}
	if text := resultText(applied); !strings.Contains(text, "/code/example") || !strings.Contains(text, `"revision":1`) {
		t.Fatalf("main standing authorizations output = %q", text)
	}
	got, err := s.standingAuthorizations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Authorizations) != 1 || got.Authorizations[0].Directory != "/code/example" {
		t.Fatalf("stored standing authorizations = %+v", got)
	}
}
