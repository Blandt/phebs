//go:build darwin

package t421

import "context"

// executionEpochSequenceResult retains every joined server/archive prefix
// needed by the later receipt composer. current always names the owner that a
// failed caller must stop; on success it names the already-joined final run.
type executionEpochSequenceResult struct {
	current *ExecutionEpochOneRun

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
func runExecutionEpochSequence(ctx context.Context, flow *ExecutionEpochOne, volume *executionPressureVolume) (*executionEpochSequenceResult, error) {
	result := &executionEpochSequenceResult{}
	if ctx == nil || ctx.Err() != nil || flow == nil || volume == nil {
		return result, ErrExecutionEpochOne
	}
	run, err := flow.StartPhysicalB(ctx)
	if run != nil {
		result.current = run
	}
	if err != nil || run == nil {
		return result, ErrExecutionEpochOne
	}
	if run.Health(ctx) != nil || run.ColdToWarm(ctx) != nil || run.ObserveWarm(ctx) != nil || run.PhysicalB(ctx) != nil {
		return result, ErrExecutionEpochOne
	}

	prior := run
	run, err = prior.StartLogicalB(ctx)
	if run != nil {
		result.current = run
	}
	if err != nil || run == nil {
		return result, ErrExecutionEpochOne
	}
	result.coldPhysical, err = prior.Wait(context.Background())
	if err != nil || !joinedExecutionEpochResult(result.coldPhysical) || run.Health(ctx) != nil || run.LogicalB(ctx) != nil {
		return result, ErrExecutionEpochOne
	}

	prior = run
	run, err = prior.StartReturnACheckpoint(ctx)
	if run != nil {
		result.current = run
	}
	if err != nil || run == nil {
		return result, ErrExecutionEpochOne
	}
	result.logical, err = prior.Wait(context.Background())
	if err != nil || !joinedExecutionEpochResult(result.logical) || run.Health(ctx) != nil || run.ReturnA(ctx) != nil || run.StaleLease(ctx) != nil {
		return result, ErrExecutionEpochOne
	}

	prior = run
	run, err = prior.CheckpointRestartBackup(ctx)
	if run != nil {
		result.current = run
	}
	if err != nil || run == nil {
		return result, ErrExecutionEpochOne
	}
	result.returnCheckpoint, err = prior.Wait(context.Background())
	if err != nil || !joinedExecutionEpochResult(result.returnCheckpoint) || run.Health(ctx) != nil || run.RecoverCheckpoint(ctx) != nil || run.Pressure(ctx, volume) != nil {
		return result, ErrExecutionEpochOne
	}

	result.backup, err = run.BackupAndStop(ctx)
	if err != nil || !joinedExecutionEpochResult(result.backup) {
		return result, ErrExecutionEpochOne
	}
	result.restore, err = run.RestoreBackup(ctx)
	if err != nil || !joinedExecutionEpochResult(result.restore) {
		return result, ErrExecutionEpochOne
	}
	prior = run
	run, err = prior.StartRestored(ctx)
	if run != nil {
		result.current = run
	}
	if err != nil || run == nil {
		return result, ErrExecutionEpochOne
	}
	if run.Health(ctx) != nil || run.CompleteArchive(ctx) != nil || run.CollectRestored(ctx) != nil || run.QueryRestored(ctx) != nil {
		return result, ErrExecutionEpochOne
	}
	result.teardown, err = volume.finishRestored(ctx, run)
	if err != nil || !result.teardown.Joined || !result.teardown.CleanupClosed || !result.teardown.CustodyAbsent {
		return result, ErrExecutionEpochOne
	}
	result.final, err = run.Wait(ctx)
	if err != nil || !joinedExecutionEpochResult(result.final) {
		return result, ErrExecutionEpochOne
	}
	return result, nil
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
