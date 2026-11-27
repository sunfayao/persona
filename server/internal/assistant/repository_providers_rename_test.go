package assistant

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// The approval card of an extension provider's repository shows what approving saves: the provider, the name it
// chose and the clone address; a built-in one still shows the Agent's input as it was.
func TestRepositoryAddCardShowsWhatAProviderChose(t *testing.T) {
	s, _, _ := acmeService(t, ruleStore{&memStore{}})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	allow := true
	add := func(args string) (json.RawMessage, RepositoryBinding) {
		t.Helper()
		var shown json.RawMessage
		done := answerRepositoryCard(t, s, proto.Resolution{Allow: &allow}, func(card proto.Interaction) { shown = card.ToolInput })
		out := repositoryAddCall(ctx, s, "repository_add", args)
		<-done
		var saved RepositoryBinding
		if out.IsError || json.Unmarshal([]byte(out.Content[0].Text), &saved) != nil {
			t.Fatalf("add %s: %+v", args, out)
		}
		return shown, saved
	}
	shown, saved := add(`{"url":"https://code.acme.test/team/app/reviews/3","purpose":"Demo"}`)
	var card map[string]any
	if err := json.Unmarshal(shown, &card); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"url": "https://code.acme.test/team/app", "provider": acmeProviderID, "provider_name": "Acme Code", "name": "App",
		"aliases": nil, "purpose": "Demo", "clone_url": "git@ssh.acme.test:team/app.git"}
	if len(card) != len(want) {
		t.Fatalf("card %s", shown)
	}
	for k, v := range want {
		if card[k] != v {
			t.Errorf("card %s: %v, want %v", k, card[k], v)
		}
	}
	if saved.Name != "App" || saved.CloneURL != "git@ssh.acme.test:team/app.git" || saved.ProviderName != "Acme Code" || saved.Provider != acmeProviderID {
		t.Fatalf("saved %+v", saved)
	}
	shown, _ = add(repoExample)
	var in repositoryInput
	if err := json.Unmarshal([]byte(repoExample), &in); err != nil {
		t.Fatal(err)
	}
	if raw, _ := json.Marshal(in); string(shown) != string(raw) {
		t.Fatalf("built-in card %s, want %s", shown, raw)
	}
}
