package t421

import (
	"errors"
	"testing"
	"time"
)

func TestPressureMutationObservationPreservesNativePrefix(t *testing.T) {
	for _, failed := range []bool{false, true} {
		reader := &executionEpochInspection{}
		mutation := executionPressureBallastMutation{Before: executionPressureBallastSample{Used: 13, Available: 29, Allocated: 3}, After: executionPressureBallastSample{Used: 21, Available: 21, Allocated: 11}, Fence: time.Unix(12, 34)}
		var err error
		if failed {
			err = errors.New("native mutation stopped")
		}
		reader.retainPressureBallast(0, mutation, err)
		got := reader.pressure.ballast[0]
		if !got.Attempted || got.Complete == failed || got.Mutation != mutation {
			t.Fatal(got)
		}
		clone := cloneExecutionTransitionObservations(executionTransitionObservations{pressure: reader.pressure})
		clone.pressure.ballast[0].Mutation.Before.Used++
		if reader.pressure.ballast[0] != got {
			t.Fatal("snapshot aliases native prefix")
		}
		if reader.pressure.ballast[1].Attempted {
			t.Fatal("missing suffix became observed")
		}
	}
}
