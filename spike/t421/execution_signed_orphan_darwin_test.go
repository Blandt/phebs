//go:build darwin

package t421

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
	"golang.org/x/sys/unix"
)

type executionSignedOrphanObservation struct {
	Inner            t4013.NativeProcessRecord `json:"inner"`
	Child            t4013.NativeProcessRecord `json:"child"`
	ChildSession     int                       `json:"child_session"`
	Executable       string                    `json:"executable"`
	ExecutableSHA256 string                    `json:"executable_sha256"`
	Stopped          bool                      `json:"stopped"`
	InnerKilled      bool                      `json:"inner_killed"`
	OuterFailed      bool                      `json:"outer_failed"`
	NoPackage        bool                      `json:"no_package"`
	CustodyRetained  bool                      `json:"custody_retained"`
	OrphanSurvived   bool                      `json:"orphan_survived"`
	HarnessDisposed  bool                      `json:"harness_disposed"`
}

type executionSignedOrphanCapture struct {
	observation executionSignedOrphanObservation
	err         error
}

// Called only after the actual signed handoff. The observer intercepts an
// actual direct-inner protected Git revalidation child; it never launches a
// substitute workload. The private record contains native identities and is
// deliberately not a source-free receipt or launcher teardown claim.
func executionSignedReadinessOrphan(t *testing.T, ctx context.Context, root string, selection executionSelectionV1,
	frame executionAuthorizationHandoffFrame, inner t4013.NativeProcessRecord, operationalInfo os.FileInfo, outer *exec.Cmd,
	waited <-chan error, packages <-chan executionReturnedOutput, captured <-chan struct{}) (joined bool) {
	t.Helper()
	if outer == nil || outer.Process == nil || inner.PID <= 0 || inner.StartIdentity == "" || inner.ParentPID != outer.Process.Pid || frame.err != nil {
		t.Error("orphan rehearsal requires the actual live outer/inner handoff")
		return false
	}
	operational := filepath.Dir(frame.value.SocketPath)
	custody, workspace, err := executionSignedOrphanCustody(operational)
	if err != nil || operationalInfo == nil || len(custody) != 5 || !os.SameFile(operationalInfo, custody[0].info) {
		t.Error("orphan rehearsal custody inventory unavailable", err)
		return false
	}
	gitDigest, err := t4013.DigestHostExecutable(ctx, selection.GitBinary)
	if err != nil {
		t.Error("orphan rehearsal selected Git identity unavailable", err)
		return false
	}
	deadline := time.Now().Add(30 * time.Second)
	if admission := time.Unix(0, frame.value.FinalAdmissionDeadlineUnixNano); admission.Before(deadline) {
		deadline = admission
	}
	observationCtx, stopObservation := context.WithDeadline(ctx, deadline)
	defer stopObservation()
	observed := make(chan executionSignedOrphanCapture, 1)
	go func() { observed <- executionCaptureSignedOrphan(observationCtx, inner, workspace, gitDigest) }()
	clientCtx, stopClient := context.WithDeadline(ctx, time.Unix(0, frame.value.FinalAdmissionDeadlineUnixNano))
	client := exec.CommandContext(clientCtx, frame.value.ClientArgv[0], frame.value.ClientArgv[1:]...)
	client.Env, client.Stdout, client.Stderr = []string{}, io.Discard, io.Discard
	client.WaitDelay = 5 * time.Second
	clientErr := client.Run()
	stopClient()
	if clientErr != nil {
		stopObservation()
	}
	result := <-observed // Join the sole bounded observer even on client refusal.
	observation := result.observation
	defer func() {
		// Separate test disposition: the launcher swept only its inner SID. Do not
		// count this independently recorded session cleanup as launcher success.
		if observation.Stopped {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if executionSignedOrphanCurrent(cleanup, observation.Child, observation.ChildSession, false) != nil {
				t.Error("recorded stopped orphan identity unavailable; retain exact custody")
			} else if err := t4013.KillPrivateProcessSession(observation.ChildSession); err != nil {
				t.Error("harness orphan session disposition failed", err)
			} else if err := t4013.WaitPrivateProcessSession(observation.ChildSession, pressureSessionDeadline(cleanup)); err != nil {
				t.Error("harness orphan session remains", err)
			} else {
				observation.HarnessDisposed = true
			}
		}
		raw, err := json.MarshalIndent(observation, "", "  ")
		if err != nil || os.WriteFile(filepath.Join(root, "orphan-private-observation.json"), append(raw, '\n'), 0600) != nil {
			t.Error("private orphan observation could not be retained")
		}
	}()
	if clientErr != nil || result.err != nil || !observation.InnerKilled {
		t.Error("orphan injection unestablished; no substitute child or cleanup pass", clientErr, result.err)
		return false
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, 20*time.Second)
	defer cancelWait()
	var waitErr error
	select {
	case waitErr = <-waited:
		joined = true
	case <-waitCtx.Done():
		t.Error("outer did not join after actual inner hard death")
		return false
	}
	finished := time.Now()
	waitDeadline, bounded := waitCtx.Deadline()
	exit, ordinaryExit := waitErr.(*exec.ExitError)
	observation.OuterFailed = ordinaryExit && exit.ProcessState != nil && exit.Exited() &&
		exit.ExitCode() == 1 && bounded && finished.Before(waitDeadline) && ctx.Err() == nil && waitCtx.Err() == nil
	select {
	case <-captured:
	case <-waitCtx.Done():
		t.Error("outer output failed to close after hard death")
		return joined
	}
	select {
	case returned := <-packages:
		observation.NoPackage = returned.err != nil && len(returned.raw) == 0
	default:
		t.Error("missing terminal output capture")
		return joined
	}
	observation.CustodyRetained = executionSignedOrphanCustodyUnchanged(custody) == nil
	observation.OrphanSurvived = executionSignedOrphanCurrent(waitCtx, observation.Child, observation.ChildSession, false) == nil
	if observation.OrphanSurvived {
		rows, err := t4013.ObserveProcessTreeRecords(waitCtx, observation.Child.PID)
		observation.OrphanSurvived = err == nil && len(rows) > 0 && rows[0].StartIdentity == observation.Child.StartIdentity && rows[0].ParentPID != inner.PID
	}
	if !observation.OuterFailed || !observation.NoPackage || !observation.CustodyRetained || !observation.OrphanSurvived {
		t.Error("actual orphan failure did not establish nonzero/no-package/retained-custody/live-separate-session predicates")
		return joined
	}
	t.Log("real inner hard death refused; exact operational image retained; separate session survived and requires harness disposition, not launcher cleanup")
	return joined
}

