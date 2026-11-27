package assistant

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sunfayao/persona/server/internal/db"
)

func TestCalibratedEstimateUsesTheMostSpecificSupportedMedian(t *testing.T) {
	key := db.TaskEstimateCohort{Kind: "code", Machine: "box", Repository: "/fixture/repository"}
	sample := func(machine, repository string, minutes int) db.TaskEstimateSample {
		return db.TaskEstimateSample{Machine: machine, Repository: repository, ActualMinutes: minutes}
	}
	sameRepo := []db.TaskEstimateSample{
		sample("box", key.Repository, 8), sample("box", key.Repository, 14),
		sample("box", key.Repository, 700), sample("box", key.Repository, 10),
		sample("box", key.Repository, 12),
	}
	for _, test := range []struct {
		name    string
		samples []db.TaskEstimateSample
		want    TaskEstimateEvidence
		learned bool
	}{
		{"repository before broad outliers", append(slices.Clone(sameRepo), sample("other", key.Repository, 1000)),
			TaskEstimateEvidence{Minutes: 12, SampleCount: 5, Cohort: estimateRepoMachine}, true},
		{"machine without sufficient repository samples", append(slices.Clone(sameRepo[:4]), sample("box", "/other", 16)),
			TaskEstimateEvidence{Minutes: 14, SampleCount: 5, Cohort: estimateMachine}, true},
		{"same local paths on other machines stay broader", []db.TaskEstimateSample{
			sample("box", key.Repository, 2), sample("other", key.Repository, 6), sample("other", key.Repository, 8),
			sample("other", key.Repository, 9), sample("other", key.Repository, 19), sample("other", key.Repository, 21),
		}, TaskEstimateEvidence{Minutes: 9, SampleCount: 6, Cohort: estimateComparable}, true},
		{"few samples keep the default", sameRepo[:4],
			TaskEstimateEvidence{Minutes: 60, SampleCount: 4, Cohort: estimateDefault}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, learned := chooseTaskEstimate(key, test.samples)
			if got != test.want || learned != test.learned {
				t.Fatalf("got %+v learned=%t, want %+v learned=%t", got, learned, test.want, test.learned)
			}
		})
	}
	capped := make([]db.TaskEstimateSample, estimateSampleLimit+15)
	for i := range capped {
		capped[i] = sample("box", key.Repository, 3000)
		if i >= estimateSampleLimit {
			capped[i].ActualMinutes = 1
		}
	}
	got, learned := chooseTaskEstimate(key, capped)
	if !learned || got.Minutes != 24*60 || got.SampleCount != estimateSampleLimit {
		t.Fatalf("bounded median = %+v (%t)", got, learned)
	}
}

type deadlineExhaustingEstimateStore struct {
	*memStore
	todoStore
	taskEstimateStore
	recovered      atomic.Bool
	reconcileCalls atomic.Int32
	sampleCalls    atomic.Int32
}

// ReconcileTaskEstimateRounds restores the fifth accepted round, as after an
// interrupted projection, then keeps working until its deadline.
func (d *deadlineExhaustingEstimateStore) ReconcileTaskEstimateRounds(ctx context.Context, _ db.TaskEstimateCohort, _, _ time.Time, _ int) error {
	d.reconcileCalls.Add(1)
	if d.sampleCalls.Load() == 0 {
		d.recovered.Store(true)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (d *deadlineExhaustingEstimateStore) RecentTaskEstimateSamples(ctx context.Context, _ db.TaskEstimateCohort, _, _ time.Time, _ int) ([]db.TaskEstimateSample, error) {
	d.sampleCalls.Add(1)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	samples := []db.TaskEstimateSample{
		{Machine: "box", ActualMinutes: 8},
		{Machine: "box", ActualMinutes: 10},
		{Machine: "box", ActualMinutes: 12},
		{Machine: "box", ActualMinutes: 14},
	}
	if d.recovered.Load() {
		samples = append(samples, db.TaskEstimateSample{Machine: "box", ActualMinutes: 120})
	}
	return samples, nil
}

// Slow recovery must neither hide durable history nor run after the read
// whose missing sample it restores.
func TestTaskEstimateHistorySurvivesReconciliationDeadline(t *testing.T) {
	store := &deadlineExhaustingEstimateStore{memStore: &memStore{}}
	s, _, _ := briefServiceOn(t, nil, "please fix the fixture issue")
	s.store = func() Store { return store }
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	task := PlanTask{Goal: "Return a fixture answer", Readonly: true, Machine: "box"}
	s.estimateNewPlanTask(ctx, "conv1", &task)
	want := TaskEstimateEvidence{Minutes: 12, SampleCount: 5, Cohort: estimateMachine}
	if task.ExpectMinutes != 12 || task.EstimateSource != briefEstimateLearned ||
		task.EstimateEvidence == nil || *task.EstimateEvidence != want {
		t.Fatalf("reconciliation deadline hid durable history: %d minutes (%s), evidence %+v",
			task.ExpectMinutes, task.EstimateSource, task.EstimateEvidence)
	}
	if store.reconcileCalls.Load() != 1 || store.sampleCalls.Load() != 1 {
		t.Fatalf("estimate store calls: reconcile=%d samples=%d", store.reconcileCalls.Load(), store.sampleCalls.Load())
	}
}
