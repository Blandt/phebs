package main

import (
	"context"
	"sync"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/readaccounting"
	"github.com/bmeddeb/phebs/internal/resolvermaterialize"
)

type t422ResolverCatalogCountsKey struct{}

type t422ResolverCatalogCountsControl struct {
	mu     sync.Mutex
	latest *readaccounting.ResolverCatalogCounts
}

// Capture the actual sealed builder counters in the selected server only.
// Existing F calls carry a matching snapshot, avoiding one wire record per
// job and preserving the existing fixed output/read/dispatch admission.
func bindT422ResolverCatalogCounts(ctx context.Context, initial dispatchadmission.ProductionSemanticSnapshot, fail func(error)) (context.Context, error) {
	if initial.Mode != dispatchadmission.ProductionSemanticV3 {
		return ctx, nil
	}
	if ctx == nil || fail == nil || !t422WorkPhase(initial, true) || ctx.Value(t422ResolverCatalogCountsKey{}) != nil {
		return nil, errT422AttemptReport
	}
	control := &t422ResolverCatalogCountsControl{}
	ctx = context.WithValue(ctx, t422ResolverCatalogCountsKey{}, control)
	return readaccounting.WithResolverCatalogObserver(ctx, func(counts readaccounting.ResolverCatalogCounts) error {
		current, err := dispatchadmission.ProductionWorkState()
		if err == nil {
			_, err = t422SourceRecord(current, initial)
		}
		if err != nil || !t422AttemptDigest(counts.GenerationSHA256) || !t422AttemptDigest(counts.ManifestSHA256) ||
			counts.DeclarationRecords > resolvermaterialize.MaxDeclarationRecords || counts.GeneratedDescriptors > resolvermaterialize.MaxGeneratedSymbolDescriptors {
			fail(errT422AttemptReport)
			return errT422AttemptReport
		}
		if err := control.retain(counts); err != nil {
			fail(err)
			return err
		}
		return nil
	})
}

func t422FinalResolverCatalogCounts(ctx context.Context, generation, manifest string) (*readaccounting.ResolverCatalogCounts, error) {
	control, _ := ctx.Value(t422ResolverCatalogCountsKey{}).(*t422ResolverCatalogCountsControl)
	if control == nil {
		if dispatchadmission.ProductionWorkSelected() {
			return nil, errT422AttemptReport
		}
		return nil, nil
	}
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.latest == nil || control.latest.GenerationSHA256 != generation || control.latest.ManifestSHA256 != manifest {
		// A later epoch may reuse a catalog without building it. No local
		// observation is manufactured: the parent must retain a prior exact
		// generation/manifest observation or refuse the receipt.
		return nil, nil
	}
	counts := *control.latest
	return &counts, nil
}

// An unchanged sealed identity cannot legitimately acquire different counts.
func (control *t422ResolverCatalogCountsControl) retain(counts readaccounting.ResolverCatalogCounts) error {
	control.mu.Lock()
	defer control.mu.Unlock()
	if control.latest != nil && control.latest.GenerationSHA256 == counts.GenerationSHA256 && control.latest.ManifestSHA256 == counts.ManifestSHA256 && *control.latest != counts {
		return errT422AttemptReport
	}
	control.latest = &counts
	return nil
}
