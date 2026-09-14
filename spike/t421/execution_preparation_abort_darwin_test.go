//go:build darwin

package t421

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
)

func TestExecutionPreparationAbortBeforeAdmission(t *testing.T) {
	for _, mode := range []string{"empty", "canceled", "expired", "replaced", "partial volume", "borrowed volume", "admitted", "ordinal consumed", "author active", "runtime unjoined", "input close error"} {
		t.Run(mode, func(t *testing.T) {
			selection, _ := testExecutionSelection(t)
			root, err := createExecutionOperationalRoot(selection)
			if err != nil {
				t.Fatal(err)
			}
			prepared := &executionInnerPreparation{operational: root, outerDeadline: time.Now().Add(time.Minute)}
			t.Cleanup(func() { _ = root.file.Close(); _ = os.Remove(root.path); _ = os.Remove(root.path + ".prior") })
			ctx := t.Context()
			wantClean := false
			switch mode {
			case "empty":
				wantClean = true
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
				wantClean = true
			case "expired":
				prepared.outerDeadline = time.Now().Add(-time.Second)
			case "replaced":
				if os.Rename(root.path, root.path+".prior") != nil || os.Mkdir(root.path, 0700) != nil {
					t.Fatal("replace root")
				}
			case "partial volume":
				prepared.volume = &executionPressureVolume{root: productionRoot{path: filepath.Join(root.path, "unknown-image")}}
			case "borrowed volume":
				prepared.volume = &executionPressureVolume{borrowed: true, root: productionRoot{path: filepath.Join(root.path, "unknown-image")}}
			case "admitted":
				prepared.flow = &ExecutionEpochOne{executionEpochPlatform: executionEpochPlatform{executionFreezeBinding: &ExecutionFreezeBinding{}}}
			case "ordinal consumed":
				prepared.ordinals = newExecutionEventOrdinals()
				if _, _, err := prepared.ordinals.consumeFinalAdmission(); err != nil {
					t.Fatal(err)
				}
			case "author active":
				prepared.author = &ExecutionAuthorCustody{active: true}
			case "input close error":
				prepared.builds = &ExecutionGoBuildCustody{closed: true, err: ErrExecutionGoBuildCustody}
			case "runtime unjoined":
				prepared.flow = &ExecutionEpochOne{profileRuntime: &executionRuntimeObservation{RootStarted: true}}
			}
			err = prepared.abortBeforeAdmission(ctx)
			_, statErr := os.Lstat(root.path)
			if wantClean {
				if err != nil || !prepared.closed || !errors.Is(statErr, os.ErrNotExist) || prepared.abortBeforeAdmission(ctx) != nil {
					t.Fatalf("clean abort: %v %v", err, statErr)
				}
			} else {
				if err == nil || prepared.closed || statErr != nil {
					t.Fatalf("refusal lost custody: %v %v", err, statErr)
				}
			}
		})
	}
}

func TestExecutionPreparationOperationLockRemoval(t *testing.T) {
	for _, mode := range []string{"removed", "still mounted", "replaced lock", "unsettled", "unexpected sibling", "volume already closed", "closed volume competing holder"} {
		t.Run(mode, func(t *testing.T) {
			path, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0700); err != nil {
				t.Fatal(err)
			}
			root, err := openProductionRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.file.Close() }()
			lock, err := t4013.LockRunRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Close() }()
			lockPath := filepath.Join(path, ".t4013-operation.lock")
			info, err := os.Lstat(lockPath)
			if err != nil {
				t.Fatal(err)
			}
			volumeParent, err := openProductionRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = volumeParent.file.Close() }()
			volume := &executionPressureVolume{parent: volumeParent, root: productionRoot{path: filepath.Join(path, "removed-image")}, lock: lock, lockInfo: info, removed: true}
			switch mode {
			case "volume already closed":
				if err := volume.Close(); err != nil {
					t.Fatal(err)
				}
			case "closed volume competing holder":
				if err := volume.Close(); err != nil {
					t.Fatal(err)
				}
				contender, err := t4013.LockRunRoot(path)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = contender.Close() }()
			case "still mounted":
				volume.removed = false
			case "unsettled":
				volume.unsettled = true
			case "unexpected sibling":
				if err := os.WriteFile(filepath.Join(path, "retained"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "replaced lock":
				if os.Rename(lockPath, lockPath+".prior") != nil || os.WriteFile(lockPath, nil, 0600) != nil {
					t.Fatal("replace lock")
				}
			}
			err = volume.removeOwnedOperationLock(root)
			_, statErr := os.Lstat(lockPath)
			if mode == "removed" || mode == "volume already closed" {
				if err != nil || !errors.Is(statErr, os.ErrNotExist) || volume.lockInfo != nil {
					t.Fatalf("lock cleanup: %v %v", err, statErr)
				}
			} else if err == nil || statErr != nil {
				t.Fatalf("uncertain lock removed: %v %v", err, statErr)
			}
		})
	}
}

func TestExecutionPressureSessionDeadlineDoesNotRenew(t *testing.T) {
	for _, allowance := range []time.Duration{-time.Second, time.Second, time.Minute} {
		t.Run(allowance.String(), func(t *testing.T) {
			caller := time.Now().Add(allowance)
			ctx, cancel := context.WithDeadline(t.Context(), caller)
			defer cancel()
			before := time.Now()
			got := pressureSessionDeadline(ctx)
			if allowance < 5*time.Second {
				if !got.Equal(caller) {
					t.Fatal("caller deadline renewed")
				}
			} else if got.Before(before.Add(5*time.Second)) || got.After(time.Now().Add(5*time.Second)) {
				t.Fatal("session five-second cap changed")
			}
		})
	}
}

func TestExecutionPreparationSourceLeaseIsFreshAndExact(t *testing.T) {
	for _, mode := range []string{"unused", "released", "held", "replaced", "canceled"} {
		t.Run(mode, func(t *testing.T) {
			path, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0700); err != nil {
				t.Fatal(err)
			}
			root, err := openProductionRoot(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.file.Close() }()
			var expected os.FileInfo
			ctx := t.Context()
			if mode != "unused" {
				lease, err := acquireProductionSourceLease(path)
				if err != nil {
					t.Fatal(err)
				}
				expected, err = lease.Stat()
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lease.Close() }()
				if mode != "held" {
					if err := lease.Close(); err != nil {
						t.Fatal(err)
					}
				}
				if mode == "replaced" {
					name := filepath.Join(path, productionSourceLeaseName)
					if os.Rename(name, name+".prior") != nil || os.WriteFile(name, nil, 0600) != nil {
						t.Fatal("replace source lease")
					}
				}
			}
			if mode == "canceled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err = verifyPreparationSourceLease(ctx, root, expected)
			wantOK := mode == "unused" || mode == "released"
			if (err == nil) != wantOK {
				t.Fatalf("lease outcome=%v", err)
			}
		})
	}
}
