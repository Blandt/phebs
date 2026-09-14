package callerexecute

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/callerleaf"
	"github.com/bmeddeb/phebs/internal/callerpublicationid"
	"github.com/bmeddeb/phebs/internal/store"
)

type currentPublicationStore struct {
	*workerTestStore
	warmCalls, coldCalls int
	warmErr              error
	warmFalse            bool
}

func (state *currentPublicationStore) CallerGenerationPublicationSummaryAuthorityCurrent(ctx context.Context, value store.CallerGenerationPublicationSummary) (bool, error) {
	state.warmCalls++
	if state.warmErr != nil || state.warmFalse {
		return false, state.warmErr
	}
	return state.workerTestStore.CallerGenerationPublicationSummaryAuthorityCurrent(ctx, value)
}

func (state *currentPublicationStore) CallerGenerationPublicationSummaryCurrent(context.Context, store.CallerGenerationPublicationSummary) (bool, error) {
	state.coldCalls++
	return false, errors.New("unexpected cold pair hash")
}

func TestReconcileCurrentCallerPublication(t *testing.T) {
	wantErr := errors.New("current caller observation failed")
	for _, test := range []struct {
		name    string
		change  func(workerHarness, *currentPublicationStore)
		current bool
		err     error
	}{
		{name: "current", current: true},
		{name: "missing cache", change: func(h workerHarness, _ *currentPublicationStore) { h.worker.forgetValidation(h.worker.validations.key) }},
		{name: "missing pointer", change: func(h workerHarness, _ *currentPublicationStore) { h.state.publication = nil }},
		{name: "resolver authority changed", change: func(h workerHarness, _ *currentPublicationStore) { h.state.resolverStale.Store(true) }},
		{name: "candidate generation changed", change: func(h workerHarness, _ *currentPublicationStore) {
			h.state.candidate.ManifestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		}},
		{name: "store authority changed", change: func(_ workerHarness, s *currentPublicationStore) { s.warmFalse = true }},
		{name: "store authority error", change: func(_ workerHarness, s *currentPublicationStore) { s.warmErr = wantErr }, err: wantErr},
		{name: "invalid warm summary", change: func(_ workerHarness, s *currentPublicationStore) {
			s.warmErr = store.ErrInvalidCallerGenerationPublication
		}},
		{name: "marker", change: func(h workerHarness, _ *currentPublicationStore) {
			path := filepath.Join(h.worker.root, callerpublicationid.RepositoryDirectory(h.state.repo.Name), callerpublicationid.PublishingName)
			if err := os.WriteFile(path, []byte("pending"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "artifact removed", change: func(h workerHarness, _ *currentPublicationStore) {
			path, err := callerleaf.ArtifactPath(h.worker.root, h.state.repo.Name, artifactReceipt(*h.state.outcomes[0].Receipt))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := newWorkerHarness(t, 1)
			h.settle(t)
			state := &currentPublicationStore{workerTestStore: h.state}
			h.worker.store = state
			if test.change != nil {
				test.change(h, state)
			}
			callbacks := 0
			h.worker.OnPublished = func(context.Context, string) error { callbacks++; return nil }
			opens, events := h.provider.opens, len(h.state.events)
			current, err := h.worker.ReconcileCurrent(t.Context(), h.state.repo.Name)
			if current != test.current || !errors.Is(err, test.err) {
				t.Fatalf("current=%t err=%v", current, err)
			}
			if (callbacks == 1) != test.current || state.coldCalls != 0 || h.provider.opens != opens || len(h.state.events) != events {
				t.Fatalf("unexpected callback, cold read or mutation: callbacks=%d cold=%d", callbacks, state.coldCalls)
			}
		})
	}
}

// Selecting native partitioned evidence must require its actual observation
// authority even when the old cached caller and every legacy store pointer are
// still valid. No member, source or native database fixture is needed to prove
// that the missing prerequisite prevents the shortcut.
type currentPartitionedPublicationStore struct {
	*currentPublicationStore
	store.PartitionedEvidenceStore
}

func TestReconcileCurrentCallerRequiresPartitionedAuthority(t *testing.T) {
	h := newWorkerHarness(t, 1)
	h.settle(t)
	state := &currentPublicationStore{workerTestStore: h.state}
	h.worker.store = &currentPartitionedPublicationStore{currentPublicationStore: state}
	h.worker.OnPublished = func(context.Context, string) error {
		t.Fatal("missing partitioned authority reached downstream")
		return nil
	}
	if current, err := h.worker.ReconcileCurrent(t.Context(), h.state.repo.Name); current || err != nil {
		t.Fatalf("missing observation must preserve queued recovery: current=%t err=%v", current, err)
	}
	if state.warmCalls != 0 || state.coldCalls != 0 {
		t.Fatalf("caller summary was trusted before native upstream: warm=%d cold=%d", state.warmCalls, state.coldCalls)
	}
}

func TestReconcileCurrentCallerCallbackRetryAndCancellation(t *testing.T) {
	h := newWorkerHarness(t, 1)
	h.settle(t)
	state := &currentPublicationStore{workerTestStore: h.state}
	h.worker.store = state
	wantErr := errors.New("downstream unavailable")
	calls := 0
	h.worker.OnPublished = func(context.Context, string) error {
		calls++
		if calls == 1 {
			return wantErr
		}
		return nil
	}
	if current, err := h.worker.ReconcileCurrent(t.Context(), h.state.repo.Name); current || !errors.Is(err, wantErr) {
		t.Fatalf("first callback: %t %v", current, err)
	}
	for range 56 {
		if current, err := h.worker.ReconcileCurrent(t.Context(), h.state.repo.Name); !current || err != nil {
			t.Fatalf("retry callback: %t %v", current, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := state.warmCalls
	if current, err := h.worker.ReconcileCurrent(ctx, h.state.repo.Name); current || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %t %v", current, err)
	}
	if calls != 57 || state.warmCalls != before || state.coldCalls != 0 {
		t.Fatalf("calls=%d warm=%d cold=%d", calls, state.warmCalls, state.coldCalls)
	}
}
