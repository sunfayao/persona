package assistant

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sunfayao/persona/server/internal/proto"
)

const researchNoPRCloseout = "<closeout status=\"done\" merged=\"n/a\" deployed=\"n/a\">\n" +
	"artifact: doc docs/research.md\nartifact: url https://github.com/o/r/issues/4\n" +
	"verified: Research findings checked against the original criteria.\n</closeout>"

const researchMarkdown = "### Persona work report\n\n- Status: done\n- Merged: n/a\n- Deployed: n/a\n" +
	"- Artifact: doc docs/research.md\n- Verified: Research findings checked against the original criteria.\n\n---"

func TestNoPRCloseoutCannotDiscardDeliveryWithoutOwnership(t *testing.T) {
	for _, tc := range []struct {
		name string
		ask  proto.Item
		text string
	}{
		{"missing applicability", proto.Item{}, "<closeout status=\"done\">\nartifact: doc docs/research.md\n</closeout>"},
		{"duplicate invalid applicability", proto.Item{}, strings.Replace(researchNoPRCloseout, `merged="n/a"`, `merged="n/a" merged="bogus"`, 1)},
		{"duplicate applicability", proto.Item{}, strings.Replace(researchNoPRCloseout, `deployed="n/a"`, `deployed="n/a" deployed="n/a"`, 1)},
		{"work not done", proto.Item{}, strings.Replace(researchNoPRCloseout, `status="done"`, `status="not_done"`, 1)},
		{"empty record", proto.Item{}, "<closeout status=\"done\" merged=\"n/a\" deployed=\"n/a\">\n</closeout>"},
		{"empty PR artifact", proto.Item{}, strings.Replace(researchNoPRCloseout, "</closeout>", "artifact: pr\n</closeout>", 1)},
		{"PR beyond artifact preview", proto.Item{}, "<closeout status=\"done\" merged=\"n/a\" deployed=\"n/a\">\n" +
			strings.Repeat("artifact: doc docs/research.md\n", 5) + "artifact: pr https://github.com/o/r/pull/5\n</closeout>"},
		{"peer evidence", proto.Item{Role: "user", Kind: proto.KindText, From: "another-agent", Text: "Unrelated research"}, researchNoPRCloseout},
		{"different request", proto.Item{Role: "user", Kind: proto.KindText, Text: "Research an unrelated repository"}, researchNoPRCloseout},
		{"injected evidence", proto.Item{Role: "user", Kind: proto.KindText, Text: "<persona-side-conversation/> Research this instead"}, researchNoPRCloseout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, todo := deliveryReviewService(t, &fakeForge{}, nil, false)
			s.hub.(*fakeHub).setItems(tc.ask, proto.Item{Role: "assistant", Kind: proto.KindText, Text: tc.text})
			if _, err := acceptReview(s, todo.ID, nil); err == nil {
				t.Fatalf("unowned or incomplete evidence bypassed a real delivery: %v", err)
			}
			if got := todoNow(s, todo.ID); got.DoneAt != nil || got.PRURL != "https://github.com/o/r/pull/9" {
				t.Fatalf("unowned evidence changed the delivery: %+v", got)
			}
		})
	}
}

func TestSeveralDeliveryArtifactsCannotHideAnUnmergedPR(t *testing.T) {
	s, f, todo := newSupervisedService(t)
	s.deploy.forge = &fakeForge{}
	f.setItems(proto.Item{Role: "assistant", Kind: proto.KindText, Text: "<closeout status=\"done\" merged=\"yes\" deployed=\"yes\">\n" +
		"artifact: pr https://github.com/o/r/pull/5\nartifact: pr https://github.com/o/r/pull/6\n</closeout>"})
	if _, _, err := s.reportedTaskPR(context.Background(), todo, "https://github.com/o/r/pull/4"); err == nil {
		t.Fatal("several explicit deliveries were silently reduced to one PR")
	}
}

func TestReportedPRFromActiveAgentsRequiresUnambiguousOwnership(t *testing.T) {
	pr := func(n string) string {
		return "<closeout status=\"done\" merged=\"no\" deployed=\"no\">\n" +
			"artifact: pr https://github.com/o/r/pull/" + n + "\n</closeout>"
	}
	for _, tc := range []struct {
		name    string
		replies []string
		want    string
		noPR    bool
		err     bool
	}{
		{"different deliveries", []string{pr("5"), pr("6")}, "", false, true},
		{"same delivery", []string{pr("5"), pr("5")}, "https://github.com/o/r/pull/5", false, false},
		{"one agent still has a PR", []string{researchNoPRCloseout, pr("5")}, "https://github.com/o/r/pull/5", false, false},
		{"all agents have no PR", []string{researchNoPRCloseout, researchNoPRCloseout}, "", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, todo := newSupervisedService(t)
			s.deploy.forge = &fakeForge{}
			other := todo.Agents[0]
			other.SessionID = "s2"
			todo.Agents = append(todo.Agents, other)
			f.mu.Lock()
			f.itemReads = 0
			f.readHooks.before = func(read int) {
				f.setItems(proto.Item{Role: "assistant", Kind: proto.KindText, Text: tc.replies[min(read-1, 1)]})
			}
			f.mu.Unlock()
			defer func() {
				f.mu.Lock()
				f.readHooks = receiptHooks{}
				f.mu.Unlock()
			}()
			got, noPR, err := s.reportedTaskPR(context.Background(), todo, "")
			if got != tc.want || noPR != tc.noPR || (err != nil) != tc.err {
				t.Fatalf("multi-agent delivery = %q, noPR=%v, %v; want %q, noPR=%v, error=%v",
					got, noPR, err, tc.want, tc.noPR, tc.err)
			}
		})
	}
}

