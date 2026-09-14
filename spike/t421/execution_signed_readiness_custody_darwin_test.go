//go:build darwin

package t421

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
	"golang.org/x/sys/unix"
)

// This actual third-party borrower is outside the volume's recorded launcher
// sessions. Its cwd must be honored by nonforced native detach. It is only a
// custody fixture, not Phebs, an author, a signer or a fabricated admitted role.
func TestExecutionSignedReadinessBusyDirectoryHelper(t *testing.T) {
	if os.Getenv("PHEBS_T422_READINESS_BUSY_DIRECTORY_HELPER") != "1" {
		return
	}
	if _, err := os.Stdout.Write([]byte{'R'}); err != nil {
		t.Fatal(err)
	}
	var release [1]byte
	if _, err := io.ReadFull(os.Stdin, release[:]); err != nil || release[0] != 'E' {
		t.Fatal("private borrower release unavailable", err)
	}
}

// Independently selected tiny native failure, with no corpus or signing. The
// healthy signed-launcher test is a separate gate. This measures one actual
// busy detach and exact retained custody rather than setting v.unsettled or
// substituting a helper for hdiutil. No second detach or forced cleanup runs.
func TestExecutionSignedReadinessBusyDetachOptionalNative(t *testing.T) {
	if os.Getenv("PHEBS_T422_BUSY_DETACH_REHEARSAL") != "1" {
		t.Skip("requires explicit serial native busy-detach custody rehearsal")
	}
	requireExternalToolFrozenHost(t)
	parent, err := os.MkdirTemp("/private/tmp", "t422-busy-detach-readiness-")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("owned native failure custody (retained; no retry): %s", parent)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	volume, err := prepareExecutionPressureVolume(ctx, parent)
	if volume != nil {
		defer func() {
			if err := volume.Close(); err != nil {
				t.Error("native volume descriptor closure refused; retain all custody", err)
			}
		}()
	}
	if err != nil {
		t.Fatal("native volume preparation failed", err)
	}
	imagePath := filepath.Join(volume.root.path, "pressure.sparseimage")
	imageBefore, err := os.Lstat(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	var mountedBefore unix.Statfs_t
	if err := unix.Statfs(volume.workspace.path, &mountedBefore); err != nil {
		t.Fatal(err)
	}
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestExecutionSignedReadinessBusyDirectoryHelper$")
	command.Env = []string{"PHEBS_T422_READINESS_BUSY_DIRECTORY_HELPER=1", "GORACE=atexit_sleep_ms=0"}
	command.Dir, command.Stderr, command.WaitDelay = volume.workspace.path, io.Discard, time.Second
	prepareProductionSession(command)
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = input.Close() }()
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = output.Close() }()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			_ = t4013.KillPrivateProcessSession(command.Process.Pid)
			_ = command.Wait()
		}
		if err := t4013.WaitPrivateProcessSession(command.Process.Pid, time.Now().Add(5*time.Second)); err != nil {
			t.Error("external borrower session remains", err)
		}
	}()
	var ready [1]byte
	if _, err := io.ReadFull(output, ready[:]); err != nil || ready[0] != 'R' {
		t.Fatal("native borrower did not hold mounted cwd", err)
	}
	beforeAttempts := len(volume.sessions)
	detachStarted := time.Now()
	if err := volume.removeEmpty(ctx); err == nil {
		t.Fatal("native detach did not refuse the active mounted-directory borrower")
	}
	// The production hdiutil operation has its own unchanged one-minute
	// command bound. Timeout or caller cancellation is not busy evidence.
	if ctx.Err() != nil || time.Since(detachStarted) >= time.Minute {
		t.Fatal("detach timeout/cancellation; retain custody without a busy-refusal claim", ctx.Err())
	}
	// A refusal before the actual detach Start is not this test's evidence.
	if len(volume.sessions) != beforeAttempts+1 || volume.removed || volume.ready {
		t.Fatal("busy fixture did not reach exactly one native detach attempt")
	}
	imageAfter, err := os.Lstat(imagePath)
	var mountedAfter unix.Statfs_t
	if err != nil || !os.SameFile(imageBefore, imageAfter) || unix.Statfs(command.Dir, &mountedAfter) != nil || mountedBefore.Fsid != mountedAfter.Fsid {
		t.Fatal("failed native detach did not preserve exact image and mounted filesystem", err)
	}
	if _, err := input.Write([]byte{'E'}); err != nil {
		t.Fatal(err)
	}
	waitErr := command.Wait()
	joined = true
	if waitErr != nil {
		t.Fatal("native borrower failed its clean join", waitErr)
	}
	if _, err := os.Lstat(volume.root.path); errors.Is(err, os.ErrNotExist) {
		t.Fatal("failed custody unexpectedly removed")
	}
	t.Logf("one actual nonforced detach refused, borrower joined, exact mounted custody retained: root=%s image=%s device=%s; no signed-receipt or clean-teardown claim", volume.root.path, imagePath, volume.device)
}

