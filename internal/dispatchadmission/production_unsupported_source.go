package dispatchadmission

import (
	"context"

	"github.com/bmeddeb/phebs/internal/readaccounting"
)

func ObserveProductionUnsupportedSource(ctx context.Context, event readaccounting.UnsupportedSourceObservation) (uint32, error) {
	selected := ProductionWorkSelected()
	observed, err := readaccounting.ObserveUnsupportedSource(ctx, selected, event)
	if err != nil && selected {
		if lifetime := productionRuntime.Load(); lifetime != nil && lifetime.client != nil {
			return observed, lifetime.client.fail(ErrProtocol)
		}
		return observed, ErrProductionBootstrap
	}
	return observed, err
}
