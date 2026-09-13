package observationpublication

import (
	"context"

	"github.com/bmeddeb/phebs/internal/dispatchadmission"
	"github.com/bmeddeb/phebs/internal/readaccounting"
)

// observeUnsupportedSourceCount reports an aggregate from a root already
// validated by its caller. The selected V3 sink refuses nonzero before the
// published inventory is announced to downstream work.
func observeUnsupportedSourceCount(ctx context.Context, unsupported int) error {
	if unsupported < 0 {
		return invalid("unsupported source count")
	}
	_, err := dispatchadmission.ObserveProductionUnsupportedSource(ctx, readaccounting.UnsupportedSourceObservation{
		Unsupported: uint64(unsupported),
	})
	return err
}