// Actual mounted source-lease exclusion at the real pre-admission abort
// boundary. This is intentionally tiny and unsigned: no author/flow booleans,
// phase evidence or successful full-profile cleanup are supplied by a fixture.
func TestExecutionSignedReadinessHeldSourceLeaseOptionalNative(t *testing.T) {
	if os.Getenv("PHEBS_T422_HELD_SOURCE_LEASE_REHEARSAL") != "1" {
		t.Skip("requires explicit serial native held-source-lease custody rehearsal")
	}
	requireExternalToolFrozenHost(t)
	selection, _ := testExecutionSelection(t)
	operational, err := createExecutionOperationalRoot(selection)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = operational.file.Close() }()
	t.Logf("owned native lease-failure custody (retained; no retry): %s", operational.path)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	volume, err := prepareExecutionPressureVolume(ctx, operational.path)
	if volume != nil {
		defer func() {
			if err := volume.Close(); err != nil {
				t.Error("native lease fixture descriptors did not close", err)
			}
		}()
	}
	if err != nil {
		t.Fatal("native volume preparation failed", err)
	}
	actualPath := filepath.Join(volume.workspace.path, "retained-lease-fixture")
	actual := []byte("actual mounted bytes retained under a live source lease\n")
	if err := os.WriteFile(actualPath, actual, 0o600); err != nil {
		t.Fatal(err)
	}
	lease, err := acquireProductionSourceLease(volume.workspace.path)
	if err != nil {
		t.Fatal("actual source lease unavailable", err)
	}
	defer func() { _ = lease.Close() }()
	imagePath := filepath.Join(volume.root.path, "pressure.sparseimage")
	imageBefore, err := os.Lstat(imagePath)
	if err != nil {
		t.Fatal(err)
	}
	var mountedBefore unix.Statfs_t
	if err := unix.Statfs(volume.workspace.path, &mountedBefore); err != nil {
		t.Fatal(err)
	}
	prepared := &executionInnerPreparation{operational: operational, volume: volume, outerDeadline: time.Now().Add(time.Minute)}
	beforeAttempts := len(volume.sessions)
	abortErr := prepared.abortBeforeAdmission(ctx)
	if abortErr == nil || ctx.Err() != nil || !time.Now().Before(prepared.outerDeadline) || !prepared.abortAttempted || prepared.closed || volume.removed || len(volume.sessions) != beforeAttempts {
		t.Fatal("held lease did not refuse actual abort before native detach", abortErr)
	}
	imageAfter, err := os.Lstat(imagePath)
	var mountedAfter unix.Statfs_t
	if err != nil || !os.SameFile(imageBefore, imageAfter) || unix.Statfs(volume.workspace.path, &mountedAfter) != nil || mountedBefore.Fsid != mountedAfter.Fsid {
		t.Fatal("held-source refusal changed image or mounted filesystem", err)
	}
	retained, err := os.ReadFile(actualPath)
	if err != nil || !bytes.Equal(retained, actual) {
		t.Fatal("held-source refusal changed actual mounted fixture bytes", err)
	}
	if err := lease.Close(); err != nil {
		t.Fatal("owned source lease did not close", err)
	}
	t.Logf("actual source lease blocked pre-admission abort before detach; holder closed, exact image/FSID/content retained: root=%s image=%s device=%s; no retry, signed-receipt or clean-teardown claim", volume.root.path, imagePath, volume.device)
}