func executionCaptureSignedOrphan(ctx context.Context, inner t4013.NativeProcessRecord, workspace, gitDigest string) executionSignedOrphanCapture {
	out := executionSignedOrphanCapture{observation: executionSignedOrphanObservation{Inner: inner}}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for attempts := 0; attempts < 3000; attempts++ {
		if err := ctx.Err(); err != nil {
			out.err = err
			return out
		}
		rows, err := t4013.ObserveProcessTreeRecords(ctx, inner.PID)
		if err != nil || len(rows) == 0 || rows[0].StartIdentity != inner.StartIdentity || rows[0].ParentPID != inner.ParentPID {
			out.err = ErrExecutionLauncher
			return out
		}
		for _, row := range rows[1:] {
			if row.ParentPID != inner.PID || row.ObservedName != "git" {
				continue
			}
			session, err := unix.Getsid(row.PID)
			if err != nil || session != row.PID || session == inner.PID {
				continue
			}
			path, err := t4013.ObserveProcessExecutablePath(ctx, row.PID)
			if err != nil {
				continue
			} // A short native Git may already have joined.
			if filepath.Base(path) != "git" || filepath.Dir(filepath.Dir(path)) != workspace || !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "t422-inputs-") {
				out.err = ErrExecutionLauncher
				return out
			}
			if executionSignedOrphanCurrent(ctx, row, session, true) != nil {
				continue
			}
			if err := unix.Kill(row.PID, syscall.SIGSTOP); err != nil {
				continue
			}
			// Retain the actual successful stop immediately so every subsequent
			// failure still attempts disposition of only this observed native SID.
			out.observation.Child, out.observation.ChildSession, out.observation.Stopped = row, session, true
			out.observation.Executable = path
			if executionSignedOrphanCurrent(ctx, row, session, true) != nil {
				out.err = ErrExecutionLauncher
				return out
			}
			confirmed, err := t4013.ObserveProcessExecutablePath(ctx, row.PID)
			digest, digestErr := t4013.DigestHostExecutable(ctx, path)
			out.observation.ExecutableSHA256 = digest
			if err != nil || digestErr != nil || confirmed != path || digest != gitDigest {
				out.err = ErrExecutionLauncher
				return out
			}
			if executionSignedOrphanCurrent(ctx, inner, inner.PID, true) != nil {
				out.err = ErrExecutionLauncher
				return out
			}
			if err := unix.Kill(inner.PID, syscall.SIGKILL); err != nil {
				out.err = err
				return out
			}
			out.observation.InnerKilled = true
			return out
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			out.err = ctx.Err()
			return out
		}
	}
	out.err = errors.New("no actual revalidation child observed within finite sample bound")
	return out
}

