//go:build darwin

package t421

import "context"

// executionEpochSequenceResult retains every joined server/archive prefix
// needed by the later receipt composer. current always names the owner that a
// failed caller must stop; on success it names the already-joined final run.
type executionEpochSequenceResult struct {
	current *ExecutionEpochOneRun
	// Exact global phase-event prefix in frozen order. Missing suffix rows are
	// explicit not_run values; callers receive a detached snapshot.
	phaseEvents []PhaseMeasurement

	coldPhysical     ExecutionEpochOneResult
	logical          ExecutionEpochOneResult
	returnCheckpoint ExecutionEpochOneResult
	backup           ExecutionEpochOneResult
	restore          ExecutionEpochOneResult
	final            ExecutionEpochOneResult
	teardown         executionTeardownResult
}

// runExecutionEpochSequence is the complete post-AuthorA production path. It
// adds no retry, logging, signing, receipt construction or launcher behavior.
func runExecutionEpochSequence(ctx context.Context, flow *ExecutionEpochOne, volume *executionPressureVolume) (result *executionEpochSequenceResult, retErr error) {
	result = &executionEpochSequenceResult{}
	if ctx == nil || ctx.Err() != nil || flow == nil || volume == nil {
		return result, ErrExecutionEpochOne
	}
	defer func() {
		values, err := flow.executionPhaseEventEvidence()
		if err != nil {
			result.phaseEvents = nil
			retErr = ErrExecutionEpochOne
			return
		}
		result.phaseEvents = values
	}()
	var run *ExecutionEpochOneRun
	if err := runExecutionPhase(flow, "cold", func() error {
		var err error
		run, err = flow.StartPhysicalB(ctx)
		if run != nil {
			result.current = run
		}
		if err != nil || run == nil || run.Health(ctx) != nil || run.ColdToWarm(ctx) != nil {
			return ErrExecutionEpochOne
		}
		return nil
	}); err != nil {
		return result, err
	}
	if err := runExecutionPhase(flow, "warm_noop", func() error { return run.ObserveWarm(ctx) }); err != nil {
		return result, err
	}
	if err := runExecutionPhase(flow, "physical_delta_b", func() error { return run.PhysicalB(ctx) }); err != nil {
		return result, err
	}

	prior := run
	if err := runExecutionPhase(flow, "logical_delta_b", func() error {
		var err error
		run, err = prior.StartLogicalB(ctx)
		if run != nil {
			result.current = run
		}
		if err != nil || run == nil {
			return ErrExecutionEpochOne
		}
		result.coldPhysical, err = prior.Wait(context.Background())
		if err != nil || !joinedExecutionEpochResult(result.coldPhysical) || run.Health(ctx) != nil {
			return ErrExecutionEpochOne
		}
		return run.LogicalB(ctx)
	}); err != nil {
		return result, err
	}

	prior = run
	if err := runExecutionPhase(flow, "return_a", func() error {
		var err error
		run, err = prior.StartReturnACheckpoint(ctx)
		if run != nil {
			result.current = run
		}
		if err != nil || run == nil {
			return ErrExecutionEpochOne
		}
		result.logical, err = prior.Wait(context.Background())
		if err != nil || !joinedExecutionEpochResult(result.logical) || run.Health(ctx) != nil {
			return ErrExecutionEpochOne
		}
		return run.ReturnA(ctx)
	}); err != nil {
		return result, err
	}
	if err := runExecutionPhase(flow, "stale_lease", func() error { return run.StaleLease(ctx) }); err != nil {
		return result, err
	}

	prior = run
	if err := runExecutionPhase(flow, "process_restart", func() error {
		var err error
		run, err = prior.CheckpointRestartBackup(ctx)
		if run != nil {
			result.current = run
		}
		if err != nil || run == nil {
			return ErrExecutionEpochOne
		}
		result.returnCheckpoint, err = prior.Wait(context.Background())
		if err != nil || !joinedExecutionEpochResult(result.returnCheckpoint) || run.Health(ctx) != nil {
			return ErrExecutionEpochOne
		}
		return run.RecoverCheckpoint(ctx)
	}); err != nil {
		return result, err
	}
	if err := run.Pressure(ctx, volume); err != nil {
		return result, err
	}

	if err := runExecutionPhase(flow, "archive_restore", func() error {
		var err error
		result.backup, err = run.BackupAndStop(ctx)
		if err != nil || !joinedExecutionEpochResult(result.backup) {
			return ErrExecutionEpochOne
		}
		if _, err := flow.recordOptionalNamedExecutionEvent("archive_restore", "archive:created"); err != nil {
			return ErrExecutionEpochOne
		}
		result.restore, err = run.RestoreBackup(ctx)
		if err != nil || !joinedExecutionEpochResult(result.restore) {
			return ErrExecutionEpochOne
		}
		prior = run
		run, err = prior.StartRestored(ctx)
		if run != nil {
			result.current = run
		}
		if err != nil || run == nil || run.Health(ctx) != nil {
			return ErrExecutionEpochOne
		}
		if err := run.CompleteArchive(ctx); err != nil {
			return err
		}
		_, err = flow.recordOptionalNamedExecutionEvent("archive_restore", "archive:comparison")
		return err
	}); err != nil {
		return result, err
	}
	if err := runExecutionPhase(flow, "lifecycle_collection", func() error { return run.CollectRestored(ctx) }); err != nil {
		return result, err
	}
	if err := runExecutionPhase(flow, "product_queries", func() error { return run.QueryRestored(ctx) }); err != nil {
		return result, err
	}
	if err := runExecutionPhase(flow, "teardown", func() error {
		var err error
		result.teardown, err = volume.finishRestored(ctx, run)
		if err != nil || !result.teardown.Joined || !result.teardown.CleanupClosed || !result.teardown.CustodyAbsent {
			return ErrExecutionEpochOne
		}
		if _, err := flow.recordOptionalNamedExecutionEvent("teardown", "teardown"); err != nil {
			return ErrExecutionEpochOne
		}
		result.final, err = run.Wait(ctx)
		if err != nil || !joinedExecutionEpochResult(result.final) {
			return ErrExecutionEpochOne
		}
		return nil
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (result *executionEpochSequenceResult) phaseEventEvidence() []PhaseMeasurement {
	if result == nil {
		return nil
	}
	return cloneExecutionPhaseEvents(result.phaseEvents)
}

func joinedExecutionEpochResult(result ExecutionEpochOneResult) bool {
	return result.RootStarted && result.RootJoined && result.SessionEmpty
}

func (result *executionEpochSequenceResult) stop(ctx context.Context) (ExecutionEpochOneResult, error) {
	if result == nil || result.current == nil {
		return ExecutionEpochOneResult{}, ErrExecutionEpochOne
	}
	return result.current.Stop(ctx)
}
