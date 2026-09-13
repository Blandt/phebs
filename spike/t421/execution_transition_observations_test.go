package t421

import (
	"reflect"
	"testing"

	"github.com/bmeddeb/phebs/internal/lifecycle"
	"github.com/bmeddeb/phebs/internal/recovery"
)

func TestExecutionTransitionObservationsWaitDetached(t *testing.T) {
	observed := executionTransitionObservations{
		collectionCycle: lifecycle.CycleObservation{Owners: []lifecycle.CycleOwnerObservation{{Name: "collection"}}},
		archiveManifest: &recovery.ArchiveTransitionManifest{
			Components: []recovery.ArchiveTransitionComponent{{Name: "component"}},
			Reports:    []recovery.ArchiveTransitionReport{{Name: "report"}},
		},
	}
	observed.pressure.normal.Owners = []lifecycle.CycleOwnerObservation{{Name: "normal"}}
	observed.pressure.recovery.Owners = []lifecycle.CycleOwnerObservation{{Name: "recovery"}}
	run := &ExecutionEpochOneRun{flow: &ExecutionEpochOne{}, done: make(chan struct{}),
		result: ExecutionEpochOneResult{transitionObservations: cloneExecutionTransitionObservations(observed)}}
	close(run.done)
	first, err := run.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.transitionObservations.collectionCycle.Owners[0].Name = "changed"
	first.transitionObservations.pressure.normal.Owners[0].Name = "changed"
	first.transitionObservations.pressure.recovery.Owners[0].Name = "changed"
	first.transitionObservations.archiveManifest.Components[0].Name = "changed"
	first.transitionObservations.archiveManifest.Reports[0].Name = "changed"
	second, err := run.Wait(t.Context())
	if err != nil || !reflect.DeepEqual(second.transitionObservations, observed) {
		t.Fatal("returned snapshot changed retained native observations", err)
	}
}