func executionSignedOrphanCurrent(ctx context.Context, expected t4013.NativeProcessRecord, session int, requireParent bool) error {
	rows, err := t4013.ObserveProcessTreeRecords(ctx, expected.PID)
	if err != nil || len(rows) == 0 || rows[0].PID != expected.PID || rows[0].StartIdentity != expected.StartIdentity || rows[0].ObservedName != expected.ObservedName || requireParent && rows[0].ParentPID != expected.ParentPID {
		return ErrExecutionLauncher
	}
	actual, err := unix.Getsid(expected.PID)
	if err != nil || actual != session {
		return ErrExecutionLauncher
	}
	return nil
}

type executionSignedOrphanPath struct {
	path string
	info os.FileInfo
}

// The live handoff identifies the exact parent. Inspect only its fixed direct
// shape, then hold metadata for the selected pressure root, image and workspace.
func executionSignedOrphanCustody(operational string) ([]executionSignedOrphanPath, string, error) {
	if !executionGitAbsolutePath(operational) {
		return nil, "", ErrExecutionLauncher
	}
	file, err := os.Open(operational)
	if err != nil {
		return nil, "", err
	}
	names, readErr := file.Readdirnames(4)
	overflow, endErr := file.Readdirnames(1)
	closeErr := file.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) || len(names) > 3 || len(overflow) != 0 || !errors.Is(endErr, io.EOF) || closeErr != nil {
		return nil, "", ErrExecutionLauncher
	}
	pressure := ""
	for _, name := range names {
		if strings.HasPrefix(name, "t422-pressure-") && pressure == "" {
			pressure = filepath.Join(operational, name)
			continue
		}
		if name != ".t4013-operation.lock" && name != executionAuthorizationSocketName {
			return nil, "", ErrExecutionLauncher
		}
	}
	if pressure == "" {
		return nil, "", ErrExecutionLauncher
	}
	workspace := filepath.Join(pressure, "mount", "workspace")
	paths := []string{operational, pressure, filepath.Join(pressure, "pressure.sparseimage"), filepath.Join(pressure, "mount"), workspace}
	out := make([]executionSignedOrphanPath, 0, len(paths))
	for index, path := range paths {
		info, err := os.Lstat(path)
		canonical, canonicalErr := filepath.EvalSymlinks(path)
		if err != nil || canonicalErr != nil || canonical != path || !inputCustodyOwned(info) || info.Mode()&os.ModeSymlink != 0 || index == 2 && !pressureImageOwned(info) || index != 2 && !info.IsDir() {
			return nil, "", ErrExecutionLauncher
		}
		out = append(out, executionSignedOrphanPath{path, info})
	}
	return out, workspace, nil
}

func executionSignedOrphanCustodyUnchanged(paths []executionSignedOrphanPath) error {
	if len(paths) != 5 {
		return ErrExecutionLauncher
	}
	for _, prior := range paths {
		current, err := os.Lstat(prior.path)
		canonical, canonicalErr := filepath.EvalSymlinks(prior.path)
		if err != nil || canonicalErr != nil || canonical != prior.path || !os.SameFile(prior.info, current) || prior.info.Mode() != current.Mode() {
			return fmt.Errorf("recorded orphan custody changed: %w", ErrExecutionLauncher)
		}
	}
	return nil
}
