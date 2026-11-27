package assistant

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/proto"
)

func configureQuotaFixture(s *Service, f *fakeHub, d *resourceDevs) *atomic.Bool {
	busyHealthy(d)
	f.launcherList = []proto.Launcher{{Name: "work", Agent: "claude"}}
	group := testPool
	group.QuotaPool = &GatewayQuotaPool{Source: "fixture-pool", Provider: "claude"}
	withLauncherGroups(s, group)
	full := new(atomic.Bool)
	full.Store(true)
	d.gatewayRetry = func(source, provider string, now time.Time) time.Time {
		if source == "fixture-pool" && provider == "claude" && full.Load() {
			return now.Add(4 * time.Minute)
		}
		return time.Time{}
	}
	return full
}

func quotaPlan(t *testing.T, s *Service, id string) *TaskPlan {
	t.Helper()
	plan, err := s.loadPlan(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func dueQuota(t *testing.T, s *Service, id string, expire bool) *TaskPlan {
	t.Helper()
	if err := s.patchPlanTask(context.Background(), id, 0, func(pt *PlanTask) {
		queue := *pt.Queue
		queue.NextAt = time.Now().Add(-time.Second)
		if expire {
			wait := *queue.Gateway
			wait.Deadline = queue.NextAt
			queue.Gateway = &wait
		}
		pt.Queue = &queue
	}); err != nil {
		t.Fatal(err)
	}
	return quotaPlan(t, s, id)
}

func TestGatewayQuotaWaitParksBeforeAdmissionAndResumes(t *testing.T) {
	s, f, d := admissionHub(t)
	full := configureQuotaFixture(s, f, d)
	p, todo := queuedStart(t, s, "busy")
	q := p.Tasks[0].Queue
	if q == nil || q.Gateway == nil || q.Key != gatewayQuotaWaitKey ||
		q.Gateway.Pool.Source != "fixture-pool" || q.Gateway.Launcher != "work" ||
		q.Gateway.Deadline.Sub(q.QueuedAt) != gatewayQuotaMaxWait ||
		q.NextAt.Sub(q.QueuedAt) != admissionQueueInterval {
		t.Fatalf("bad persisted quota wait: %+v", q)
	}
	if todo.DoneAt != nil || todo.StartError != "" || len(taskStartMachines(f)) != 0 ||
		s.admission.machineClaims["busy"] != 0 || s.admission.groupClaims["pool"] != 0 {
		t.Fatalf("deferral reached admission, reported failure, or reached a node: %+v", todo)
	}
	card := queuedCard(t, s, p.ID)
	if card.State != "waiting" || !strings.Contains(card.Reason, q.Reason) {
		t.Fatalf("the card must explain the wait: %+v", card)
	}

	s.sweepAdmissionQueue(context.Background())
	if len(taskStartMachines(f)) != 0 {
		t.Fatal("a quota wait must not be released before its next check")
	}
	dueQuota(t, s, p.ID, false)
	s.sweepAdmissionQueue(context.Background())
	wait := quotaPlan(t, s, p.ID).Tasks[0].Queue
	if wait == nil || wait.Claimed || wait.Attempt != 2 || !wait.Gateway.Deadline.Equal(q.Gateway.Deadline) ||
		len(taskStartMachines(f)) != 0 {
		t.Fatalf("recheck must keep the same wait budget without a start: %+v", wait)
	}

	full.Store(false)
	dueQuota(t, s, p.ID, false)
	s.sweepAdmissionQueue(context.Background())
	waitFor(t, "one successful quota release", func() bool {
		return len(taskStartMachines(f)) == 1 && quotaPlan(t, s, p.ID).Tasks[0].Queue == nil
	})
	s.sweepAdmissionQueue(context.Background())
	if starts := taskStartMachines(f); len(starts) != 1 || starts[0] != "busy" {
		t.Fatalf("the scheduled retry must start once on the original computer: %v", starts)
	}
	if got := quotaPlan(t, s, p.ID).Tasks[0]; got.Launcher != "work" || got.Machine != "busy" {
		t.Fatalf("quota deferral changed placement or launcher: %+v", got)
	}
}

func TestGatewayQuotaWaitExpiresIntoOrdinaryAdmission(t *testing.T) {
	s, f, d := admissionHub(t)
	configureQuotaFixture(s, f, d)
	p, _ := queuedStart(t, s, "busy")
	if p.Tasks[0].Queue == nil {
		t.Fatal("expected an initial quota wait")
	}
	dueQuota(t, s, p.ID, true)
	busyOverloaded(d)
	s.sweepAdmissionQueue(context.Background())
	waitFor(t, "expiry to leave quota and enter the existing resource gate", func() bool {
		q := quotaPlan(t, s, p.ID).Tasks[0].Queue
		return q != nil && q.Key == "assistant-tasks.admission_load"
	})
	q := quotaPlan(t, s, p.ID).Tasks[0].Queue
	if q.Attempt != 1 || q.Gateway == nil || q.Claimed || len(taskStartMachines(f)) != 0 {
		t.Fatalf("ordinary admission must have a new retry budget: %+v", q)
	}
	busyHealthy(d)
	dueQuota(t, s, p.ID, false)
	s.sweepAdmissionQueue(context.Background())
	waitFor(t, "the ordinary gate to start after the bounded quota wait", func() bool {
		return len(taskStartMachines(f)) == 1 && quotaPlan(t, s, p.ID).Tasks[0].Queue == nil
	})
}

func TestGatewayQuotaWaitRespectsCancelledTask(t *testing.T) {
	s, f, d := admissionHub(t)
	full := configureQuotaFixture(s, f, d)
	p, todo := queuedStart(t, s, "busy")
	if p.Tasks[0].Queue == nil {
		t.Fatal("expected an initial quota wait")
	}
	if err := s.failTodo(context.Background(), todo.ID, "ended by fixture"); err != nil {
		t.Fatal(err)
	}
	full.Store(false)
	s.sweepAdmissionQueue(context.Background()) // clear an ended task even before the next check was due
	if quotaPlan(t, s, p.ID).Tasks[0].Queue != nil || len(taskStartMachines(f)) != 0 {
		t.Fatal("an ended quota wait must be removed without contacting a node")
	}
}

func TestGatewayQuotaWaitLeavesAnAlreadyLinkedTaskAlone(t *testing.T) {
	ctx := context.Background()
	s, f, d := admissionHub(t)
	full := configureQuotaFixture(s, f, d)
	p, todo := queuedStart(t, s, "busy")
	if p.Tasks[0].Queue == nil {
		t.Fatal("expected an initial quota wait")
	}
	if err := s.linkTodoAgent(ctx, todo.ID, "busy", "fixture-existing-agent", "Already started", "/fixture/work", "claude"); err != nil {
		t.Fatal(err)
	}
	full.Store(false)
	s.sweepAdmissionQueue(ctx)
	if quotaPlan(t, s, p.ID).Tasks[0].Queue != nil || len(taskStartMachines(f)) != 0 {
		t.Fatal("the task already had an Agent and must not start another one")
	}
}

func TestGatewayQuotaWaitRequiresOptInAndActualLauncherFit(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Service, *fakeHub, *resourceDevs)
	}{
		{"unconfigured quota", func(s *Service, _ *fakeHub, _ *resourceDevs) {
			withLauncherGroups(s, testPool)
		}},
		{"unknown launcher kind", func(_ *Service, f *fakeHub, _ *resourceDevs) {
			f.launcherList = []proto.Launcher{{Name: "work"}}
		}},
		{"different launcher provider", func(_ *Service, f *fakeHub, _ *resourceDevs) {
			f.launcherList = []proto.Launcher{{Name: "work", Agent: "codex"}}
		}},
		{"recovery past the bounded wait", func(_ *Service, _ *fakeHub, d *resourceDevs) {
			d.gatewayRetry = func(_, _ string, now time.Time) time.Time { return now.Add(2 * gatewayQuotaMaxWait) }
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, f, d := admissionHub(t)
			configureQuotaFixture(s, f, d)
			tc.change(s, f, d)
			p, _ := queuedStart(t, s, "busy")
			if q := p.Tasks[0].Queue; q != nil || len(taskStartMachines(f)) != 1 {
				t.Fatalf("must start through the normal path, queue %+v, starts %v", q, taskStartMachines(f))
			}
		})
	}
}

