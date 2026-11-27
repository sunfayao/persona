package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

// testReportDelay is the tests' reportDelay: short, set on each service (never the shared constant).
const testReportDelay = 50 * time.Millisecond

// reportService is a task service whose Feishu bot is paired, with reports on (or off) and the execution steps on (the
// card as it was before #3218), recording the posts.
func reportService(t *testing.T, on bool) (*Service, *fakeHub, func() []string) {
	t.Helper()
	return reportServiceWith(t, FeishuFeatures{Main: true, Cards: true, Reports: on, ReportSteps: true})
}

// reportServiceWith is reportService with the bot's switches as given.
func reportServiceWith(t *testing.T, feats FeishuFeatures) (*Service, *fakeHub, func() []string) {
	t.Helper()
	s, f := newTaskService(t)
	s.reports.mu.Lock()
	s.reports.delay = testReportDelay
	s.reports.mu.Unlock()
	var mu sync.Mutex
	var posts []string
	f.answerApp(AppFeishu, FeishuStatusGet, func(json.RawMessage) (any, error) {
		return FeishuStatus{Ready: true, Enabled: true, Owner: "ou_me", PublicURL: "https://hub.example.com",
			Features: feats}, nil
	})
	f.answerApp(AppFeishu, FeishuPost, func(p json.RawMessage) (any, error) {
		var m struct {
			Card string `json:"card"`
		}
		_ = json.Unmarshal(p, &m)
		mu.Lock()
		posts = append(posts, m.Card)
		mu.Unlock()
		return map[string]any{"message_id": "om_r"}, nil
	})
	return s, f, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), posts...)
	}
}

// A task's turn that ends with a merged PR is one card in the chat: the task, its stage, what was said, links to the
// task's page, the PR and the agent; never a bare id. The todo carries the stage and the PR for the apps.
func TestTaskDoneIsReportedInFeishu(t *testing.T) {
	s, f, posts := reportService(t, true)
	todo := startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "修好了，https://github.com/o/r/pull/901 已合并", 0)
	waitFor(t, "the report", func() bool { return len(posts()) == 1 })
	card := posts()[0]
	for _, want := range []string{"修登录崩溃", "✅开发", "🔵**合入**", "⚪部署", "已合并",
		"https://hub.example.com/todos/" + todo.ID, "https://github.com/o/r/pull/901", "https://hub.example.com/s/box/s1"} {
		if !strings.Contains(card, want) {
			t.Errorf("card lacks %q:\n%s", want, card)
		}
	}
	if got := onlyTodo(s); got.Stage != "merge" || got.PRURL != "https://github.com/o/r/pull/901" {
		t.Fatalf("todo stage %q pr %q", got.Stage, got.PRURL)
	}
	time.Sleep(4 * testReportDelay)
	if n := len(posts()); n != 1 {
		t.Fatalf("%d posts", n)
	}
}

