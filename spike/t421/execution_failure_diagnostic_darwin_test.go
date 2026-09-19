//go:build darwin

package t421

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestExecutionFailureDiagnosticRootAndLeaf(t *testing.T) {
	for _, name := range []string{"empty owned root", "missing owner", "removed root", "replaced root", "symlink root", "existing leaf", "symlink leaf", "invalid name", "over bound", "invalid bound"} {
		t.Run(name, func(t *testing.T) {
			root := executionAuthorizationTestRoot(t)
			leaf, raw, limit := executionFailureSummaryName, []byte("original failure"), executionFailureSummaryLimit
			originalPath := root.path
			t.Cleanup(func() { _ = os.RemoveAll(originalPath + ".retained") })
			switch name {
			case "missing owner":
				root = productionRoot{}
			case "removed root":
				if err := os.Remove(root.path); err != nil {
					t.Fatal(err)
				}
			case "replaced root", "symlink root":
				if err := os.Rename(root.path, root.path+".retained"); err != nil {
					t.Fatal(err)
				}
				if name == "replaced root" {
					if err := os.Mkdir(root.path, 0o700); err != nil {
						t.Fatal(err)
					}
				} else if err := os.Symlink(root.path+".retained", root.path); err != nil {
					t.Fatal(err)
				}
			case "existing leaf":
				if err := os.WriteFile(filepath.Join(root.path, leaf), []byte("do not replace"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink leaf":
				if err := os.WriteFile(filepath.Join(root.path, "target"), []byte("do not replace"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("target", filepath.Join(root.path, leaf)); err != nil {
					t.Fatal(err)
				}
			case "invalid name":
				leaf = "../escape"
			case "over bound":
				limit = len(raw) - 1
			case "invalid bound":
				limit = executionFailureOutputLimit + 1
			}
			err := writeExecutionFailureLeaf(root, leaf, raw, limit)
			if name == "empty owned root" {
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(root.path, leaf))
				info, statErr := os.Stat(filepath.Join(root.path, leaf))
				if err != nil || statErr != nil || !bytes.Equal(got, raw) || info.Mode().Perm() != 0o600 {
					t.Fatal("private bytes or mode changed", err, statErr)
				}
			} else if !errors.Is(err, errExecutionFailureDiagnostic) {
				t.Fatal("unsafe custody accepted", err)
			}
			if name == "existing leaf" || name == "symlink leaf" {
				got, err := os.ReadFile(filepath.Join(root.path, leaf))
				if err != nil || string(got) != "do not replace" {
					t.Fatal("existing bytes overwritten", err)
				}
			}
			if name == "removed root" {
				if _, err := os.Lstat(originalPath); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("removed root recreated", err)
				}
			}
			if name == "replaced root" || name == "symlink root" {
				for _, path := range []string{originalPath, originalPath + ".retained"} {
					if _, err := os.Lstat(filepath.Join(path, executionFailureSummaryName)); !errors.Is(err, os.ErrNotExist) {
						t.Fatal("replacement received a write", err)
					}
				}
			}
		})
	}
}

func TestExecutionFailureDiagnosticJoinedPrefix(t *testing.T) {
	for _, mode := range []string{"joined", "not done", "root unjoined", "backup unjoined"} {
		t.Run(mode, func(t *testing.T) {
			root := executionAuthorizationTestRoot(t)
			run := &ExecutionEpochOneRun{done: make(chan struct{}), result: ExecutionEpochOneResult{RootJoined: true}, output: &checkoutCommandOutput{}}
			run.output.buffer.WriteString("already captured native output\n")
			run.stopDiagnostic.Before.Existing = errors.New("original server failure")
			run.processObservation = &epochProcessObservation{gauge: &ProcessObservationGauge{privateRefusalDetail: "original server process refusal"}}
			run.inspection = &executionEpochInspection{failureStatus: 503, failureOrdinal: 7,
				readFailure: epochReadFailure{Stage: "response", Ordinal: 7, Cause: errors.New("original inspection failure")},
				failureBody: bytes.Repeat([]byte("x"), executionFailureBodyLimit+1)}
			if mode != "not done" {
				close(run.done)
			}
			if mode == "root unjoined" {
				run.result.RootJoined = false
			}
			if mode == "backup unjoined" {
				run.backupStarted = true
			}
			recorder := &executionPhaseEventRecorder{active: -1, slots: []executionPhaseEventSlot{{value: PhaseMeasurement{Phase: "cold", StartEventOrdinal: 3, FinishEventOrdinal: 5, Metrics: ReceiptMetrics{WallMS: 2}}}}}
			original := errors.New("original execution failure")
			receiptErr := errors.New("receipt refused observed prefix")
			err := retainExecutionFailureDiagnostic(root, "receipt_composition", recorder, nil, original, receiptErr, "original whole process refusal", run, nil)
			if err != nil {
				t.Fatal(err)
			}
			summary, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
			if err != nil || !bytes.Contains(summary, []byte(original.Error())) || !bytes.Contains(summary, []byte(receiptErr.Error())) ||
				!bytes.Contains(summary, []byte("phase=cold start=3 finish=5 wall_ms=2")) {
				t.Fatal("actual failure/prefix lost", err)
			}
			for _, name := range []string{executionFailureOutputName, executionFailureBodyName} {
				raw, err := os.ReadFile(filepath.Join(root.path, name))
				if mode != "joined" && mode != "backup unjoined" {
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatal("unjoined bytes retained", name, err)
					}
				} else if err != nil {
					t.Fatal(err)
				} else if name == executionFailureBodyName {
					if len(raw) != executionFailureBodyLimit || !bytes.Contains(summary, []byte("response_observed_bytes=65537 response_truncated=true")) {
						t.Fatal("response truncation unreported")
					}
				} else if !bytes.Equal(raw, run.output.buffer.Bytes()) {
					t.Fatal("joined output changed")
				}
			}
			if mode == "joined" && (!bytes.Contains(summary, []byte("original server failure")) || !bytes.Contains(summary, []byte("original inspection failure")) || !bytes.Contains(summary, []byte("original server process refusal")) || !bytes.Contains(summary, []byte("original whole process refusal"))) {
				t.Fatal("existing diagnostics lost")
			}
			if len(run.inspection.failureBody) != executionFailureBodyLimit+1 {
				t.Fatal("retention mutated owner bytes")
			}
		})
	}
}

func TestExecutionFailureDiagnosticBoundsAndOriginalFailure(t *testing.T) {
	root := executionAuthorizationTestRoot(t)
	original := errors.New(strings.Repeat("original failure ", executionFailureSummaryLimit))
	err := retainExecutionFailureDiagnostic(root, "package_construction", nil, nil, original, nil, "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
	if err != nil || len(raw) > executionFailureSummaryLimit || !bytes.Contains(raw, []byte("execution_failure_truncated=true")) || bytes.Contains(raw, []byte(strings.Repeat("original failure ", 129))) {
		t.Fatal("summary bound/truncation lost", err)
	}
	// An existing diagnostic cannot be retried or overwrite the original error.
	failedRetention := retainExecutionFailureDiagnostic(root, "package_construction", nil, nil, original, nil, "", nil, nil)
	combined := errors.Join(original, failedRetention)
	if !errors.Is(combined, original) || !errors.Is(combined, errExecutionFailureDiagnostic) {
		t.Fatal("retention replaced original failure")
	}
	fresh := executionAuthorizationTestRoot(t)
	if retainExecutionFailureDiagnostic(fresh, "package_emit", nil, nil, nil, nil, "", nil, nil) == nil {
		t.Fatal("success wrote diagnostics")
	}
	entries, err := os.ReadDir(fresh.path)
	if err != nil || len(entries) != 0 {
		t.Fatal("success root changed", err)
	}
}

func TestExecutionFailureDiagnosticArchiveStreams(t *testing.T) {
	for _, mode := range []string{"same owner", "restored owner", "server unjoined", "backup unjoined", "restore unjoined", "backup not started", "restore not started", "success"} {
		t.Run(mode, func(t *testing.T) {
			root := executionAuthorizationTestRoot(t)
			archive := &ExecutionEpochOneRun{done: make(chan struct{}), result: ExecutionEpochOneResult{RootJoined: true},
				backupStarted: true, backupJoined: true, backupSessionEmpty: true, backupComplete: true, backupManifestSHA256: "present",
				restoreStarted: true, restoreJoined: true, restoreSessionEmpty: true, restoreComplete: true, restoreManifestSHA256: "present",
				output: &checkoutCommandOutput{}, backupOutput: &epochBackupOutput{backup: &checkoutCommandOutput{}, restore: &checkoutCommandOutput{}}}
			close(archive.done) // The server stop predates the restore operation.
			archive.backupWork.Complete, archive.backupWork.ScanComplete = true, true
			archive.backupWork.WorkspaceBytes.Bound, archive.backupWork.WorkspaceBytes.Complete = true, true
			archive.backupWork.WorkspaceBytes.Phases[11].Attempts, archive.backupWork.WorkspaceBytes.Phases[11].Completed = 15, 15
			archive.output.buffer.WriteString("server output\n")
			archive.backupOutput.backup.buffer.WriteString("backup output\n")
			archive.backupOutput.restore.buffer.WriteString("restore output\n")
			current := archive
			switch mode {
			case "restored owner":
				current = &ExecutionEpochOneRun{done: make(chan struct{}), result: ExecutionEpochOneResult{RootJoined: true}, output: &checkoutCommandOutput{}}
				close(current.done)
				current.output.buffer.WriteString("restored server output\n")
			case "server unjoined":
				archive.result.RootJoined = false
			case "backup unjoined":
				archive.backupJoined = false
			case "restore unjoined":
				archive.restoreJoined = false
			case "backup not started":
				archive.backupStarted = false
			case "restore not started":
				archive.restoreStarted = false
			}
			rows := []struct {
				name, want string
				joined     bool
				output     *checkoutCommandOutput
			}{
				{executionFailureOutputName, "server output\n", current.result.RootJoined, current.output},
				{executionFailureBackupName, "backup output\n", archive.backupStarted && archive.backupJoined, archive.backupOutput.backup},
				{executionFailureRestoreName, "restore output\n", archive.restoreStarted && archive.restoreJoined, archive.backupOutput.restore},
			}
			if mode == "restored owner" {
				rows[0].want = "restored server output\n"
			}
			for _, row := range rows {
				if !row.joined {
					// Race runs prove retention does not even inspect live bytes.
					stop, stopped := make(chan struct{}), make(chan struct{})
					go func() {
						defer close(stopped)
						for {
							select {
							case <-stop:
								return
							default:
								row.output.buffer.Reset()
								row.output.buffer.WriteString("live unjoined output\n")
							}
						}
					}()
					t.Cleanup(func() { close(stop); <-stopped })
				}
			}
			executionErr := ErrExecutionEpochOne
			if mode == "success" {
				executionErr = nil
			}
			err := retainExecutionFailureDiagnostic(root, "receipt_composition", nil, &ExecutionEpochOne{}, executionErr, nil, "", current, archive)
			if mode == "success" {
				entries, readErr := os.ReadDir(root.path)
				if !errors.Is(err, errExecutionFailureDiagnostic) || readErr != nil || len(entries) != 0 {
					t.Fatal("successful operation wrote diagnostics", err, readErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				raw, err := os.ReadFile(filepath.Join(root.path, row.name))
				if row.joined && (err != nil || string(raw) != row.want) || !row.joined && !errors.Is(err, os.ErrNotExist) {
					t.Fatal("wrong stream owner or join gate", row.name, err)
				}
			}
			summary, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
			if err != nil || !bytes.Contains(summary, []byte("manifest_digest_present=true work_complete=true scan_complete=true workspace_bound=true workspace_complete=true workspace_unavailable=false workspace_limit_exceeded=false workspace_attempts=15 workspace_completed=15")) {
				t.Fatal("archive scalar prefix lost", err)
			}
		})
	}
}

func TestExecutionFailureDiagnosticSharedOutputCap(t *testing.T) {
	var summary executionFailureSummary
	remaining := 12
	for i, name := range []string{"server", "backup", "restore"} {
		output := &checkoutCommandOutput{}
		output.buffer.WriteString("eight!!!")
		raw := summary.output(name, output, &remaining)
		want := []int{8, 4, 0}[i]
		if len(raw) != want || want > 0 && &raw[0] != &output.buffer.Bytes()[0] || output.buffer.Len() != 8 {
			t.Fatal("output copied, mutated, or exceeded remaining shared cap")
		}
	}
	if remaining != 0 || !strings.Contains(string(summary.bytes()), "backup_output_observed_bytes=8 backup_output_retained_bytes=4 backup_output_truncated=true") ||
		!strings.Contains(string(summary.bytes()), "restore_output_observed_bytes=8 restore_output_retained_bytes=0 restore_output_truncated=true") {
		t.Fatal("shared-cap truncation not reported")
	}
}

func TestExecutionFailureDiagnosticFieldPrecision(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", executionFailureFieldLimit), strings.Repeat("\x00", executionFailureFieldLimit+1)} {
		var summary executionFailureSummary
		summary.failure("failure", errors.New(value))
		encoded, marker, ok := strings.Cut(strings.TrimPrefix(string(summary.bytes()), "failure="), " failure_truncated=")
		decoded, err := strconv.Unquote(encoded)
		if !ok || err != nil || decoded != value[:min(len(value), executionFailureFieldLimit)] ||
			strings.TrimSpace(marker) != strconv.FormatBool(len(value) > executionFailureFieldLimit) {
			t.Fatal("error precision or truncation marker differs", err)
		}
	}
	var summary executionFailureSummary
	_, _ = summary.Write(bytes.Repeat([]byte("x"), executionFailureSummaryLimit+1))
	if raw := summary.bytes(); len(raw) > executionFailureSummaryLimit || !bytes.HasSuffix(raw, []byte(executionFailureSummaryTruncated)) {
		t.Fatal("aggregate summary bound or marker differs")
	}
}

func TestExecutionFailureDiagnosticFailedActiveRecorder(t *testing.T) {
	root := executionAuthorizationTestRoot(t)
	recorder := &executionPhaseEventRecorder{
		active: 1, next: 1, failed: true, stopped: true,
		phases: []string{"cold", "warm_noop"},
		slots: []executionPhaseEventSlot{
			{value: PhaseMeasurement{Phase: "cold", StartEventOrdinal: 3, FinishEventOrdinal: 5, Metrics: ReceiptMetrics{WallMS: 2}}},
			{value: PhaseMeasurement{Phase: "warm_noop", StartEventOrdinal: 6}},
		},
	}
	if phases, err := recorder.snapshot(); err == nil || phases != nil {
		t.Fatal("public snapshot must refuse the failed active recorder")
	}
	if err := retainExecutionFailureDiagnostic(root, "receipt_composition", recorder, nil, ErrExecutionEpochOne, nil, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
	if err != nil || !bytes.Contains(raw, []byte("active=1 next=1 failed=true stopped=true observed_slots=2 slots_truncated=false")) ||
		!bytes.Contains(raw, []byte("phase=cold start=3 finish=5 wall_ms=2")) ||
		!bytes.Contains(raw, []byte("phase=warm_noop start=6 finish=0 wall_ms=0")) {
		t.Fatal("private diagnostic lost or completed an actual slot", err)
	}
	if recorder.active != 1 || !recorder.failed || !recorder.stopped || recorder.slots[1].value.FinishEventOrdinal != 0 {
		t.Fatal("private retention repaired recorder state")
	}
}

func TestExecutionFailureDiagnosticRetainsPhaseOperationAndClosure(t *testing.T) {
	root := executionAuthorizationTestRoot(t)
	flow := &ExecutionEpochOne{}
	flow.recordExecutionPhaseFailure("process_restart", checkpointRestartError("store successor", errors.New("protocol refused")), errors.New("disk sample refused"))
	flow.recordExecutionPhaseFailure("process_restart", errors.New("later failure"), errors.New("later close"))
	flow.recordExecutionPhaseFailure("pressure_80", nil, errors.New("phase begin refused"))
	if err := retainExecutionFailureDiagnostic(root, "receipt_composition", nil, flow, ErrExecutionEpochOne, nil, "", nil, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
	if err != nil || !bytes.Contains(raw, []byte("phase_failure_phase=\"process_restart\"")) ||
		!bytes.Contains(raw, []byte("phase_failure_operation=\"execution epoch-one launch unavailable or incomplete: checkpoint restart store successor: protocol refused\"")) ||
		!bytes.Contains(raw, []byte("phase_failure_closure=\"disk sample refused\"")) ||
		!bytes.Contains(raw, []byte("phase_failure_phase=\"pressure_80\"")) ||
		!bytes.Contains(raw, []byte("phase_failure_operation=\"<nil>\"")) ||
		!bytes.Contains(raw, []byte("phase_failure_closure=\"phase begin refused\"")) || bytes.Contains(raw, []byte("later failure")) {
		t.Fatal("private phase failure distinction lost", err)
	}
}

func TestExecutionFailureDiagnosticPressurePrefix(t *testing.T) {
	for _, mode := range []string{"same owner", "restored owner", "not done", "root unjoined", "no inspection"} {
		t.Run(mode, func(t *testing.T) {
			root := executionAuthorizationTestRoot(t)
			archive := &ExecutionEpochOneRun{done: make(chan struct{}), result: ExecutionEpochOneResult{RootJoined: true}, inspection: &executionEpochInspection{}}
			mutation := executionPressureBallastMutation{
				Before: executionPressureBallastSample{Used: 90, Available: 10, Allocated: 60, FreeBlocks: 11},
				After:  executionPressureBallastSample{Used: 74, Available: 26, Allocated: 45, FreeBlocks: 27},
			}
			mutation.Settlement.observe(mutation.Before)
			mutation.Settlement.observe(mutation.After)
			completed := mutation
			completed.Fence = time.Unix(1, 0)
			archive.inspection.retainPressureBallast(0, completed, nil)
			archive.inspection.retainPressureBallast(2, mutation, errPressureVolume)
			archive.inspection.retainPressureQuiet(mutation.Settlement, nil)
			want := archive.inspection.pressure.ballast
			current, owner := archive, archive
			if mode == "same owner" {
				owner = nil // Exercise the fallback to the current owner.
			}
			if mode == "restored owner" {
				current = &ExecutionEpochOneRun{done: make(chan struct{}), result: ExecutionEpochOneResult{RootJoined: true}, inspection: &executionEpochInspection{}}
				close(current.done)
			}
			if mode != "not done" {
				close(archive.done)
			}
			if mode == "root unjoined" {
				archive.result.RootJoined = false
			}
			if mode == "no inspection" {
				archive.inspection = nil
			}
			if err := retainExecutionFailureDiagnostic(root, "receipt_composition", nil, nil, ErrExecutionEpochOne, nil, "", current, owner); err != nil {
				t.Fatal(err)
			}
			raw, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
			if err != nil {
				t.Fatal(err)
			}
			available := mode == "same owner" || mode == "restored owner"
			if !bytes.Contains(raw, []byte("pressure_ballast_available="+strconv.FormatBool(available))) {
				t.Fatal("pressure snapshot availability is incorrect")
			}
			if available {
				for _, fragment := range []string{
					"pressure_quiet_complete=true samples=2 used_changes=1 min_used=74 max_used=90 max_used_step=16 min_free_blocks=11 max_free_blocks=27",
					"pressure_ballast_index=0 attempted=true complete=true fence_present=true",
					"pressure_ballast_index=2 attempted=true complete=false fence_present=false before_used=90 before_available=10 before_allocated=60 after_used=74 after_available=26 after_allocated=45",
					"pressure_ballast_index=3 attempted=false complete=false fence_present=false before_used=0 before_available=0 before_allocated=0 after_used=0 after_available=0 after_allocated=0",
					"pressure_settlement_index=2 samples=2 used_changes=1 min_used=74 max_used=90 max_used_step=16 min_free_blocks=11 max_free_blocks=27 before_free_blocks=11 after_free_blocks=27",
					"pressure_settlement_endpoint_index=2 endpoint=first used=90 available=10 allocated=60 free_blocks=11",
					"pressure_settlement_endpoint_index=2 endpoint=last used=74 available=26 allocated=45 free_blocks=27",
					"pressure_settlement_index=3 samples=0 used_changes=0 min_used=0 max_used=0 max_used_step=0 min_free_blocks=0 max_free_blocks=0",
				} {
					if !bytes.Contains(raw, []byte(fragment)) {
						t.Fatal("pressure prefix lost or repaired", fragment)
					}
				}
				if bytes.Count(raw, []byte("pressure_ballast_index=")) != 4 {
					t.Fatal("pressure prefix is not the fixed four rows")
				}
				if bytes.Contains(raw, []byte("pressure_settlement_endpoint_index=1 ")) || bytes.Contains(raw, []byte("pressure_settlement_endpoint_index=3 ")) {
					t.Fatal("missing settlement samples invented endpoints")
				}
			} else if bytes.Contains(raw, []byte("pressure_ballast_index=")) {
				t.Fatal("unavailable pressure state was read")
			}
			if archive.inspection != nil && archive.inspection.pressure.ballast != want {
				t.Fatal("diagnostic retention mutated the pressure prefix")
			}
		})
	}
}
