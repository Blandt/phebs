//go:build darwin

package t421

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

// These fixtures use the existing native authority constructors with modeled
// measurements and signatures. They test deterministic composition, never a
// live ceremony or authenticated acceptance; run with the package native gate.
func TestAssembleExecutionReceiptModeledEvidence(t *testing.T) {
	plan := clonePlan(t, correctedTestPlan(t))
	if err := applyProcessAccountingCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	binding := frozenReceiptTestBinding(t, plan)
	baseline := completeTestReceipt(t, plan, binding)
	for _, test := range []struct {
		name string
		edit func(*Receipt)
	}{
		{"passed", func(*Receipt) {}},
		{"stopped prefix", func(value *Receipt) {
			stopTestReceipt(t, value, plan, "pressure_80", ReceiptFailure{
				Phase: "pressure_80", Class: "topology", Code: "materialized_cartesian_owner_pairs_nonzero",
				Observation: failureObservation(t, plan, "counter_crossing", "materialized_cartesian_owner_pairs", 0, 1),
			})
			value.Measurements[slices.Index(plan.PhaseOrder, "pressure_80")].Metrics.MaterializedOwnerPairs = 1
		}},
		{"failed cleanup", func(value *Receipt) {
			value.Teardown.Completed = false
			value.Teardown.Outcome = "failed"
			value.Teardown.Failure = teardownFailure(t, plan, "teardown_incomplete")
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := cloneTestReceipt(t, baseline)
			test.edit(&fixture)
			evidence := modeledReceiptEvidence(t, fixture, plan)
			value, err := assembleExecutionReceipt(plan, binding, evidence)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateReceipt(value, plan, binding, returnedPackageTestBinding(t, value, plan, binding)); err != nil {
				t.Fatal(err)
			}
			if err := ValidateReceipt(value, plan, binding, ReturnedPackageBinding{}); err == nil {
				t.Fatal("unsigned assembly authenticated itself")
			}
			if !reflect.DeepEqual(value.Measurements, fixture.Measurements) || !reflect.DeepEqual(value.Teardown, fixture.Teardown) {
				t.Fatal("assembly changed observed measurements or cleanup")
			}
			evidence.Measurements[0].Metrics.WallMS++
			evidence.States[0].Runtime.OwnedStart.NativeIdentitySHA256 = testDigest("mutated")
			if value.Measurements[0].Metrics.WallMS == evidence.Measurements[0].Metrics.WallMS ||
				value.StateResults[0].Runtime.OwnedStart.NativeIdentitySHA256 == testDigest("mutated") {
				t.Fatal("assembly aliases its caller")
			}
		})
	}
}

func TestAssembleExecutionReceiptRefusesMissingEvidence(t *testing.T) {
	plan := clonePlan(t, correctedTestPlan(t))
	if err := applyProcessAccountingCorrection(&plan); err != nil {
		t.Fatal(err)
	}
	binding := frozenReceiptTestBinding(t, plan)
	baseline := completeTestReceipt(t, plan, binding)
	for _, test := range []struct {
		name string
		edit func(*executionReceiptEvidence)
	}{
		{"measurement inventory", func(v *executionReceiptEvidence) { v.Measurements = v.Measurements[:14] }},
		{"preflight disk", func(v *executionReceiptEvidence) { v.Measurements[0].Metrics.AvailableDiskBytes = 0 }},
		{"teardown observation", func(v *executionReceiptEvidence) { v.Teardown = ReceiptTeardown{} }},
		{"native runtime", func(v *executionReceiptEvidence) { v.States[0].Runtime = nil }},
		{"caller publication", func(v *executionReceiptEvidence) { v.Relationships.Caller = nil }},
		{"archive", func(v *executionReceiptEvidence) {
			v.Transitions[slices.Index(transitionPhases, "archive_restore")].Archive = nil
		}},
		{"stopped without typed failure", func(v *executionReceiptEvidence) { v.Phases[1].Outcome = "stopped" }},
		{"unrun without stop", func(v *executionReceiptEvidence) { v.Phases[1].Outcome = "not_run" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			evidence := modeledReceiptEvidence(t, cloneTestReceipt(t, baseline), plan)
			test.edit(&evidence)
			value, err := assembleExecutionReceipt(plan, binding, evidence)
			if !errors.Is(err, errExecutionReceiptAssembly) || !reflect.DeepEqual(value, Receipt{}) {
				t.Fatal("incomplete evidence accepted", err)
			}
		})
	}
}

func modeledReceiptEvidence(t *testing.T, value Receipt, plan Plan) executionReceiptEvidence {
	t.Helper()
	outcomes, _, err := validateReceiptPhases(value.PhaseResults, plan)
	if err != nil {
		t.Fatal(err)
	}
	authorities, err := resolveAuthorityResults(value.Authority.ExtractionRootSnapshots, value.Authority.Snapshots,
		value.Authority.Results, plan.PhaseOrder[1:len(plan.PhaseOrder)-1], outcomes)
	if err != nil {
		t.Fatal(err)
	}
	return executionReceiptEvidence{Phases: value.PhaseResults, Measurements: value.Measurements, Authorities: authorities,
		States: value.StateResults, Transitions: value.TransitionResults, Queries: value.QueryResults,
		Relationships: value.RelationshipResults, Revisions: value.RevisionResults, Teardown: value.Teardown}
}
