package resolvermaterialize

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/resolvercatalogid"
	"github.com/bmeddeb/phebs/internal/store"
)

func TestReconcileCurrentResolverPublication(t *testing.T) {
	wantErr := errors.New("current resolver observation failed")
	for _, test := range []struct {
		name    string
		change  func(*workerFixture)
		current bool
		err     error
	}{
		{name: "current", current: true},
		{name: "missing cache", change: func(f *workerFixture) { f.worker.forget(f.repository) }},
		{name: "missing pointer", change: func(f *workerFixture) { f.state.pointer = nil }},
		{name: "invalid pointer", change: func(f *workerFixture) { f.state.pointerErr = store.ErrInvalidResolverCatalogPublication }},
		{name: "pointer changed", change: func(f *workerFixture) { f.state.pointer.ManifestDigest = workerTestDigest('f') }},
		{name: "declaration not settled", change: func(f *workerFixture) { delete(f.state.outcomes, "proto-contract") }},
		{name: "declaration observation error", change: func(f *workerFixture) { f.state.outcomeErr = wantErr }, err: wantErr},
		{name: "declaration authority changed", change: func(f *workerFixture) { f.state.authorityCurrent = false }},
		{name: "authority error", change: func(f *workerFixture) { f.state.authorityErr = wantErr }, err: wantErr},
		{name: "marker", change: func(f *workerFixture) {
			if err := os.WriteFile(filepath.Join(f.worker.root, resolvercatalogid.PublishingName(f.repository)), []byte("pending"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "artifact replaced", change: func(f *workerFixture) {
			name := filepath.Join(f.worker.root, f.state.pointer.ManifestPath)
			if err := os.Rename(name, name+".old"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(name, []byte("replaced"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkerFixture(t)
			fixture.publishDeclaration(fixture.digest)
			if err := fixture.handle(false); err != nil {
				t.Fatal(err)
			}
			if test.change != nil {
				test.change(fixture)
			}
			published := 0
			fixture.worker.OnPublished = func(context.Context, string) error { published++; return nil }
			opens, reads, writes := fixture.provider.openCalls, fixture.blobReads, fixture.state.publishCalls
			current, err := fixture.worker.ReconcileCurrent(t.Context(), fixture.repository)
			if current != test.current || !errors.Is(err, test.err) {
				t.Fatalf("current=%t err=%v", current, err)
			}
			if (published == 1) != test.current || fixture.provider.openCalls != opens || fixture.blobReads != reads || fixture.state.publishCalls != writes {
				t.Fatalf("unexpected callback or content/publication work: callback=%d", published)
			}
		})
	}
}

func TestReconcileCurrentResolverCallbackRetryAndCancellation(t *testing.T) {
	fixture := newWorkerFixture(t)
	fixture.publishDeclaration(fixture.digest)
	if err := fixture.handle(false); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("downstream unavailable")
	calls := 0
	fixture.worker.OnPublished = func(context.Context, string) error {
		calls++
		if calls == 1 {
			return wantErr
		}
		return nil
	}
	if current, err := fixture.worker.ReconcileCurrent(t.Context(), fixture.repository); current || !errors.Is(err, wantErr) {
		t.Fatalf("first callback: %t %v", current, err)
	}
	for range 56 {
		if current, err := fixture.worker.ReconcileCurrent(t.Context(), fixture.repository); !current || err != nil {
			t.Fatalf("retry callback: %t %v", current, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := fixture.state.pointerCalls
	if current, err := fixture.worker.ReconcileCurrent(ctx, fixture.repository); current || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled: %t %v", current, err)
	}
	if calls != 57 || fixture.state.pointerCalls != before {
		t.Fatalf("calls=%d pointer calls=%d", calls, fixture.state.pointerCalls)
	}
}

func TestReconcileCurrentResolverDetectsNewEnabledDeclaration(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "still unavailable", true: "newly published"}[changed], func(t *testing.T) {
			fixture := newWorkerFixture(t)
			fixture.publishDeclaration(fixture.digest)
			outcome := fixture.state.outcomes["proto-contract"]
			outcome.Disposition = store.DomainOutcomeUnavailablePrerequisite
			outcome.RunID = ""
			if err := fixture.handle(false); err != nil {
				t.Fatal(err)
			}
			if len(fixture.state.pointer.Declarations) != 0 {
				t.Fatal("fixture did not publish settled-empty resolver")
			}
			if changed {
				fixture.publishDeclaration(fixture.digest)
			}
			callbacks := 0
			fixture.worker.OnPublished = func(context.Context, string) error { callbacks++; return nil }
			opens, reads, writes := fixture.provider.openCalls, fixture.blobReads, fixture.state.publishCalls
			current, err := fixture.worker.ReconcileCurrent(t.Context(), fixture.repository)
			if err != nil || current == changed {
				t.Fatalf("current=%t changed=%t err=%v", current, changed, err)
			}
			if (callbacks == 1) != current || opens != fixture.provider.openCalls || reads != fixture.blobReads || writes != fixture.state.publishCalls {
				t.Fatal("unexpected callback, content read, or publication")
			}
		})
	}
}
