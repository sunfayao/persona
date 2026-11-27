package hub

// A manager's messages to the apps (ws.broadcast, bus_broadcast.go): only its types, about its own things, read with
// exact keys.

import (
	"bytes"
	"encoding/json"
	"os"
	"slices"
	"testing"

	"github.com/sunfayao/persona/server/internal/extwire"
	"github.com/sunfayao/persona/server/internal/proto"
)

// marked: one of got carries mark.
func marked(got []map[string]json.RawMessage, mark string) bool {
	for _, m := range got {
		b, _ := json.Marshal(m)
		if bytes.Contains(b, []byte(mark)) {
			return true
		}
	}
	return false
}

func TestMessageSendStateCapabilityRequiresHubOwnership(t *testing.T) {
	count := 0
	for _, feature := range proto.Features {
		if feature == proto.FeatureMessageSendState {
			count++
		}
	}
	if count != 1 || !hubMessageTypes[proto.FeatureMessageSendState] || managerMessageTypes[proto.FeatureMessageSendState] {
		t.Fatalf("receipt capability must be declared once and reserved to the hub: count=%d", count)
	}
}

// The feature array includes extension claims, including on legacy hubs. The
// receipt version must be a separate, hub-authored hello field. The same probe
// runs against a40a73e1b's hello.go with PERSONA_TEST_LEGACY_RECEIPT_HELLO=1:
// forged feature claims remain visible there, but establish no receipt authority.
func TestReceiptHelloVersionIsHubAuthored(t *testing.T) {
	legacy := os.Getenv("PERSONA_TEST_LEGACY_RECEIPT_HELLO") == "1"
	if legacy {
		before := proto.Features
		proto.Features = slices.DeleteFunc(slices.Clone(before), func(feature string) bool { return feature == proto.FeatureMessageSendState })
		t.Cleanup(func() { proto.Features = before })
	}
	for _, declared := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy feature report", true: "declared extension feature"}[declared], func(t *testing.T) {
			h, _, _ := pathsHub(t, quiet, quiet)
			h.keepExtReport("m-0b0b0b", &extwire.Report{EventsDeclared: declared,
				Features: []string{proto.FeatureMessageSendState},
				APIs:     []extwire.APIPrefix{{Prefix: "fixture", Events: []string{proto.FeatureMessageSendState}}}})
			raw, err := json.Marshal(h.hello())
			if err != nil {
				t.Fatal(err)
			}
			var hello map[string]json.RawMessage
			if err := json.Unmarshal(raw, &hello); err != nil {
				t.Fatal(err)
			}
			var features []string
			if err := json.Unmarshal(hello["features"], &features); err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(features, proto.FeatureMessageSendState) {
				t.Fatal("fixture did not inject the informational feature")
			}
			version, present := hello[proto.FeatureMessageSendState]
			trusted := bytes.Equal(version, []byte("1"))
			if legacy {
				if present || trusted {
					t.Fatalf("legacy feature claim acquired receipt authority: %s", raw)
				}
			} else if !present || !trusted {
				t.Fatalf("hub-authored receipt version must be the integer 1: %s", raw)
			}
			t.Logf("legacy=%t feature_present=true receipt_version_present=%t receipt_trusted=%t", legacy, present, trusted)
		})
	}
}

// exactJSON: one JSON object, no key twice in an object, no case variant of a checked key at any depth or of the stamp
// at the top level.
func TestExactJSON(t *testing.T) {
	for raw, ok := range map[string]bool{
		`{"type":"inbox","a":{"b":[1,{"c":2}]},"d":[]}`: true,
		`{"type":"inbox","machine_ids":"x","Note":"x"}`: true,
		`{"type":"inbox"} {"type":"inbox"}`:             false,
		`{"type":"inbox","a":1,"a":2}`:                  false,
		`{"type":"inbox","a":[{"b":1,"b":1}]}`:          false,
		`{"type":"inbox","TYPE":"x"}`:                   false,
		`{"type":"inbox","a":{"Session_Id":"x"}}`:       false,
		`{"type":"inbox","a":[[{"CONVERSATION":1}]]}`:   false,
		`{"type":"inbox","Manager":"x"}`:                false,
		`{"type":"inbox","a":{"Manager":"x"}}`:          true, // the sender's own field, below the stamp
		`{"type":`:                                      false,
		``:                                              false,
	} {
		if err := exactJSON([]byte(raw)); (err == nil) != ok {
			t.Errorf("%s: %v", raw, err)
		}
	}
}
