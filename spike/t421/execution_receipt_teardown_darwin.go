//go:build darwin

package t421

import "slices"

// Project the actual completed teardown corridor, or the actual failed Close
// and retained-root observations. Counts describe custody roots, not files.
func composeExecutionTeardownEvidence(plan Plan, freeze ExecutionFreeze, sequence *executionEpochSequenceResult, measurements []PhaseMeasurement, events map[string]uint64, unavailable []string, ownedServer bool) (ReceiptTeardown, error) {
	if sequence == nil || sequence.teardown.Started.IsZero() {
		return ReceiptTeardown{}, errExecutionReceiptAssembly
	}
	t := sequence.teardown
	value := ReceiptTeardown{Attempted: true, Completed: t.CleanupClosed, Outcome: "clean", BackingVolumeIdentity: freeze.Host.BackingVolumeIdentity,
		PressureVolumeDetached: t.Detached, PressureImageRemoved: t.ImageRemoved, RetainedSourceFreeOnly: t.CustodyAbsent,
		StoreClosed:            t.CleanupClosed && t.StoreError == nil && t.Store.Opened == t.Store.TerminalEOF,
		MeasurementUnavailable: slices.Clone(unavailable), MeasurementErrors: uint64(len(unavailable)),
		Scoped: &ScopedTeardownEvidence{Schema: ScopedTeardownSchema,
			OperationalFenceEventOrdinal: events["teardown:operational-fence"], OwnedHandlesJoinedEventOrdinal: events["teardown:owned-handles-joined"],
			LeaseReleasedEventOrdinal: events["teardown:lease-released"], BeforeDetach: t.BeforeDetach,
			DetachEventOrdinal: events["teardown:detach"], DetachNonForced: t.Detached,
			ExactImageRemovalEventOrdinal: events["teardown:image-removed"], ExactRootRemovalEventOrdinal: events["teardown:root-removed"],
			CustodyLockHeld: t.CustodyAbsent, CleanupJoinedEventOrdinal: events["teardown:cleanup-joined"], AfterCleanup: t.AfterCleanup,
			CleanupClosedEventOrdinal: events["teardown:cleanup-closed"],
		},
	}
	if !t.CustodyAbsent {
		stopped := sequence.stopped
		value.Outcome = "failed"
		value.Scoped.CustodyLockHeld = stopped.CustodyLockHeld
		value.Scoped.OwnedHandleWaitErrors = stopped.StopErrors
		if stopped.CloseError != nil {
			value.Scoped.CleanupWaitErrors = 1
		}
		if stopped.WorkspacePresent {
			value.DerivedCustodyPaths = 1
		}
		if stopped.SourcePresent {
			value.ScratchSourcePaths = 1
		}
		if stopped.ImagePresent {
			value.PressureImagePaths = 1
			value.BackingDerivedCustodyPaths = 1
		}
		if stopped.PathError != nil {
			value.DerivedRemovalErrors = 1
			value.ScratchRemovalErrors = 1
			value.ImageRemovalErrors = 1
		}
		value.PressureBallastBytes = stopped.BallastBytes
		if stopped.BallastError != nil {
			value.BallastRemovalErrors = 1
		}
		if value.Scoped.BeforeDetach.EventOrdinal == 0 {
			value.Scoped.BeforeDetach = t.InitialJoined
		}
	}
	failed, err := receiptTeardownFailedChecks(value, measurements, plan, freeze, ownedServer)
	if err != nil {
		return ReceiptTeardown{}, err
	}
	if len(failed) > 0 {
		value.Outcome = "failed"
		failure := TeardownFailure{Schema: plan.ReceiptContract.TeardownFailureSchema, Kind: "multiple", FailedChecks: failed}
		if len(failed) == 1 {
			failure.Kind = failed[0]
		}
		failure.EvidenceSHA256, err = receiptSHA256(failure)
		if err != nil {
			return ReceiptTeardown{}, err
		}
		value.Failure = &failure
	}
	if _, err := validateReceiptTeardown(value, measurements, plan, freeze, ownedServer); err != nil {
		return ReceiptTeardown{}, err
	}
	return value, nil
}
