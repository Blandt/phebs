//go:build darwin

package t421

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
)

// abortBeforeAdmission owns only a refused preparation, never an admitted
// phase or receipt. The existing one-minute native detach timeout and outer
// lifetime bound cleanup even if the preparation caller was canceled.
func (prepared *executionInnerPreparation) abortBeforeAdmission(ctx context.Context) error {
	if prepared == nil {
		return nil
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed {
		return nil
	}
	if prepared.abortAttempted || ctx == nil || !prepared.beforeAdmissionLocked() {
		return ErrExecutionLauncher
	}
	prepared.abortAttempted = true
	deadline := time.Now().Add(time.Minute)
	if prepared.outerDeadline.Before(deadline) {
		deadline = prepared.outerDeadline
	}
	cleanup, cancel := context.WithDeadline(context.WithoutCancel(ctx), deadline)
	defer cancel()
	if cleanup.Err() != nil || pressureRootsUnchanged(prepared.operational) != nil {
		return ErrExecutionLauncher
	}
	volume := prepared.volume
	if volume != nil {
		volume.mu.Lock()
		// A failed volume constructor may have created no image custody at all.
		// Partially created or ambiguously attached images remain retained.
		noImage := volume.root.path == "" && volume.image == nil && volume.device == "" && len(volume.sessions) == 0
		valid := noImage || volume.ready && !volume.closed && !volume.unsettled && volume.teardownRun == nil &&
			(volume.flow == nil || volume.flow == prepared.flow) && volume.parent.path == prepared.operational.path && volume.check() == nil
		volume.mu.Unlock()
		if !valid {
			return errPressureVolume
		}
	}
	if err := prepared.closeInputOwnersLocked(); err != nil {
		return err
	}
	if volume != nil {
		volume.mu.Lock()
		if volume.root.path != "" {
			if err := prepared.releasePreparationWorkspaceLocked(cleanup); err != nil {
				volume.mu.Unlock()
				return err
			}
		}
		volume.mu.Unlock()
		if cleanup.Err() != nil {
			return errPressureVolume
		}
		if err := volume.removeOwnedOperationLock(prepared.operational); err != nil {
			return err
		}
		if err := volume.Close(); err != nil {
			return err
		}
	}
	if err := closeExecutionOperationalRoot(prepared.operational); err != nil {
		return err
	}
	prepared.closed = true
	return nil
}

// The preparation mutex excludes authorizeAndAuthorA while these existing
// owners are checked. No ordinal, started flag, measurement, or retry is made.
func (prepared *executionInnerPreparation) beforeAdmissionLocked() bool {
	if ordinals := prepared.ordinals; ordinals != nil {
		ordinals.mu.Lock()
		defer ordinals.mu.Unlock()
		if ordinals.last != 0 || ordinals.failed {
			return false
		}
	}
	if flow := prepared.flow; flow != nil {
		flow.mu.Lock()
		defer flow.mu.Unlock()
		if flow.executionFreezeBinding != nil || flow.executionEventOrdinals != nil || flow.executionPhaseEvents != nil ||
			flow.used || flow.authored || !flow.authorStarted.IsZero() || flow.retained != nil ||
			flow.serverSessions != ([5]int{}) || flow.archiveSessions != ([2]int{}) || !flow.profileRuntime.releasable() {
			return false
		}
	}
	if author := prepared.author; author != nil {
		author.mu.Lock()
		defer author.mu.Unlock()
		if author.active || author.borrowedBy != nil || author.next != 0 || author.sessions != ([3]int{}) {
			return false
		}
	}
	if epochs := prepared.epochs; epochs != nil {
		epochs.mu.Lock()
		defer epochs.mu.Unlock()
		if epochs.active || epochs.released != 0 {
			return false
		}
	}
	return true
}

// Called after input owners close, with the preparation and volume locks held.
// The existing source lease proves no borrower; non-forced native detach then
// discards the owned image, without recursively deleting mounted contents.
func (prepared *executionInnerPreparation) releasePreparationWorkspaceLocked(ctx context.Context) error {
	volume := prepared.volume
	if ctx.Err() != nil || volume.check() != nil || volume.teardownRun != nil {
		return errPressureVolume
	}
	var expectedLease os.FileInfo
	if prepared.author != nil {
		expectedLease = prepared.author.leaseInfo
	}
	if err := verifyPreparationSourceLease(ctx, volume.workspace, expectedLease); err != nil {
		return err
	}
	if ballast := volume.ballast; ballast != nil {
		// Preparation creates only a zero-length inode. A used, failed, or
		// replaced ballast requires the operational/retained-custody path.
		if ballast.volume != volume || ballast.failed || ballast.next != 0 || ballast.removed || ballast.file == nil || ballast.info == nil {
			return errPressureVolume
		}
		held, err := ballast.file.Stat()
		path := filepath.Join(volume.workspace.path, "pressure-ballast")
		current, pathErr := os.Lstat(path)
		if err != nil || pathErr != nil || !os.SameFile(ballast.info, held) || !os.SameFile(held, current) ||
			!inputCustodyOwned(current) || !current.Mode().IsRegular() || current.Size() != 0 || ctx.Err() != nil {
			return errPressureVolume
		}
		if ballast.file.Close() != nil || os.Remove(path) != nil || volume.workspace.file.Sync() != nil {
			return errPressureVolume
		}
		ballast.file = nil
		ballast.removed = true
	}
	volume.borrowed = false
	return volume.remove(ctx, false)
}

// Remove only the captured lock inode in the fresh operational parent after
// image removal. Close still owns releasing the kernel lock. No signer claim,
// key, evidence, or external namespace entry is deleted.
func (v *executionPressureVolume) removeOwnedOperationLock(root productionRoot) (retErr error) {
	if v == nil {
		return nil
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.lockInfo == nil {
		if v.lock != nil {
			return errPressureVolume
		}
		return nil
	}
	if !v.removed && (v.root.path != "" || v.image != nil || v.device != "" || len(v.sessions) != 0) ||
		v.borrowed || v.unsettled || v.lock == nil || root.path != v.parent.path || root.info == nil || v.parent.info == nil ||
		!os.SameFile(root.info, v.parent.info) || pressureRootsUnchanged(root) != nil || !executionOperationLockOnly(root) {
		return errPressureVolume
	}
	// Operational teardown may already have closed the volume's descriptors
	// and released its lock. Reacquire that exact inode, never a competing one,
	// through the still-held preparation parent before removing its pathname.
	lock := v.lock
	if v.closed {
		var err error
		lock, err = t4013.LockRunRoot(root.path)
		if err != nil {
			return errPressureVolume
		}
		defer func() { retErr = errors.Join(retErr, lock.Close()) }()
	}
	identity, ok := lock.(interface{ Stat() (os.FileInfo, error) })
	if !ok {
		return errPressureVolume
	}
	held, heldErr := identity.Stat()
	if heldErr != nil || !os.SameFile(v.lockInfo, held) {
		return errPressureVolume
	}
	path := filepath.Join(root.path, ".t4013-operation.lock")
	current, err := os.Lstat(path)
	if err != nil || !os.SameFile(v.lockInfo, current) || !inputCustodyOwned(current) || !current.Mode().IsRegular() || current.Mode().Perm() != 0o600 {
		return errPressureVolume
	}
	if err := errors.Join(os.Remove(path), root.file.Sync()); err != nil {
		return errPressureVolume
	}
	v.lockInfo = nil
	return nil
}

// At most the expected name and one overflow sentinel are read. The captured
// parent must contain only the lock before its pathname may be removed.
func executionOperationLockOnly(root productionRoot) bool {
	file, err := os.Open(root.path)
	if err != nil {
		return false
	}
	held, statErr := file.Stat()
	names, readErr := file.Readdirnames(1)
	overflow, endErr := file.Readdirnames(1)
	closeErr := file.Close()
	return statErr == nil && os.SameFile(root.info, held) && readErr == nil && len(names) == 1 &&
		names[0] == ".t4013-operation.lock" && len(overflow) == 0 && errors.Is(endErr, io.EOF) && closeErr == nil
}

// Reuse the native nonblocking lease boundary after preparation holders close.
// No historical acquisition or successful owner Close replaces this check.
func verifyPreparationSourceLease(ctx context.Context, root productionRoot, expected os.FileInfo) error {
	if ctx == nil || ctx.Err() != nil || pressureRootsUnchanged(root) != nil {
		return errPressureVolume
	}
	lease, err := acquireProductionSourceLease(root.path)
	if err != nil {
		return errPressureVolume
	}
	info, statErr := lease.Stat()
	closeErr := lease.Close()
	if statErr != nil || closeErr != nil || ctx.Err() != nil || expected != nil && !inputCustodySame(expected, info) {
		return errPressureVolume
	}
	return nil
}