func TestGatewayQuotaWaitReleasesAfterBindingRemoval(t *testing.T) {
	s, f, d := admissionHub(t)
	configureQuotaFixture(s, f, d)
	p, _ := queuedStart(t, s, "busy")
	if p.Tasks[0].Queue == nil {
		t.Fatal("expected an initial quota wait")
	}
	// The old pool still reports it is full. A removed binding no longer consents to waiting.
	withLauncherGroups(s, testPool)
	dueQuota(t, s, p.ID, false)
	s.sweepAdmissionQueue(context.Background())
	waitFor(t, "a task to start after removing its binding", func() bool {
		return len(taskStartMachines(f)) == 1 && quotaPlan(t, s, p.ID).Tasks[0].Queue == nil
	})
}

func TestGatewayQuotaWaitDoesNotChangeDirectAdmission(t *testing.T) {
	s, f, d := admissionHub(t)
	configureQuotaFixture(s, f, d)
	release, err := s.admitAgentStart(context.Background(), "busy", "work")
	if err != nil {
		t.Fatalf("direct admission cannot take the quota wait: %v", err)
	}
	release()
}

func TestGatewayQuotaPoolSettingValidation(t *testing.T) {
	valid := GatewayQuotaPool{Source: "test.gateway_1", Provider: "claude"}
	for _, item := range []struct {
		binding GatewayQuotaPool
		ok      bool
	}{
		{valid, true}, {GatewayQuotaPool{Source: valid.Source}, false},
		{GatewayQuotaPool{Source: "https://gateway.example.test", Provider: "claude"}, false},
		{GatewayQuotaPool{Source: valid.Source, Provider: "unknown"}, false},
	} {
		group := testPool
		group.QuotaPool = &item.binding
		cfg := AdmissionSettings{LauncherGroups: []LauncherGroup{group}}
		if err := cfg.validate(); (err == nil) != item.ok {
			t.Fatalf("binding %+v validity = %v", item.binding, err)
		}
		if item.ok {
			raw, err := json.Marshal(cfg)
			if err != nil || !strings.Contains(string(raw), "\"quota_pool\"") {
				t.Fatalf("the explicit binding needs to be saved: %s, %v", raw, err)
			}
		}
	}
}
