//go:build darwin

package t421

import "testing"

func TestComposeExecutionTeardownReceiptMetricsBindsJoinedOwner(t *testing.T) {
	plan, work, dispatch, store, inspection, processes := receiptMetricTestEvidence(t)
	teardown := executionTeardownResult{Joined: true, CleanupClosed: true, CustodyAbsent: true,
		Detached: true, ImageRemoved: true, RootRemoved: true, ByteObservations: 2,
		Bytes:      custodyBytePhase{Completed: true, Maximum: custodyByteSample{LogicalBytes: 301, AllocatedBytes: 401}},
		Accounting: dispatch, Store: store, Work: work}
	got, err := composeExecutionTeardownReceiptMetrics(plan, teardown, inspection, processes)
	if err != nil || !got.Coverage[14].Workspace || got.JoinedFamilies[14] ||
		got.Metrics[14].DataLogicalBytes != 301 || got.Metrics[14].DataAllocatedBytes != 401 {
		t.Fatal("joined teardown byte prefix was not retained without overstating coverage", got, err)
	}
	for _, edit := range []func(*executionTeardownResult){
		func(value *executionTeardownResult) { value.Joined = false },
		func(value *executionTeardownResult) { value.AccountingError = errExecutionReceiptMetrics },
		func(value *executionTeardownResult) { value.ByteObservations = 1 },
		func(value *executionTeardownResult) {
			value.Bytes.Maximum.LogicalBytes = plan.WorkEnvelope.MaximumDataLogicalBytes + 1
		},
	} {
		changed := teardown
		edit(&changed)
		if _, err := composeExecutionTeardownReceiptMetrics(plan, changed, inspection, processes); err == nil {
			t.Fatal("unjoined teardown prefix was accepted")
		}
	}
}
