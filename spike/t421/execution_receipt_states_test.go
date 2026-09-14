package t421

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

func TestComposeExecutionReceiptStatesModeledObservations(t *testing.T) {
	plan := clonePlan(t, correctedTestPlan(t))
	if err := applyProcessAccountingCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	binding := frozenReceiptTestBinding(t, plan)
	fixture := Receipt{PhaseResults: make([]PhaseResult, len(plan.PhaseOrder)), Measurements: make([]PhaseMeasurement, len(plan.PhaseOrder))}
	for index, phase := range plan.PhaseOrder {
		outcome := "passed"
		if phase == "teardown" {
			outcome = plan.ReceiptContract.TeardownPhaseOutcome
		}
		fixture.PhaseResults[index] = PhaseResult{Name: phase, Outcome: outcome}
		fixture.Measurements[index] = testPhaseMeasurement(plan, binding.freeze, index)
	}
	authorities := make([]AuthorityPhaseResult, len(plan.PhaseOrder)-2)
	for index, phase := range plan.PhaseOrder[1 : len(plan.PhaseOrder)-1] {
		projection, err := expectedStateProjectionForPhase(plan, phase)
		if err != nil {
			t.Fatal(err)
		}
		authorities[index] = AuthorityPhaseResult{Phase: phase, Outcome: "passed", AuthorityState: AuthorityState{
			PhysicalRevision: projection.PhysicalRevision, LogicalRevision: projection.LogicalRevision, Current: true,
			SearchInventory: projection.SearchInventory, ObservationInputInventory: projection.ObservationInputInventory}}
		observed := testObservedPhaseState(t, plan, phase, authorities[index])
		runtime := testPhaseRuntime(t, binding.freeze, phase, fixture.Measurements)
		if runtime.Startup != nil {
			runtime.Startup.FinishEventOrdinal++
		}
		fixture.StateResults = append(fixture.StateResults, ExactPhaseEvidence{Phase: phase, Outcome: "passed",
			ExpectedSHA256: observed.ProjectionSHA256, ObservedProjectionSHA256: observed.ProjectionSHA256,
			ObservedSHA256: mustReceiptSHA256(t, observed), Observed: &observed,
			RuntimeSHA256: mustReceiptSHA256(t, runtime), Runtime: &runtime})
	}
	servers := make([]ExecutionEpochOneResult, 5)
	for index, state := range fixture.StateResults {
		runtime := state.Runtime
		server := &servers[runtime.ServerEpoch-1]
		server.RootStarted, server.RootJoined, server.SessionEmpty = true, true, true
		if runtime.OwnedStart != nil {
			server.ServerProcesses = ExecutionServerProcessObservation{Joined: true, ServerEpoch: runtime.ServerEpoch,
				LaunchPhase: state.Phase, StartEventOrdinal: runtime.StartEventOrdinal,
				NativeIdentityEventOrdinal: runtime.StartEventOrdinal + 1,
				NativeIdentitySHA256:       runtime.OwnedStart.NativeIdentitySHA256,
				HealthReadyEventOrdinal:    runtime.Startup.FinishEventOrdinal, HealthElapsedMS: *runtime.Startup.ElapsedMS}
		}
		projection, err := expectedStateProjectionForPhase(plan, state.Phase)
		if err != nil {
			t.Fatal(err)
		}
		server.Inspection = append(server.Inspection, ExecutionPhaseInspection{ServerEpoch: runtime.ServerEpoch,
			Phase: state.Phase, FirstOrdinal: 1, NextOrdinal: 2, AcceptedReports: 1, SelectorAccepted: true,
			Final: &ExecutionInspectionFinal{Ordinal: 1, Authority: authorities[index].AuthorityState, Projection: projection}})
	}
	got, err := composeExecutionReceiptStates(plan, binding.freeze, fixture.PhaseResults, fixture.Measurements, authorities, servers)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, fixture.StateResults) {
		t.Fatal("actual modeled state/runtime projection changed")
	}
	// Joining is a teardown observation. Accepted state and native start facts
	// must survive a later failure to stop the owning server.
	retained := slices.Clone(servers)
	retained[0].RootJoined, retained[0].SessionEmpty, retained[0].ServerProcesses.Joined = false, false, false
	preserved, err := composeExecutionReceiptStates(plan, binding.freeze, fixture.PhaseResults, fixture.Measurements, authorities, retained)
	if err != nil || !reflect.DeepEqual(preserved, got) {
		t.Fatal("cleanup refusal erased accepted state", err)
	}
	for _, test := range []struct {
		name string
		edit func([]ExecutionEpochOneResult)
	}{
		{"unstarted", func(v []ExecutionEpochOneResult) { v[0].RootStarted = false }},
		{"unaccepted", func(v []ExecutionEpochOneResult) { v[0].Inspection[0].SelectorAccepted = false }},
		{"missing final", func(v []ExecutionEpochOneResult) { v[0].Inspection[0].Final = nil }},
		{"wrong observed projection", func(v []ExecutionEpochOneResult) {
			v[0].Inspection[0].Final.Projection.CatalogLogicalSHA256 = testDigest("different observation")
		}},
		{"missing native identity", func(v []ExecutionEpochOneResult) { v[0].ServerProcesses.NativeIdentitySHA256 = "" }},
		{"missing readiness", func(v []ExecutionEpochOneResult) { v[0].ServerProcesses.HealthReadyEventOrdinal = 0 }},
		{"wrong epoch", func(v []ExecutionEpochOneResult) { v[0].ServerProcesses.ServerEpoch = 2 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			values := slices.Clone(servers)
			for index := range values {
				values[index].Inspection = cloneInspectionEvidence(values[index].Inspection)
			}
			test.edit(values)
			if _, err := composeExecutionReceiptStates(plan, binding.freeze, fixture.PhaseResults, fixture.Measurements, authorities, values); !errors.Is(err, errExecutionReceiptStates) {
				t.Fatal("missing observed state accepted", err)
			}
		})
	}
}
