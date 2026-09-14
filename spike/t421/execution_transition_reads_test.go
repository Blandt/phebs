package t421

import (
	"math"
	"testing"
)

func TestExecutionTransitionReadPrefix(t *testing.T) {
	for _, test := range []struct {
		path       string
		transition bool
	}{
		{"/api/t422/retention/current-prior", true}, {"/api/t422/logical-activation/hit", true},
		{"/api/t422/return-a-marker/recovered", true}, {"/api/t422/checkpoint/hit", true},
		{"/api/t422/stale-lease/recovered", true}, {"/api/t422/lifecycle/pressure-80", true},
		{"/api/t422/archive/transition", true}, {"/api/t421/final-authority", false},
		{"/api/t421/tail-readiness", false},
	} {
		t.Run(test.path, func(t *testing.T) {
			reader := &executionEpochInspection{}
			reader.bounds.TransitionReadClass = correctedPhysicalTransitionReadClass
			reader.evidence.rows = []ExecutionPhaseInspection{{Phase: "physical_delta_b"}}
			if err := reader.retainTransitionReads(test.path, epochInspectionReport{ControlFileReads: 3, MemberVisits: 7}); err != nil {
				t.Fatal(err)
			}
			got := reader.evidence.rows[0].TransitionReads
			if !test.transition {
				if got != nil {
					t.Fatal("ordinary read entered transition subtotal")
				}
				return
			}
			if got == nil || got.ReportCalls != 1 || got.ControlFileReads != 3 || got.MemberReads != 7 {
				t.Fatal("actual report lost")
			}
			copy := cloneInspectionEvidence(reader.evidence.rows)
			got.ReportCalls = math.MaxUint64
			if err := reader.retainTransitionReads(test.path, epochInspectionReport{}); err == nil || got.ControlFileReads != 3 {
				t.Fatal("overflow changed prefix")
			}
			if copy[0].TransitionReads.ReportCalls != 1 {
				t.Fatal("subtotal aliases caller")
			}
		})
	}
}

func TestExecutionStoppedStartupRetainsTerminalObservation(t *testing.T) {
	for _, test := range []struct {
		name                              string
		identity, ready, stopped, ordinal uint64
		fail                              bool
	}{
		{"terminal failure", 4, 0, 0, 5, false}, {"missing native identity", 0, 0, 0, 5, true},
		{"before native identity", 4, 0, 0, 3, true}, {"already ready", 4, 5, 0, 6, true},
		{"already stopped", 4, 0, 5, 6, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			meter := &epochProcessObservation{result: ExecutionServerProcessObservation{
				NativeIdentityEventOrdinal: test.identity, HealthReadyEventOrdinal: test.ready, HealthStoppedEventOrdinal: test.stopped}}
			err := meter.stoppedStartup(test.ordinal, 3)
			if (err != nil) != test.fail {
				t.Fatal("unexpected terminal event outcome", err)
			}
			if !test.fail && (meter.result.HealthStoppedEventOrdinal != test.ordinal || meter.result.HealthElapsedMS != 3 || meter.result.HealthReadyEventOrdinal != 0) {
				t.Fatal("stopped startup was not retained exactly")
			}
		})
	}
}
