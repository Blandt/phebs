package observationpublication

import (
	"errors"
	"testing"

	"github.com/bmeddeb/phebs/internal/readaccounting"
)

func TestObserveUnsupportedSourceCount(t *testing.T) {
	called := 0
	ctx, err := readaccounting.WithUnsupportedSourceObserver(t.Context(), func(readaccounting.UnsupportedSourceObservation) (uint32, error) {
		called++
		return 2, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := observeUnsupportedSourceCount(ctx, -1); err == nil || called != 0 {
		t.Fatal(err, called)
	}
	if err := observeUnsupportedSourceCount(ctx, 0); err != nil || called != 1 {
		t.Fatal(err, called)
	}
	sink, err := readaccounting.WithUnsupportedSourceObserver(t.Context(), func(readaccounting.UnsupportedSourceObservation) (uint32, error) {
		return 0, errors.New("sink")
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := observeUnsupportedSourceCount(sink, 1); err == nil {
		t.Fatal("sink refusal lost")
	}
}
