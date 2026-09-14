package main

import (
	"context"
	"errors"
	"testing"

	"github.com/bmeddeb/phebs/internal/store"
)

func TestEnqueueUnlessCurrent(t *testing.T) {
	checkErr, queueErr := errors.New("authority unavailable"), errors.New("queue unavailable")
	for _, test := range []struct {
		name                    string
		missing, current, force bool
		checkErr, queueErr      error
		wantChecks, wantQueues  int
	}{
		{name: "cold", missing: true, wantQueues: 1},
		{name: "current", current: true, wantChecks: 1},
		{name: "stale", wantChecks: 1, wantQueues: 1},
		{name: "forced", current: true, force: true, wantQueues: 1},
		{name: "authority or callback failure", checkErr: checkErr, wantChecks: 1, wantQueues: 1},
		{name: "inconsistent success", current: true, checkErr: checkErr, wantChecks: 1, wantQueues: 1},
		{name: "both failures", checkErr: checkErr, queueErr: queueErr, wantChecks: 1, wantQueues: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			checks, queues := 0, 0
			const repository = "example.invalid/repository"
			current := func(ctx context.Context, target string) (bool, error) {
				if ctx != t.Context() || target != repository {
					t.Fatal("changed current binding")
				}
				checks++
				return test.current, test.checkErr
			}
			if test.missing {
				current = nil
			}
			queued := &store.Job{}
			job, err := enqueueUnlessCurrent(t.Context(), store.JobCallerLeaf, repository, test.force, current,
				func(ctx context.Context, kind store.JobKind, target string, force bool) (*store.Job, error) {
					if ctx != t.Context() || kind != store.JobCallerLeaf || target != repository || force != test.force {
						t.Fatal("changed enqueue binding")
					}
					queues++
					return queued, test.queueErr
				})
			if checks != test.wantChecks || queues != test.wantQueues || (queues == 1 && job != queued) || (queues == 0 && job != nil) {
				t.Fatalf("checks=%d queues=%d job=%v", checks, queues, job)
			}
			if (err == nil) != (test.checkErr == nil && test.queueErr == nil) {
				t.Fatalf("error=%v", err)
			}
			for _, want := range []error{test.checkErr, test.queueErr} {
				if want != nil && !errors.Is(err, want) {
					t.Fatalf("error=%v missing %v", err, want)
				}
			}
		})
	}
}

func TestCurrentPartitionSettlementPreservesCallbacksWithoutJobs(t *testing.T) {
	const repository = "example.invalid/repeated-settlement"
	reconciles, advances, callers, queues := 0, 0, 0, 0
	reconcile := func(context.Context, string) error { reconciles++; return nil }
	advance := func(context.Context, string) error { advances++; return nil }
	enqueue := func(context.Context, store.JobKind, string, bool) (*store.Job, error) {
		queues++
		return &store.Job{}, nil
	}
	callerCurrent := func(context.Context, string) (bool, error) { callers++; return true, nil }
	enqueueCaller := func(ctx context.Context, kind store.JobKind, repo string, force bool) (*store.Job, error) {
		return enqueueUnlessCurrent(ctx, kind, repo, force, callerCurrent, enqueue)
	}
	resolverCurrent := func(ctx context.Context, repo string) (bool, error) {
		err := afterResolverPublication(ctx, repo, reconcile, advance, enqueueCaller, true)
		return err == nil, err
	}
	enqueueDownstream := func(ctx context.Context, kind store.JobKind, repo string, force bool) (*store.Job, error) {
		current := callerCurrent
		if kind == store.JobResolverCatalog {
			current = resolverCurrent
		}
		return enqueueUnlessCurrent(ctx, kind, repo, force, current, enqueue)
	}
	for range 56 {
		if err := afterPartitionExtractionSettlement(t.Context(), repository, reconcile, enqueueDownstream, true, true); err != nil {
			t.Fatal(err)
		}
	}
	if queues != 0 || reconciles != 112 || advances != 56 || callers != 112 {
		t.Fatalf("queues=%d reconciles=%d advances=%d callers=%d", queues, reconciles, advances, callers)
	}
}