func TestUnknownPRHistoryRequiresExplicitDeliveryEvidence(t *testing.T) {
	s, todo := deliveryReviewService(t, &fakeForge{}, nil, false)
	ctx := context.Background()
	s.setTaskStage(ctx, todo.ID, "review", "https://github.com/o/r/pull/4")
	s.hub.(*fakeHub).setItems(proto.Item{Role: "assistant", Kind: proto.KindText, Text: "A related PR was mentioned earlier."})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/o/r/issues/4" {
			_, _ = w.Write([]byte(`{"number":4}`))
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	s.deploy.forge = githubForge{base: server.URL, client: server.Client()}
	for i := 0; i < 2; i++ {
		if _, err := acceptReview(s, todo.ID, nil); err == nil || !strings.Contains(err.Error(), "delivery ownership") {
			t.Fatalf("unattributed history was silently promoted or discarded: %v", err)
		}
		if got := todoNow(s, todo.ID); got.DoneAt != nil || got.PRURL != "https://github.com/o/r/pull/4" {
			t.Fatalf("uncertain delivery state was lost: %+v", got)
		}
	}
}

func TestExplicitUnmergedDeliveryStillBlocksWithRelatedReferences(t *testing.T) {
	for _, recorded := range []string{"", "4", "9"} {
		t.Run("recorded_"+recorded, func(t *testing.T) {
			s, f, todo := newSupervisedService(t)
			ctx := context.Background()
			deliveryOn(t, s)
			if recorded != "" {
				s.setTaskStage(ctx, todo.ID, "review", "https://github.com/o/r/pull/"+recorded)
			}
			f.setItems(proto.Item{Role: "assistant", Kind: proto.KindText, Text: "Related merged PR: https://github.com/o/r/pull/9\n" +
				"<closeout status=\"not_done\" merged=\"no\" deployed=\"no\">\n" +
				"artifact: pr https://github.com/o/r/pull/5\n</closeout>"})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/o/r/issues/4":
					_, _ = w.Write([]byte(`{"number":4}`))
				case "/repos/o/r/issues/5":
					_, _ = w.Write([]byte(`{"pull_request":{}}`))
				case "/repos/o/r/pulls/5":
					_, _ = w.Write([]byte(`{"merged":false}`))
				case "/repos/o/r/pulls/9":
					_, _ = w.Write([]byte(`{"merged":true}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			s.deploy.forge = githubForge{base: server.URL, client: server.Client()}
			s.deploy.hubBuild = func(context.Context) (string, string, error) { return "dev", "", nil }
			s.deploy.listPaths = []string{}
			_, err := acceptReview(s, todo.ID, nil)
			if err == nil || !strings.Contains(err.Error(), "not merged") {
				t.Fatalf("the actual unmerged delivery was not blocked: %v", err)
			}
			got := todoNow(s, todo.ID)
			if got.DoneAt != nil || got.PRURL != "https://github.com/o/r/pull/5" {
				t.Fatalf("the explicit delivery did not replace the incidental reference: %+v", got)
			}
		})
	}
}

func TestNoPRCloseoutOutranksIncidentalPRNumbers(t *testing.T) {
	text := fmt.Sprintf("Related PR #9: https://github.com/o/r/pull/9\n%s", researchNoPRCloseout)
	if got := prNumberIn(text); got != "" {
		t.Fatalf("a no-PR closeout fell back to incidental PR number %s", got)
	}
	if _, got := stageOf(text); got != "" {
		t.Fatalf("a no-PR closeout fell back to incidental PR URL %s", got)
	}
}

func TestLegacyProgressWithoutAnAuthoritativeCloseout(t *testing.T) {
	for _, text := range []string{
		"Example:\n```text\n<closeout status=\"done\">\nartifact: doc docs/research.md\n</closeout>\n```\n",
		"> <closeout status=\"done\">\n> artifact: doc docs/research.md\n> </closeout>\n",
		"Implementing the <closeout> parser.\n",
	} {
		t.Run(text, func(t *testing.T) {
			if got := prNumberIn(text + "Pushed PR #5"); got != "5" {
				t.Fatalf("a non-authoritative example suppressed legacy PR progress: %q", got)
			}
			if _, got := stageOf(text + "Opened https://github.com/o/r/pull/5"); got != "https://github.com/o/r/pull/5" {
				t.Fatalf("a non-authoritative example suppressed legacy PR URL progress: %q", got)
			}
		})
	}
}

func TestCurrentNumberedArtifactKeepsOfflineDeploymentProof(t *testing.T) {
	for _, ref := range []string{"#9", "9", "o/r#9"} {
		t.Run(ref, func(t *testing.T) {
			forge := &fakeForge{err: fmt.Errorf("the forge is offline")}
			s, todo := deliveryReviewService(t, forge, &runningHub{commit: oldHub}, false)
			writeDeployed(t, s, oldHub, mergeSHA+" fix: the task's delivery (#9)")
			s.hub.(*fakeHub).setItems(proto.Item{Role: "assistant", Kind: proto.KindText,
				Text: "<closeout status=\"done\" merged=\"yes\" deployed=\"yes\">\nartifact: pr " + ref + "\n</closeout>"})
			if _, err := acceptReview(s, todo.ID, nil); err != nil {
				t.Fatalf("an already-recorded delivery lost its local deployment proof: %v", err)
			}
			// The running build's receipt proves deployment without a forge request.
			if got := todoNow(s, todo.ID); got.DoneAt == nil || forge.calls != 0 {
				t.Fatalf("local delivery was not accepted while the forge failed: done=%v, calls=%d", got.DoneAt, forge.calls)
			}
		})
	}
}
