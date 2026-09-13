package readaccounting

import (
	"context"
)

type UnsupportedSourceObservation struct {
	Unsupported uint64
}

type unsupportedSourceObserverKey struct{}

func WithUnsupportedSourceObserver(ctx context.Context, observe func(UnsupportedSourceObservation) (uint32, error)) (context.Context, error) {
	if ctx == nil || observe == nil || ctx.Value(unsupportedSourceObserverKey{}) != nil {
		return nil, ErrScope
	}
	return context.WithValue(ctx, unsupportedSourceObserverKey{}, observe), nil
}

// ObserveUnsupportedSource reports the validated aggregate unsupported count
// returned by one actual inventory-v2 handler attempt.
func ObserveUnsupportedSource(ctx context.Context, required bool, event UnsupportedSourceObservation) (observed uint32, err error) {
	var observe func(UnsupportedSourceObservation) (uint32, error)
	if ctx != nil {
		observe, _ = ctx.Value(unsupportedSourceObserverKey{}).(func(UnsupportedSourceObservation) (uint32, error))
	}
	if observe == nil {
		if required {
			return 0, ErrScope
		}
		return 0, nil
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	defer func() {
		if recover() != nil {
			observed, err = 0, ErrEvent
		}
	}()
	observed, err = observe(event)
	if err != nil {
		return observed, err
	}
	if observed < 1 || observed > 15 {
		return observed, ErrEvent
	}
	return observed, ctx.Err()
}
