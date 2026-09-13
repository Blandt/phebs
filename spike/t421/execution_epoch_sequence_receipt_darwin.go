//go:build darwin

package t421

import "slices"

type executionEpochSequenceReceiptEvidence struct {
	metrics   executionReceiptMetrics
	authority executionReceiptAuthorityInventory
}

func composeExecutionEpochSequenceReceiptEvidence(
	plan Plan,
	sequence *executionEpochSequenceResult,
) (executionEpochSequenceReceiptEvidence, error) {
	var out executionEpochSequenceReceiptEvidence
	if sequence == nil || plan.Schema != PlanV3Schema || validatePlan(plan, &plan.Revisions) != nil ||
		!slices.Equal(plan.PhaseOrder, frozenPhaseOrder()) || !joinedExecutionEpochResult(sequence.restore) ||
		!sequence.teardown.Joined || !sequence.teardown.CleanupClosed || !sequence.teardown.CustodyAbsent {
		return out, ErrExecutionEpochOne
	}
	servers := [...]ExecutionEpochOneResult{
		sequence.coldPhysical,
		sequence.logical,
		sequence.returnCheckpoint,
		sequence.backup,
		sequence.final,
	}
	var inspections []ExecutionPhaseInspection
	processes := make([]ExecutionServerProcessObservation, 0, len(servers))
	for _, server := range servers {
		if !joinedExecutionEpochResult(server) || len(server.ServerProcesses.Phases) == 0 {
			return executionEpochSequenceReceiptEvidence{}, ErrExecutionEpochOne
		}
		inspections = append(inspections, cloneInspectionEvidence(server.Inspection)...)
		processes = append(processes, cloneServerProcessObservation(server.ServerProcesses))
	}
	if len(inspections) != 14 || len(sequence.final.Authorities) != len(plan.PhaseOrder)-2 ||
		!slices.EqualFunc(sequence.final.Authorities, plan.PhaseOrder[1:len(plan.PhaseOrder)-1],
			func(value AuthorityPhaseResult, phase string) bool { return value.Phase == phase }) {
		return executionEpochSequenceReceiptEvidence{}, ErrExecutionEpochOne
	}
	var err error
	out.metrics, err = composeExecutionTeardownReceiptMetrics(plan, sequence.teardown, inspections, processes)
	if err != nil {
		return executionEpochSequenceReceiptEvidence{}, err
	}
	out.authority, err = composeExecutionReceiptAuthorityInventory(plan, sequence.final.Authorities)
	if err != nil {
		return executionEpochSequenceReceiptEvidence{}, err
	}
	return out, nil
}
