//go:build darwin

package t421

import (
	"context"
	"errors"
	"slices"
	"time"
)

// executionEpochSequenceResult retains every joined server/archive prefix
// needed by the later receipt composer. current always names the owner that a
// failed caller must stop; on success it names the already-joined final run.
type executionEpochSequenceResult struct {
	current *ExecutionEpochOneRun
	runs    [5]*ExecutionEpochOneRun
	stopped executionStoppedTeardown
	// Exact global phase-event prefix in frozen order. Missing suffix rows are
	// explicit not_run values; callers receive a detached snapshot.
	phaseEvents []PhaseMeasurement
	resources   executionWholeResourceEvidence

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
	if ctx == nil || flow == nil || volume == nil || ctx.Err() != nil && !flow.executionColdAuthorActive {
		return result, ErrExecutionEpochOne
	}
	if flow.executionWholeResources != nil {
		flow.executionWholeResources.volume = volume
		ctx = flow.executionWholeResources.ctx
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
			if run.epoch.Epoch >= 1 && run.epoch.Epoch <= 5 {
				result.runs[run.epoch.Epoch-1] = run
			}
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
			if run.epoch.Epoch >= 1 && run.epoch.Epoch <= 5 {
				result.runs[run.epoch.Epoch-1] = run
			}
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
			if run.epoch.Epoch >= 1 && run.epoch.Epoch <= 5 {
				result.runs[run.epoch.Epoch-1] = run
			}
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
			if run.epoch.Epoch >= 1 && run.epoch.Epoch <= 5 {
				result.runs[run.epoch.Epoch-1] = run
			}
		}
		if err != nil {
			return err
		}
		if run == nil {
			return checkpointRestartError("epoch-four result", nil)
		}
		result.returnCheckpoint, err = prior.Wait(context.Background())
		if err != nil {
			return checkpointRestartError("prior join", err)
		}
		if !joinedExecutionEpochResult(result.returnCheckpoint) {
			return checkpointRestartError("prior join result", nil)
		}
		if err := run.Health(ctx); err != nil {
			return checkpointRestartError("epoch-four health", err)
		}
		if err := run.RecoverCheckpoint(ctx); err != nil {
			return checkpointRestartError("checkpoint recovery", err)
		}
		return nil
	}); err != nil {
		return result, err
	}
	if err := run.Pressure(ctx, volume); err != nil {
		return result, err
	}

	if err := runExecutionPhase(flow, "archive_restore", func() error {
		var err error
		result.backup, err = run.BackupAndStop(ctx)
		if err != nil {
			return err
		}
		if !joinedExecutionEpochResult(result.backup) {
			return epochArchiveFailure(nil, "backup joined result", nil)
		}
		if _, err := flow.recordOptionalNamedExecutionEvent("archive_restore", "archive:created"); err != nil {
			return epochArchiveFailure(nil, "created event", err)
		}
		result.restore, err = run.RestoreBackup(ctx)
		if err != nil {
			return err
		}
		if !joinedExecutionEpochResult(result.restore) {
			return epochArchiveFailure(nil, "restore joined result", nil)
		}
		prior = run
		run, err = prior.StartRestored(ctx)
		if run != nil {
			result.current = run
			if run.epoch.Epoch >= 1 && run.epoch.Epoch <= 5 {
				result.runs[run.epoch.Epoch-1] = run
			}
		}
		if err != nil {
			return epochArchiveFailure(nil, "restored launch", err)
		}
		if run == nil {
			return epochArchiveFailure(nil, "restored launch result", nil)
		}
		if err := run.Health(ctx); err != nil {
			return epochArchiveFailure(nil, "restored health", err)
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
	if err := runExecutionPhase(flow, "teardown", func() (cleanupErr error) {
		defer func() {
			if cleanupErr != nil {
				cleanupErr = errors.Join(cleanupErr, result.observeStoppedTeardown(ctx, flow, volume))
			}
		}()
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

func (recorder *executionPhaseEventRecorder) begin(phase string) error {
	return recorder.beginAt(phase, time.Now())
}

func (recorder *executionPhaseEventRecorder) finish(phase, outcome string) error {
	return recorder.finishAt(phase, outcome, time.Now())
}

func cloneExecutionPhaseEvents(values []PhaseMeasurement) []PhaseMeasurement {
	return slices.Clone(values)
}

func (flow *ExecutionEpochOne) beginExecutionPhase(phase string) error {
	if flow == nil {
		return ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder, resources := flow.executionPhaseEvents, flow.executionWholeResources
	continuedCold := phase == "cold" && flow.executionColdAuthorActive
	if continuedCold {
		flow.executionColdAuthorActive = false
	}
	flow.mu.Unlock()
	if continuedCold {
		_ = flow.sampleExecutionDisk(false)
		return nil
	}
	if err := recorder.begin(phase); err != nil {
		return err
	}
	if err := resources.begin(phase); err != nil {
		return err
	}
	// Measurement failures cancel operational work, but never suppress cleanup.
	_ = flow.sampleExecutionDisk(false)
	return nil
}

func (flow *ExecutionEpochOne) finishExecutionPhase(phase, outcome string) error {
	if flow == nil {
		return ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder, resources := flow.executionPhaseEvents, flow.executionWholeResources
	flow.mu.Unlock()
	diskErr := errors.Join(flow.sampleExecutionDisk(false), resources.finish())
	if diskErr != nil && phase != "teardown" {
		outcome = "stopped"
	}
	if outcome == "stopped" {
		if _, err := flow.recordNamedExecutionEvent(phase, "failure:"+phase); err != nil {
			return err
		}
	}
	return errors.Join(recorder.finish(phase, outcome), diskErr)
}

func (flow *ExecutionEpochOne) executionPhaseEventEvidence() ([]PhaseMeasurement, error) {
	if flow == nil {
		return nil, ErrExecutionEpochOne
	}
	flow.mu.Lock()
	recorder := flow.executionPhaseEvents
	flow.mu.Unlock()
	return recorder.snapshot()
}

func (flow *ExecutionEpochOne) recordExecutionPhaseFailure(phase string, operation, closure error) {
	if flow == nil || operation == nil && closure == nil {
		return
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.executionPhaseFailures == nil {
		flow.executionPhaseFailures = make(map[string]executionPhaseFailure)
	}
	if _, exists := flow.executionPhaseFailures[phase]; exists {
		return
	}
	flow.executionPhaseFailures[phase] = executionPhaseFailure{phase: phase, operation: operation, closure: closure}
}

func (flow *ExecutionEpochOne) executionPhaseFailureSnapshot() []executionPhaseFailure {
	if flow == nil {
		return nil
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	values := make([]executionPhaseFailure, 0, len(flow.executionPhaseFailures))
	for _, phase := range frozenPhaseOrder() {
		if value, ok := flow.executionPhaseFailures[phase]; ok {
			values = append(values, value)
		}
	}
	return values
}

func runExecutionPhase(flow *ExecutionEpochOne, phase string, operation func() error) error {
	if operation == nil || flow.beginExecutionPhase(phase) != nil {
		return ErrExecutionEpochOne
	}
	err := operation()
	outcome := "passed"
	if err != nil {
		outcome = "stopped"
	}
	if phase == "teardown" {
		outcome = "clean"
		if err != nil {
			outcome = "failed"
		}
	}
	finishErr := flow.finishExecutionPhase(phase, outcome)
	flow.recordExecutionPhaseFailure(phase, err, finishErr)
	if finishErr != nil {
		err = errors.Join(err, finishErr)
	}
	if err != nil {
		return errors.Join(ErrExecutionEpochOne, err)
	}
	return nil
}
