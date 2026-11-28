package assistant

import (
	"slices"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

func TestEnrichDiscoveredModelWindows(t *testing.T) {
	// An exact catalog window replaces a server-reported one.
	exact := enrichDiscoveredModel(proto.ProviderModel{ID: "claude-opus-4-8", ContextWindow: 123, Name: "From server"})
	if exact.ContextWindow != 1000000 || exact.Name != "From server" || exact.InheritedFrom != "" || exact.DefaultEffort != "medium" {
		t.Fatalf("exact window: %+v", exact)
	}
	// A server-reported window wins over one inherited from the previous generation.
	reported := enrichDiscoveredModel(proto.ProviderModel{ID: "claude-opus-5-6", ContextWindow: 123})
	if reported.ContextWindow != 123 || reported.InheritedFrom != "claude-opus-5-5" || reported.DefaultEffort != "medium" || reported.Name != "" {
		t.Fatalf("reported over inherited: %+v", reported)
	}
	// With no server window, the previous generation fills it.
	gap := enrichDiscoveredModel(proto.ProviderModel{ID: "claude-opus-5-6"})
	if gap.ContextWindow != 1000000 || gap.InheritedFrom != "claude-opus-5-5" {
		t.Fatalf("inherited gap: %+v", gap)
	}
	// A server-reported window beats [1m], which beats an inherited window.
	reportedMark := enrichDiscoveredModel(proto.ProviderModel{ID: "claude-haiku-4-6[1m]", ContextWindow: 123})
	if reportedMark.ContextWindow != 123 || reportedMark.InheritedFrom != "" {
		t.Fatalf("server over [1m]: %+v", reportedMark)
	}
	marked := enrichDiscoveredModel(proto.ProviderModel{ID: "claude-opus-5-6[1m]"})
	if marked.ContextWindow != 1000000 || marked.InheritedFrom != "claude-opus-5-5" {
		t.Fatalf("discovered [1m]: %+v", marked)
	}
	reportedOpus := enrichDiscoveredModel(proto.ProviderModel{ID: "claude-opus-5-6[1m]", ContextWindow: 123})
	if reportedOpus.ContextWindow != 123 || reportedOpus.InheritedFrom != "claude-opus-5-5" || reportedOpus.DefaultEffort != "medium" {
		t.Fatalf("server window keeps inherited efforts: %+v", reportedOpus)
	}
}

func TestTrailingZeroDoesNotReplaceGatewayWindow(t *testing.T) {
	got := enrichDiscoveredModel(proto.ProviderModel{ID: "glm-5.3.0", Name: "Gateway", ContextWindow: 128000})
	if got.Name != "Gateway" || got.ContextWindow != 128000 || got.InheritedFrom != "glm-5.3" || !slices.Contains(got.Efforts, "low") {
		t.Fatalf("%+v", got)
	}
}

func TestDiscoveredExactCatalogWindowWins(t *testing.T) {
	for id, want := range builtinModels.All() {
		if want.ContextWindow == 0 {
			continue
		}
		got := enrichDiscoveredModel(proto.ProviderModel{ID: id, Name: "server", ContextWindow: 123})
		if got.ContextWindow != want.ContextWindow || got.InheritedFrom != "" || got.Name != "server" {
			t.Errorf("%s: %+v", id, got)
		}
		marked := enrichDiscoveredModel(proto.ProviderModel{ID: id + "[1m]", ContextWindow: 123})
		if marked.ContextWindow != want.ContextWindow || marked.InheritedFrom != "" {
			t.Errorf("%s[1m]: %+v", id, marked)
		}
	}
}
