//go:build darwin

package t421

import "testing"

func TestComposeExecutionEpochSequenceReceiptEvidence(t *testing.T) {
	plan, work, dispatch, store, inspections, processes := receiptMetricTestEvidence(t)
	joined := func(rows []ExecutionPhaseInspection, process ExecutionServerProcessObservation) ExecutionEpochOneResult {
		return ExecutionEpochOneResult{RootStarted: true, RootJoined: true, SessionEmpty: true, Inspection: rows, ServerProcesses: process}
	}
	authorities := make([]AuthorityPhaseResult, len(plan.PhaseOrder)-2)
	for index, phase := range plan.PhaseOrder[1 : len(plan.PhaseOrder)-1] {
		authorities[index] = AuthorityPhaseResult{Phase: phase, Outcome: "passed"}
	}
	sequence := &executionEpochSequenceResult{
		coldPhysical:     joined(inspections[0:3], processes[0]),
		logical:          joined(inspections[3:4], processes[1]),
		returnCheckpoint: joined(inspections[4:7], processes[2]),
		backup:           joined(inspections[7:11], processes[3]),
		restore:          ExecutionEpochOneResult{RootStarted: true, RootJoined: true, SessionEmpty: true},
		final:            joined(inspections[11:14], processes[4]),
		teardown: executionTeardownResult{
			Joined: true, CleanupClosed: true, CustodyAbsent: true,
			Detached: true, ImageRemoved: true, RootRemoved: true, ByteObservations: 2,
			Bytes:      custodyBytePhase{Completed: true, Maximum: custodyByteSample{LogicalBytes: 301, AllocatedBytes: 401}},
			Accounting: dispatch, Store: store, Work: work,
		},
	}
	sequence.final.Authorities = authorities
	got, err := composeExecutionEpochSequenceReceiptEvidence(plan, sequence)
	if err != nil || len(got.authority.Results) != len(authorities) || got.metrics.Metrics[14].DataAllocatedBytes != 401 {
		t.Fatal("complete joined sequence did not compose receipt evidence", err)
	}
	if _, err := composeExecutionEpochSequenceReceiptEvidence(Plan{}, sequence); err == nil {
		t.Fatal("invalid plan was accepted")
	}

	sequence.final.Authorities = sequence.final.Authorities[:len(sequence.final.Authorities)-1]
	if _, err := composeExecutionEpochSequenceReceiptEvidence(plan, sequence); err == nil {
		t.Fatal("incomplete authority prefix was accepted")
	}
}
