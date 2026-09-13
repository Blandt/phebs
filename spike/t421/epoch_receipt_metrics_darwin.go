//go:build darwin

package t421

// composeExecutionTeardownReceiptMetrics is the only successful-teardown
// adapter. It binds the cumulative DA/SA pair and seven producer records to
// their actual joined cleanup owner, then adds the retained phase-fifteen byte
// maximum. It still does not supply phase timing, ordinals or cleanup-native
// process evidence.
func composeExecutionTeardownReceiptMetrics(
	plan Plan,
	teardown executionTeardownResult,
	inspection []ExecutionPhaseInspection,
	processes []ExecutionServerProcessObservation,
) (executionReceiptMetrics, error) {
	if !teardown.Joined || !teardown.CleanupClosed || !teardown.CustodyAbsent ||
		!teardown.Detached || !teardown.ImageRemoved || !teardown.RootRemoved ||
		teardown.AccountingError != nil || teardown.StoreError != nil || teardown.ByteObservations != 2 ||
		!teardown.Bytes.Completed || teardown.ByteUnavailable || teardown.ByteLimitExceeded ||
		teardown.Bytes.Maximum.LogicalBytes == 0 || teardown.Bytes.Maximum.AllocatedBytes == 0 ||
		teardown.Bytes.Maximum.LogicalBytes > plan.WorkEnvelope.MaximumDataLogicalBytes ||
		teardown.Bytes.Maximum.AllocatedBytes > plan.SafetyEnvelope.MaximumDataAllocatedBytes {
		return executionReceiptMetrics{}, errExecutionReceiptMetrics
	}
	out, err := composeExecutionReceiptMetrics(plan, teardown.Work, teardown.Accounting, teardown.Store, inspection, processes)
	if err != nil {
		return executionReceiptMetrics{}, err
	}
	index := len(out.Metrics) - 1
	out.Metrics[index].DataLogicalBytes = Bytes(teardown.Bytes.Maximum.LogicalBytes)
	out.Metrics[index].DataAllocatedBytes = Bytes(teardown.Bytes.Maximum.AllocatedBytes)
	out.Metrics[index].AllocationMeasurementAvailable = true
	out.Coverage[index].Workspace = true
	return out, nil
}
