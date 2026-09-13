package readaccounting

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedSourceObserver(t *testing.T) {
	for _, test := range []struct {
		name             string
		required, bind   bool
		cancel, sinkFail bool
		wantCalls        int
		wantOK           bool
	}{
		{name: "ordinary", wantOK: true},
		{name: "missing", required: true},
		{name: "zero", required: true, bind: true, wantCalls: 1, wantOK: true},
		{name: "nonzero", required: true, bind: true, wantCalls: 1, wantOK: true},
		{name: "canceled", required: true, bind: true, cancel: true},
		{name: "sink", required: true, bind: true, sinkFail: true, wantCalls: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			if test.bind {
				var err error
				ctx, err = WithUnsupportedSourceObserver(ctx, func(UnsupportedSourceObservation) (uint32, error) {
					calls++
					if test.sinkFail {
						return 2, errors.New("sink")
					}
					return 2, nil
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			if test.cancel {
				cancel()
			}
			event := UnsupportedSourceObservation{}
			if test.name == "nonzero" {
				event.Unsupported = 1
			}
			_, err := ObserveUnsupportedSource(ctx, test.required, event)
			if (err == nil) != test.wantOK || calls != test.wantCalls {
				t.Fatal(err, calls)
			}
		})
	}
	if _, err := WithUnsupportedSourceObserver(t.Context(), func(UnsupportedSourceObservation) (uint32, error) { return 2, nil }); err != nil {
		t.Fatal(err)
	} else if _, err := WithUnsupportedSourceObserver(context.Background(), nil); !errors.Is(err, ErrScope) {
		t.Fatal(err)
	}
}
