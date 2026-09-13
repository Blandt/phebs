package t4013

import (
	"context"
	"testing"
)

func TestObserveProcessExecutablePathsRefusesInvalidOrCanceledInput(t *testing.T) {
	for _, pids := range [][]int{nil, {0}, {1, 1}, {1, 2, 3}} {
		if paths, err := ObserveProcessExecutablePaths(t.Context(), pids); err == nil || paths != nil {
			t.Fatalf("invalid PID cardinality admitted: %v, %v", paths, err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if paths, err := ObserveProcessExecutablePaths(ctx, []int{1}); err == nil || paths != nil {
		t.Fatalf("canceled observation admitted: %v, %v", paths, err)
	}
	if path, err := ObserveProcessExecutablePath(t.Context(), 0); err == nil || path != "" {
		t.Fatalf("invalid single-PID observation admitted: %q, %v", path, err)
	}
}