// A chart in the Agent's answer (persona-card, #3140) is drawn on the report card as its numbers, never cut down to
// the title alone: the owner reads this card in Lark (#4093).
func TestTaskReportDrawsAChartAsNumbers(t *testing.T) {
	s, f, posts := reportService(t, true)
	startedTask(t, s, f)
	agentTurn(s, f, proto.StatusWorking, "", 0)
	agentTurn(s, f, proto.StatusDone, "睡眠这一周\n\n```persona-card\n"+
		`{"type":"line","title":"Sleep this week","unit":"h","points":[{"label":"Mon","value":7.2},{"label":"Tue","value":6.5}]}`+
		"\n```\n\n睡得比上周多点。", 0)
	waitFor(t, "the report", func() bool { return len(posts()) == 1 })
	card := posts()[0]
	for _, want := range []string{"Sleep this week", "Mon", "7.2", "Tue", "6.5", "睡得比上周多点"} {
		if !strings.Contains(card, want) {
			t.Errorf("the chart is not drawn as numbers: the card lacks %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "persona-card") || strings.Contains(card, `\"type\"`) {
		t.Errorf("the chart block is left on the card as it was written:\n%s", card)
	}
}

func TestStageOf(t *testing.T) {
	for text, want := range map[string]string{
		"改好了，还在跑测试":                                  "develop",
		"提了 https://github.com/o/r/pull/1":           "review",
		"https://github.com/o/r/pull/1 已合入":          "merge",
		"合入 https://github.com/o/r/pull/1 并已部署到 n37": "deploy",
		"已部署，实测通过，截图为证":                              "verify",
		// Not deployed yet, waiting for it, or asking: no "Please verify".
		"https://github.com/o/r/pull/1 已合并，还没部署到 hub，等你部署":       "merge",
		"Merged https://github.com/o/r/pull/2, not deployed yet": "merge",
		"已合入。未部署（这台连不上 hub）":                                     "develop",
		"部署了吗？还没有":                                               "develop",
		"等部署完再验":                                                 "develop",
		"Ready to be deployed once CI passes":                    "develop",
		"deploy.sh 跑完，已上线":                                       "deploy",
		"已部署到 hub，还没验证":                                          "deploy",
		"已部署到 hub。未部署的部分：网页":                                     "deploy",
	} {
		if got, _ := stageOf(text); got != want {
			t.Errorf("stageOf(%q) = %q, want %q", text, got, want)
		}
	}
	if got := stageBar("zh-Hans", "", false); strings.Contains(got, "✅") || strings.Contains(got, "🔵") {
		t.Errorf("no stage: %s", got)
	}
	if got := stageBar("zh-Hans", "", true); strings.Contains(got, "✅") || strings.Contains(got, "完成") {
		t.Errorf("no stage, done: %s", got) // nothing reached: the plain strip, never a Done of nothing
	}
	// Done after the steps taken (#11590): no current step, nothing claimed past Develop.
	if got, want := stageBar("en", "develop", true), "✅Plan → ✅Build → ✅**Done**"; got != want {
		t.Errorf("done at develop = %s, want %s", got, want)
	}
	if got := stageBar("en", "develop", false); !strings.Contains(got, "🔵**Build**") || !strings.Contains(got, "⚪Verified") {
		t.Errorf("open at develop = %s", got)
	}
	_ = context.Background
}

// Realistic replies: told as deployed or not (negated, deferred, asked). A false "deployed" sends a bogus "Please verify" card.
func TestSaidDeployed(t *testing.T) {
	for text, want := range map[string]bool{
		"已完成部署，请验收": true, "部署好了": true, "已经部署，不需要你再看": true, "已部署到 hub，不需要你再操作": true,
		"部署完成，链接 …": true, "上线了，可以去看看": true, "Deployed to staging; nothing pending.": true,
		"shipped and deployed, nothing pending": true, "It is live now": true,
		"not yet deployed": false, "hasn't been deployed yet": false, "it isn't fully deployed yet": false,
		"nothing has been deployed yet": false, "it has not been deployed": false, "先别部署到生产，等我确认": false,
		"无需部署到生产": false, "等你确认后再部署到生产": false, "待验证后再部署到生产": false, "还没部署到 hub，等我打声招呼": false,
		"尚未部署到 hub，先别验收": false, "准备部署到生产，等 CI": false, "等部署完再说": false, "部署了吗？": false,
		"Not deployed yet.": false, "we haven't deployed it yet": false, "never deployed": false,
		"awaiting deploy, will ping you": false, "It will be deployed after review": false,
		"还没合入。已部署到 hub": true, "PR 已合并，未部署（连不上 hub）": false,
	} {
		if got := said(deployWords, text); got != want {
			t.Errorf("said(%q) = %v, want %v", text, got, want)
		}
	}
}

// A failed read of the stages keeps the book unloaded: a stage or delivery set meanwhile is not written over every
// other task's, and the next look reads the stored stages again.
func TestStagesReadFailureNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	st := &downStore{memStore: &memStore{}}
	keep := map[string]todoStage{"a": {Stage: "review", PR: "https://git.example.com/team/repo/pull/1",
		Delivery: &TodoDelivery{State: deliveryPR}}}
	if err := st.PutSetting(ctx, reportStagesKey, keep); err != nil {
		t.Fatal(err)
	}
	s := newServiceOn(t, newFakeHub().withMachine(), st)

	st.failing(true)
	if got := s.todoStages(ctx); len(got) != 0 {
		t.Fatalf("stages while unreadable: %v", got)
	}
	st.failing(false)
	s.setTaskStage(ctx, "b", "review", "")
	s.setTaskDelivery(ctx, "c", TodoDelivery{State: deliveryMerged})

	var stored map[string]todoStage
	if _, err := st.Setting(ctx, reportStagesKey, &stored); err != nil {
		t.Fatal(err)
	}
	a := stored["a"]
	if a.Stage != "review" || a.PR != keep["a"].PR || a.Delivery == nil || a.Delivery.State != deliveryPR {
		t.Fatalf("task a after a failed read: %+v (stored %v)", a, stored)
	}
	if stored["b"].Stage != "review" || stored["c"].Delivery == nil || stored["c"].Stage != "merge" {
		t.Fatalf("the later changes were not kept: %v", stored)
	}

	// While the read keeps failing, nothing is written and nothing is cached.
	st.failing(true)
	s.reports.mu.Lock()
	s.reports.loaded, s.reports.stages = false, nil
	s.reports.mu.Unlock()
	s.setTaskStage(ctx, "d", "review", "")
	s.setTaskDelivery(ctx, "e", TodoDelivery{State: deliveryPR})
	st.failing(false)
	stored = nil
	if _, err := st.Setting(ctx, reportStagesKey, &stored); err != nil {
		t.Fatal(err)
	}
	if _, ok := stored["d"]; ok || len(stored) != 3 {
		t.Fatalf("written while unreadable: %v", stored)
	}
	if got := s.todoStages(ctx); len(got) != 3 {
		t.Fatalf("stages after the store is back: %v", got)
	}
}
