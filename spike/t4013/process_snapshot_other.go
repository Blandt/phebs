//go:build !darwin

package t4013

import (
	"context"
	"errors"
)

func nativeProcessSnapshotProbe() func(context.Context, int) ([]int, map[int]processSnapshot, error) {
	return nil
}

// nativeMemberObservation fails closed: only the Darwin collector observes one
// coherent per-member kernel record without a process-tree walk.
func nativeMemberObservation(int) (processSnapshot, error) {
	return processSnapshot{}, errors.New("native process-member observation requires macOS")
}
