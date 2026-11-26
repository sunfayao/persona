package assistant

import (
	"encoding/json"
	"errors"
	"github.com/sunfayao/persona/server/internal/assistantwire"
	"github.com/sunfayao/persona/server/internal/proto"
	"github.com/sunfayao/persona/server/internal/rpc"
	"strings"
	"testing"
)

func TestMainDefaultWaitsForExplicitSwitchBeforeCrossingExecutionPaths(t *testing.T) {
	for _, tc := range []struct {
		name, launcher, model string
	}{
		{"another engine", "cx", "gpt-5"},
		{"same engine in another runtime", "hosted-claude", "opus"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := pickFixture(t)
			f.setSessions("server", proto.Session{SessionID: "main", Agent: "claude", Assistant: true, Launcher: "gw"})
			f.mu.Lock()
			f.launcherList = append(f.launcherList, proto.Launcher{Name: "hosted-claude", Source: "runtime-other", Agent: "claude", Assistant: true})
			f.mu.Unlock()
			f.answerNode("assistant.segment.check", func(string, json.RawMessage) (any, error) {
				return segmentTarget{Registered: true, Prepare: true, Engine: "claude|runtime:runtime-other"}, nil
			})
			setModel(t, s, purposeMain, ModelBinding{Provider: "launcher." + tc.launcher, Model: tc.model})
			p := map[string]any{}
			err := s.sendModel(t.Context(), "server", "main", p, s.lang(t.Context()))
			var refusal *rpc.Error
			if !errors.As(err, &refusal) || refusal.Code == assistantwire.CodeLauncherOtherKind || !strings.Contains(string(refusal.Data), `"delivered":"none"`) {
				t.Fatalf("default implicitly changed execution: %v %v", p, err)
			}
			if _, kept := s.modelPickOf(t.Context(), "server", "main"); kept || s.settings(t.Context()).Main != "main" {
				t.Fatal("a default save changed conversation ownership or its explicit choice")
			}
		})
	}
}

func TestRuntimeMainAppliesOwnBindingWithoutChangingOtherRuntimeChats(t *testing.T) {
	for _, tc := range []struct {
		name, session, target string
		own, refused          bool
	}{
		{"legacy main", "main", "hosted-one", false, false},
		{"own main model", "main", "hosted-one", true, false},
		{"side stays on its runtime", "side", "hosted-one", true, false},
		{"main cannot resume natively", "main", "claude", true, true},
		{"main cannot resume another runtime", "main", "hosted-two", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f := pickFixture(t)
			f.mu.Lock()
			f.launcherList = append(f.launcherList,
				proto.Launcher{Name: "hosted-one", Source: "runtime-one", Agent: "claude", Assistant: true},
				proto.Launcher{Name: "hosted-two", Source: "runtime-two", Agent: "claude", Assistant: true})
			f.mu.Unlock()
			f.setSessions("server", proto.Session{SessionID: tc.session, Assistant: true, Agent: "claude", Launcher: "hosted-one", SourceKind: proto.SourceRuntime, SourceInstanceID: "runtime-one"})
			f.answerNode("assistant.segment.check", func(string, json.RawMessage) (any, error) {
				identity := "claude"
				if tc.target == "hosted-two" {
					identity += "|runtime:runtime-two"
				}
				return segmentTarget{Registered: true, Prepare: true, Engine: identity}, nil
			})
			if tc.own {
				setModel(t, s, purposeMain, ModelBinding{Provider: "launcher." + tc.target, Model: "main-model", Effort: "high"})
			}
			p := map[string]any{}
			err := s.sendModel(t.Context(), "server", tc.session, p, s.lang(t.Context()))
			if tc.refused {
				var refusal *rpc.Error
				if !errors.As(err, &refusal) || !strings.Contains(string(refusal.Data), `"delivered":"none"`) {
					t.Fatalf("runtime default changed execution: %v %v", p, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.own && tc.session == "main" {
				if p["launcher"] != "hosted-one" || p["model"] != "main-model" || p["effort"] != "high" {
					t.Fatalf("runtime main ignored own binding: %v", p)
				}
			} else if p["launcher"] != nil || p["model"] != nil || p["effort"] != nil {
				t.Fatalf("runtime inheritance changed: %v", p)
			}
		})
	}
}
