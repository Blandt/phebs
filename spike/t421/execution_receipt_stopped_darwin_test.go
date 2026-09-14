//go:build darwin

package t421

import "testing"

// No corpus constructor or native process is involved: these cases model an
// unavailable disk sample before any server starts, with real API-shaped data.
func TestAssembleExecutionReceiptEarlyStoppedModeledEvidence(t *testing.T) {
	plan := accountingTestPlan(t)
	binding := frozenReceiptTestBinding(t, plan)
	for _, test := range []struct {
		name, phase   string
		failedCleanup bool
	}{{"preflight", "preflight", false}, {"cold before server", "cold", false}, {"retained custody", "cold", true}} {
		t.Run(test.name, func(t *testing.T) {
			evidence := executionReceiptEvidence{Queries: QueryEvidence{Phase: "product_queries", Outcome: "not_run"}, Relationships: RelationshipEvidence{Phase: "product_queries", Outcome: "not_run"}}
			stopped := false
			for index, phase := range plan.PhaseOrder {
				outcome := "passed"
				measurement := testPhaseMeasurement(plan, binding.freeze, index)
				result := PhaseResult{Name: phase, Outcome: outcome}
				if phase == test.phase {
					stopped = true
					outcome = "stopped"
					observation := FailureObservation{Schema: plan.ReceiptContract.FailureObservationSchema, Kind: "measurement_unavailable", UnavailableMetrics: []string{"available_disk_bytes"}}
					observation.EvidenceSHA256 = mustReceiptSHA256(t, observation)
					result.Failure = &ReceiptFailure{Phase: phase, Class: "internal", Code: "measurement_unavailable", Observation: observation}
					measurement.Metrics.AvailableDiskBytes = 0
				} else if stopped {
					outcome = "not_run"
					measurement = PhaseMeasurement{Phase: phase}
				}
				if phase == "teardown" {
					outcome = plan.ReceiptContract.TeardownPhaseOutcome
					measurement = testPhaseMeasurement(plan, binding.freeze, index)
				}
				result.Outcome = outcome
				evidence.Phases = append(evidence.Phases, result)
				evidence.Measurements = append(evidence.Measurements, measurement)
			}
			outcomes, _, err := validateReceiptPhases(evidence.Phases, plan)
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range plan.PhaseOrder[1 : len(plan.PhaseOrder)-1] {
				row := AuthorityPhaseResult{Phase: phase, Outcome: outcomes[phase]}
				if row.Outcome == "stopped" {
					row.PhysicalRevision, row.LogicalRevision = "a", "a"
				}
				evidence.Authorities = append(evidence.Authorities, row)
			}
			evidence.Revisions, err = composeExecutionRevisionResults(plan, outcomes, nil)
			if err != nil {
				t.Fatal(err)
			}
			evidence.States, err = composeExecutionReceiptStates(plan, binding.freeze, evidence.Phases, evidence.Measurements, evidence.Authorities, nil)
			if err != nil {
				t.Fatal(err)
			}
			evidence.Transitions, err = composeExecutionReceiptTransitions(plan, binding.freeze, evidence.Phases, evidence.Measurements, evidence.Authorities, nil, nil, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			evidence.Teardown, _ = accountingTestTeardown(plan)
			evidence.Teardown.BackingVolumeIdentity = binding.freeze.Host.BackingVolumeIdentity
			if test.failedCleanup {
				evidence.Teardown.Outcome = "failed"
				evidence.Teardown.PressureVolumeDetached = false
				evidence.Teardown.VolumeDetachErrors = 1
				failed, err := receiptTeardownFailedChecks(evidence.Teardown, evidence.Measurements, plan, binding.freeze, false)
				if err != nil {
					t.Fatal(err)
				}
				kind := "multiple"
				if len(failed) == 1 {
					kind = failed[0]
				}
				evidence.Teardown.Failure = &TeardownFailure{Schema: plan.ReceiptContract.TeardownFailureSchema, Kind: kind, FailedChecks: failed}
				evidence.Teardown.Failure.EvidenceSHA256 = mustReceiptSHA256(t, *evidence.Teardown.Failure)
			}
			receipt, err := assembleExecutionReceipt(plan, binding, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if receipt.Decision.Outcome != "stopped" || receipt.PhaseResults[2].Outcome != "not_run" || len(receipt.RevisionResults) != 3 {
				t.Fatal("stopped prefix lost")
			}
			if test.failedCleanup && receipt.Decision.Reason != "teardown_failed" {
				t.Fatal("failed cleanup lost precedence")
			}
			if err := ValidateReceipt(receipt, plan, binding, ReturnedPackageBinding{}); err == nil {
				t.Fatal("unsigned receipt authenticated")
			}
		})
	}
}
