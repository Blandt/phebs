package t421

import (
	"reflect"
	"testing"
	"time"
)

func TestExecutionPhaseEventRecorder(t *testing.T) {
	newRecorder := func(t *testing.T) (*executionPhaseEventRecorder, time.Time) {
		t.Helper()
		ordinals := newExecutionEventOrdinals()
		admitted, ordinal, err := ordinals.consumeFinalAdmission()
		if err != nil || ordinal != 1 {
			t.Fatal("final admission did not reserve ordinal one", ordinal, err)
		}
		started := time.Unix(1_800_000_000, 0)
		recorder, err := newExecutionPhaseEventRecorder(admitted, frozenPhaseOrder(), started, started.Add(18*time.Hour))
		if err != nil {
			t.Fatal("recorder construction failed", err)
		}
		return recorder, started
	}

	t.Run("complete", func(t *testing.T) {
		recorder, outer := newRecorder(t)
		phases := frozenPhaseOrder()
		for index, phase := range phases {
			started := outer.Add(time.Duration(index*3) * time.Millisecond)
			outcome := "passed"
			if phase == "teardown" {
				outcome = "clean"
			}
			if err := recorder.beginAt(phase, started); err != nil ||
				recorder.finishAt(phase, outcome, started.Add(1500*time.Microsecond)) != nil {
				t.Fatalf("phase %q was not recorded", phase)
			}
		}
		got, err := recorder.snapshot()
		if err != nil || len(got) != len(phases) {
			t.Fatal("complete event inventory unavailable", err)
		}
		var prior uint64 = 1
		for index, value := range got {
			if value.Phase != phases[index] || value.StartEventOrdinal <= prior ||
				value.FinishEventOrdinal <= value.StartEventOrdinal || value.Metrics.WallMS != 2 {
				t.Fatalf("phase %d is not exact: %+v", index, value)
			}
			prior = value.FinishEventOrdinal
		}
		got[0].Phase = "changed"
		again, err := recorder.snapshot()
		if err != nil || again[0].Phase != phases[0] || reflect.DeepEqual(got, again) {
			t.Fatal("snapshot aliases retained evidence", err)
		}
		if err := recorder.beginAt(phases[0], outer.Add(time.Hour)); err == nil {
			t.Fatal("completed recorder was reused")
		}
	})

	t.Run("stopped_suffix", func(t *testing.T) {
		recorder, outer := newRecorder(t)
		phases := frozenPhaseOrder()
		for index, phase := range phases[:5] {
			started := outer.Add(time.Duration(index*3) * time.Millisecond)
			outcome := "passed"
			if index == 4 {
				outcome = "stopped"
			}
			if recorder.beginAt(phase, started) != nil || recorder.finishAt(phase, outcome, started.Add(time.Millisecond)) != nil {
				t.Fatalf("phase %q was not recorded", phase)
			}
		}
		teardownStart := outer.Add(time.Minute)
		if recorder.beginAt("teardown", teardownStart) != nil ||
			recorder.finishAt("teardown", "failed", teardownStart.Add(time.Millisecond)) != nil {
			t.Fatal("unconditional teardown was not recorded after stop")
		}
		got, err := recorder.snapshot()
		if err != nil || len(got) != len(phases) || got[4].StartEventOrdinal == 0 || got[14].StartEventOrdinal == 0 {
			t.Fatal("stopped inventory unavailable", err, got)
		}
		for index := 5; index < 14; index++ {
			if !reflect.DeepEqual(got[index], PhaseMeasurement{Phase: phases[index]}) {
				t.Fatalf("phase %q is not an empty stopped suffix: %+v", phases[index], got[index])
			}
		}
		if got[14].StartEventOrdinal <= got[4].FinishEventOrdinal || got[14].FinishEventOrdinal <= got[14].StartEventOrdinal {
			t.Fatal("teardown did not continue the global ordinal stream")
		}
		if err := recorder.beginAt("teardown", outer.Add(2*time.Minute)); err == nil {
			t.Fatal("teardown recorder was reused")
		}
	})
}
