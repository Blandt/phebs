//go:build darwin

package t421

import (
	"context"
	"errors"
	"os"
	"time"
)

// Ordinary stop preserves real cleanup refusals. It cannot borrow the success
// route's health assertion or release a failed populated volume as a rehearsal.
type executionStoppedTeardown struct {
	Servers                        [5]ExecutionEpochOneResult
	StopErrors                     uint64
	CloseError                     error
	ImagePresent, WorkspacePresent bool
	SourcePresent                  bool
	BallastBytes                   uint64
	BallastError                   error
	PathError                      error
	CustodyLockHeld                bool
}

func (result *executionEpochSequenceResult) stopAndObserve(ctx context.Context, flow *ExecutionEpochOne, volume *executionPressureVolume) (retErr error) {
	if result == nil || ctx == nil || flow == nil || volume == nil {
		return ErrExecutionEpochOne
	}
	if err := flow.beginExecutionPhase("teardown"); err != nil {
		return errors.Join(err, result.observeStoppedTeardown(ctx, flow, volume))
	}
	defer func() {
		closeErr := flow.finishExecutionPhase("teardown", "failed")
		flow.recordExecutionPhaseFailure("teardown", retErr, closeErr)
		retErr = errors.Join(retErr, closeErr)
		result.phaseEvents, _ = flow.executionPhaseEventEvidence()
	}()
	return result.observeStoppedTeardown(ctx, flow, volume)
}

// Also runs inside an already-active failed success teardown, before its phase
// endpoint is frozen. It never begins a second teardown or claims healthy reuse.
func (result *executionEpochSequenceResult) observeStoppedTeardown(ctx context.Context, flow *ExecutionEpochOne, volume *executionPressureVolume) (retErr error) {
	deadline := time.Now().Add(time.Duration(flow.plan.PhaseDeadlines[14].DeadlineMS) * time.Millisecond)
	if outer, ok := ctx.Deadline(); ok && outer.Before(deadline) {
		deadline = outer
	}
	if !result.teardown.Deadline.IsZero() && result.teardown.Deadline.Before(deadline) {
		deadline = result.teardown.Deadline
	}
	cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	if flow.executionWholeResources != nil {
		flow.executionWholeResources.volume = volume
	}
	if result.teardown.Started.IsZero() {
		result.teardown.Started, result.teardown.Deadline = time.Now(), deadline
	}
	defer func() {
		if flow.controller != nil && flow.store != nil {
			retErr = errors.Join(retErr, flow.teardownAccounting(&result.teardown))
		}
		result.teardown.Work = flow.joinedWorkSnapshot()
		volume.mu.Lock()
		volume.teardownEvidence(&result.teardown)
		volume.mu.Unlock()
	}()
	// Stop's native/protocol joins remain authoritative even if its operational
	// error is non-nil. Retain every started epoch, not only the last pointer.
	for index, run := range result.runs {
		if run == nil {
			continue
		}
		observed, err := run.Stop(cleanup)
		result.stopped.Servers[index] = observed
		switch index {
		case 0:
			result.coldPhysical = observed
		case 1:
			result.logical = observed
		case 2:
			result.returnCheckpoint = observed
		case 3:
			result.restore = observed
		case 4:
			result.final = observed
		}
		if err != nil && (!observed.RootJoined || !observed.SessionEmpty) {
			result.stopped.StopErrors++
		}
		if !observed.RootJoined || !observed.SessionEmpty {
			retErr = errors.Join(retErr, ErrExecutionEpochOne)
		}
	}
	volume.mu.Lock()
	census, censusErr := volume.censusTeardownSessions(cleanup, true)
	if censusErr == nil {
		ordinal, eventErr := flow.recordOptionalNamedExecutionEvent("teardown", "stopped:joined-census")
		census.EventOrdinal = ordinal
		censusErr = eventErr
	}
	volume.teardownInitial = census
	result.teardown.Joined = censusErr == nil
	// A fresh joined scope plus terminal EOF for every actually opened receiver
	// permits this existing bounded read-only walk, even with unopened suffixes.
	if censusErr == nil && volume.bytes != nil && volume.ready && volume.check() == nil {
		confirm := func() bool {
			state, err := flow.store.Snapshot()
			return err == nil && state.Opened == state.TerminalEOF && volume.check() == nil && cleanup.Err() == nil
		}
		if confirm() {
			_, err := volume.bytes.sample(cleanup, 15, confirm)
			if err == nil {
				volume.teardownByteSamples++
			}
			retErr = errors.Join(retErr, err)
		}
	}
	result.stopped.CustodyLockHeld = volume.lock != nil
	if volume.ballast != nil && !volume.ballast.removed {
		if volume.ballast.file == nil {
			result.stopped.BallastError = errPressureVolume
		} else if info, err := volume.ballast.file.Stat(); err != nil || info.Size() < 0 {
			result.stopped.BallastError = errPressureVolume
		} else {
			result.stopped.BallastBytes = uint64(info.Size())
		}
	}
	for index, path := range []string{volume.workspace.path, volume.root.path + "/pressure.sparseimage", flow.epochs.author.Directory()} {
		_, err := os.Lstat(path)
		if err == nil {
			switch index {
			case 0:
				result.stopped.WorkspacePresent = true
			case 1:
				result.stopped.ImagePresent = true
			case 2:
				result.stopped.SourcePresent = true
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			result.stopped.PathError = errors.Join(result.stopped.PathError, err)
		}
	}
	volume.mu.Unlock()
	retErr = errors.Join(retErr, censusErr)
	// Close itself enforces borrowed/unsettled custody. Its actual refusal is
	// retained; no mounted deletion, fake healthy result or forced detach occurs.
	result.stopped.CloseError = volume.Close()
	volume.mu.Lock()
	after, afterErr := volume.censusTeardownSessions(cleanup, false)
	if afterErr == nil {
		after.EventOrdinal, afterErr = flow.recordOptionalNamedExecutionEvent("teardown", "stopped:after-close")
	}
	volume.teardownAfter = after
	volume.mu.Unlock()
	return errors.Join(retErr, result.stopped.CloseError, afterErr, errPressureVolume)
}
