//go:build darwin

package t421

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

const (
	executionFailureSummaryLimit = 64 << 10
	executionFailureFieldLimit   = 2 << 10
	executionFailureOutputLimit  = 64 << 20
	executionFailureBodyLimit    = 64 << 10
	executionFailureSummaryName  = "execution-failure.txt"
	executionFailureOutputName   = "execution-failure-server.log"
	executionFailureBackupName   = "execution-failure-backup.log"
	executionFailureRestoreName  = "execution-failure-restore.log"
	executionFailureBodyName     = "execution-failure-response.body"
)

var errExecutionFailureDiagnostic = errors.New("private execution failure diagnostic unavailable; original failure and custody retained")

// This is unsigned private troubleshooting, never returned evidence. The caller
// has already stopped owners and joined the whole-process observer. Exact root
// cleanup makes diagnostics unavailable; no directory is recreated or retried.
func retainExecutionFailureDiagnostic(root productionRoot, stage string, recorder *executionPhaseEventRecorder, flow *ExecutionEpochOne, executionErr, resultErr error, wholeProcessRefusal string, run, archiveRun *ExecutionEpochOneRun) error {
	if executionErr == nil && resultErr == nil || checkExecutionFailureRoot(root) != nil {
		return errExecutionFailureDiagnostic
	}
	switch stage {
	case "receipt_composition", "package_construction", "owner_close", "package_emit", "execution_stopped":
	default:
		return errExecutionFailureDiagnostic
	}
	var summary executionFailureSummary
	_, _ = fmt.Fprintf(&summary, "private unsigned diagnostic; not receipt evidence\nstage=%s\n", stage)
	summary.failure("execution_failure", executionErr)
	summary.failure("result_failure", resultErr)
	summary.text("whole_process_refusal", wholeProcessRefusal)
	summary.phases(recorder)
	summary.phaseFailures(flow)
	var output, backup, restore, body []byte
	remaining := executionFailureOutputLimit
	if run != nil {
		select {
		case <-run.done:
			// done publishes the stop diagnostic. Each native Wait separately
			// owns its output buffer, even when the byte allowance is shared.
			run.mu.Lock()
			joined := run.result.RootJoined
			stopped, native := run.stopDiagnostic, run.nativeStopErr
			captured, inspection, process := run.output, run.inspection, run.processObservation
			run.mu.Unlock()
			_, _ = fmt.Fprintf(&summary, "joined_server_output=%t\n", joined)
			summary.text("stop_wake", stopped.Wake)
			for _, field := range []struct {
				name string
				err  error
			}{
				{"stop_context", stopped.Before.Context}, {"stop_dispatch", stopped.Before.Dispatch},
				{"stop_control", stopped.Before.Control}, {"stop_store", stopped.Before.Store},
				{"stop_existing", stopped.Before.Existing}, {"stop_wake_error", stopped.Before.WakeError},
				{"stop_wait", stopped.Before.Wait}, {"dispatch_receiver", stopped.DispatchReceiver},
				{"dispatch_join", stopped.DispatchJoin}, {"store_join", stopped.StoreJoin},
				{"dispatch_final", stopped.DispatchFinal}, {"store_final", stopped.StoreFinal},
				{"stop_native", stopped.NativeStop}, {"stop_output", stopped.Output}, {"native_stop", native},
			} {
				summary.failure(field.name, field.err)
			}
			summary.admission("admission_before", stopped.Before.Admission)
			summary.admission("admission_after_join", stopped.AdmissionAfterJoin)
			if process != nil && process.gauge != nil {
				summary.text("server_process_refusal", process.gauge.privateRefusal())
			}
			if joined {
				output = summary.output("server", captured, &remaining)
				if inspection != nil {
					inspection.mu.Lock()
					failure, status, ordinal := inspection.readFailure, inspection.failureStatus, inspection.failureOrdinal
					body = inspection.failureBody
					inspection.mu.Unlock()
					summary.text("inspection_stage", failure.Stage)
					summary.failure("inspection_failure", failure.Cause)
					_, _ = fmt.Fprintf(&summary, "inspection_ordinal=%d response_status=%d response_ordinal=%d response_observed_bytes=%d response_truncated=%t\n", failure.Ordinal, status, ordinal, len(body), len(body) > executionFailureBodyLimit)
					body = body[:min(len(body), executionFailureBodyLimit)]
				}
			}
		default:
			// Do not acquire output/inspection locks or inspect a live buffer.
			_, _ = fmt.Fprintln(&summary, "server_stop_unjoined=true; output and inspection untouched")
		}
	}
	// The sequence keeps the actual epoch-four owner after restored startup
	// clears flow.retained. Its done predates restore, so use each native join.
	if archiveRun == nil {
		archiveRun = run
	}
	if archiveRun != nil {
		summary.pressureBallast(archiveRun)
		var rows [2]struct {
			started, joined, empty, complete, digest bool
			workComplete, scanComplete               bool
			bound, measured, unavailable, exceeded   bool
			attempts, completed                      uint64
			output                                   *checkoutCommandOutput
		}
		archiveRun.mu.Lock()
		rows[0].started, rows[0].joined, rows[0].empty = archiveRun.backupStarted, archiveRun.backupJoined, archiveRun.backupSessionEmpty
		rows[0].complete, rows[0].digest = archiveRun.backupComplete, archiveRun.backupManifestSHA256 != ""
		rows[1].started, rows[1].joined, rows[1].empty = archiveRun.restoreStarted, archiveRun.restoreJoined, archiveRun.restoreSessionEmpty
		rows[1].complete, rows[1].digest = archiveRun.restoreComplete, archiveRun.restoreManifestSHA256 != ""
		for i, work := range []*ExecutionAttemptObservation{&archiveRun.backupWork, &archiveRun.result.RestoreWork} {
			row := &rows[i]
			row.workComplete, row.scanComplete = work.Complete, work.ScanComplete
			bytes := &work.WorkspaceBytes
			row.bound, row.measured, row.unavailable, row.exceeded = bytes.Bound, bytes.Complete, bytes.Unavailable, bytes.LimitExceeded
			row.attempts, row.completed = bytes.Phases[11].Attempts, bytes.Phases[11].Completed
			if row.started && row.joined && archiveRun.backupOutput != nil {
				if i == 0 {
					row.output = archiveRun.backupOutput.backup
				} else {
					row.output = archiveRun.backupOutput.restore
				}
			}
		}
		archiveRun.mu.Unlock()
		for i, name := range []string{"backup", "restore"} {
			row := rows[i]
			_, _ = fmt.Fprintf(&summary, "%s_started=%t joined=%t session_empty=%t complete=%t manifest_digest_present=%t work_complete=%t scan_complete=%t workspace_bound=%t workspace_complete=%t workspace_unavailable=%t workspace_limit_exceeded=%t workspace_attempts=%d workspace_completed=%d\n",
				name, row.started, row.joined, row.empty, row.complete, row.digest, row.workComplete, row.scanComplete, row.bound, row.measured, row.unavailable, row.exceeded, row.attempts, row.completed)
			raw := summary.output(name, row.output, &remaining)
			if i == 0 {
				backup = raw
			} else {
				restore = raw
			}
		}
	}
	for _, leaf := range []struct {
		name  string
		raw   []byte
		limit int
	}{{executionFailureSummaryName, summary.bytes(), executionFailureSummaryLimit}, {executionFailureOutputName, output, executionFailureOutputLimit},
		{executionFailureBackupName, backup, executionFailureOutputLimit}, {executionFailureRestoreName, restore, executionFailureOutputLimit}, {executionFailureBodyName, body, executionFailureBodyLimit}} {
		if len(leaf.raw) != 0 {
			if err := writeExecutionFailureLeaf(root, leaf.name, leaf.raw, leaf.limit); err != nil {
				return err
			}
		}
	}
	return nil
}

// The epoch-four owner retains the fixed pressure prefix even after restore.
// Copy only joined scalar state; never resample the volume or repair a row.
func (w *executionFailureSummary) pressureBallast(run *ExecutionEpochOneRun) {
	var rows [4]executionPressureBallastObservation
	available := false
	select {
	case <-run.done:
		run.mu.Lock()
		joined, inspection := run.result.RootJoined, run.inspection
		run.mu.Unlock()
		if joined && inspection != nil {
			inspection.mu.Lock()
			rows = inspection.pressure.ballast
			inspection.mu.Unlock()
			available = true
		}
	default:
	}
	_, _ = fmt.Fprintf(w, "pressure_ballast_available=%t\n", available)
	if available {
		for i, row := range rows {
			before, after := row.Mutation.Before, row.Mutation.After
			_, _ = fmt.Fprintf(w, "pressure_ballast_index=%d attempted=%t complete=%t fence_present=%t before_used=%d before_available=%d before_allocated=%d after_used=%d after_available=%d after_allocated=%d\n",
				i, row.Attempted, row.Complete, !row.Mutation.Fence.IsZero(), before.Used, before.Available, before.Allocated, after.Used, after.Available, after.Allocated)
		}
	}
}

// Borrow only a joined buffer; all three process logs share one retention cap.
// Selection is deterministic: current server, then backup, then restore.
func (w *executionFailureSummary) output(name string, captured *checkoutCommandOutput, remaining *int) []byte {
	if captured == nil {
		return nil
	}
	raw := captured.buffer.Bytes()
	kept := min(len(raw), *remaining)
	_, _ = fmt.Fprintf(w, "%s_output_observed_bytes=%d %s_output_retained_bytes=%d %s_output_truncated=%t\n", name, len(raw), name, kept, name, kept < len(raw))
	*remaining -= kept
	return raw[:kept]
}

func (w *executionFailureSummary) phaseFailures(flow *ExecutionEpochOne) {
	for _, value := range flow.executionPhaseFailureSnapshot() {
		w.text("phase_failure_phase", value.phase)
		w.failure("phase_failure_operation", value.operation)
		w.failure("phase_failure_closure", value.closure)
	}
}

// Only the small summary is buffered here. Existing joined output/body slices
// are borrowed unchanged; retaining them never allocates another 64-MiB buffer.
type executionFailureSummary struct {
	buffer    bytes.Buffer
	truncated bool
}

const executionFailureSummaryTruncated = "\nsummary_truncated=true\n"

// Error() may itself materialize an owner's joined error string. Clip that
// result before fmt sees it; never format a whole error tree or stop struct.
func (w *executionFailureSummary) failure(name string, err error) {
	if err == nil {
		w.text(name, "<nil>")
		return
	}
	w.text(name, err.Error())
}

func (w *executionFailureSummary) text(name, value string) {
	_, _ = fmt.Fprintf(w, "%s=%q %s_truncated=%t\n", name, value[:min(len(value), executionFailureFieldLimit)], name, len(value) > executionFailureFieldLimit)
}

func (w *executionFailureSummary) admission(name string, value *epochAdmissionFailure) {
	_, _ = fmt.Fprintf(w, "%s_present=%t\n", name, value != nil)
	if value != nil {
		w.text(name+"_stage", value.Stage)
		_, _ = fmt.Fprintf(w, "%s_site=%d %s_deadline_expired=%t\n", name, value.Site, name, value.DeadlineExpired)
		w.failure(name+"_check", value.Check)
		w.failure(name+"_context", value.Context)
	}
}

// A private scalar snapshot preserves failed/active slots that the strict public
// snapshot correctly refuses. It asserts no completion, admission or receipt.
func (w *executionFailureSummary) phases(recorder *executionPhaseEventRecorder) {
	if recorder == nil {
		_, _ = fmt.Fprintln(w, "phase_recorder_available=false")
		return
	}
	var slots [15]struct {
		phase               string
		start, finish, wall uint64
	}
	recorder.mu.Lock()
	count, active, next, failed, stopped := len(recorder.slots), recorder.active, recorder.next, recorder.failed, recorder.stopped
	for i := range min(count, len(slots)) {
		row := &recorder.slots[i].value
		slots[i].phase, slots[i].start, slots[i].finish, slots[i].wall = row.Phase, row.StartEventOrdinal, row.FinishEventOrdinal, uint64(row.Metrics.WallMS)
	}
	recorder.mu.Unlock()
	_, _ = fmt.Fprintf(w, "phase_recorder_available=true active=%d next=%d failed=%t stopped=%t observed_slots=%d slots_truncated=%t\n", active, next, failed, stopped, count, count > len(slots))
	for i, row := range slots[:min(count, len(slots))] {
		_, _ = fmt.Fprintf(w, "phase_index=%d phase=%s start=%d finish=%d wall_ms=%d phase_truncated=%t\n", i, row.phase[:min(len(row.phase), executionFailureFieldLimit)], row.start, row.finish, row.wall, len(row.phase) > executionFailureFieldLimit)
	}
}

func (w *executionFailureSummary) Write(raw []byte) (int, error) {
	n := min(len(raw), executionFailureSummaryLimit-len(executionFailureSummaryTruncated)-w.buffer.Len())
	_, _ = w.buffer.Write(raw[:n])
	w.truncated = w.truncated || n < len(raw)
	return len(raw), nil
}

func (w *executionFailureSummary) bytes() []byte {
	if w.truncated {
		_, _ = w.buffer.WriteString(executionFailureSummaryTruncated)
	}
	return w.buffer.Bytes()
}

func checkExecutionFailureRoot(root productionRoot) error {
	if root.info == nil || root.info.Mode() != os.ModeDir|0o700 || pressureRootsUnchanged(root) != nil {
		return errExecutionFailureDiagnostic
	}
	current, err := os.Lstat(root.path)
	if err != nil || current.Mode() != os.ModeDir|0o700 {
		return errExecutionFailureDiagnostic
	}
	return nil
}

// Fixed leaves are opened relative to the already-held root. A replacement
// root/leaf cannot redirect writes; every created prefix remains on refusal.
func writeExecutionFailureLeaf(root productionRoot, name string, raw []byte, limit int) (retErr error) {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || limit < 1 || limit > executionFailureOutputLimit ||
		len(raw) == 0 || len(raw) > limit || checkExecutionFailureRoot(root) != nil {
		return errExecutionFailureDiagnostic
	}
	rootFD := int(root.file.Fd())
	fd, err := unix.Openat(rootFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return errExecutionFailureDiagnostic
	}
	file := os.NewFile(uintptr(fd), name)
	check := func(size int64) error {
		var held, current unix.Stat_t
		if checkExecutionFailureRoot(root) != nil || unix.Fstat(fd, &held) != nil || unix.Fstatat(rootFD, name, &current, unix.AT_SYMLINK_NOFOLLOW) != nil ||
			held.Dev != current.Dev || held.Ino != current.Ino || held.Mode != unix.S_IFREG|0o600 || current.Mode != held.Mode ||
			held.Uid != uint32(os.Geteuid()) || current.Uid != held.Uid || held.Nlink != 1 || current.Nlink != 1 || held.Size != size || current.Size != size {
			return errExecutionFailureDiagnostic
		}
		return nil
	}
	defer func() {
		syncErr := file.Sync()
		if retErr == nil {
			syncErr = errors.Join(syncErr, check(int64(len(raw))))
		}
		if errors.Join(syncErr, file.Close(), root.file.Sync(), checkExecutionFailureRoot(root)) != nil {
			retErr = errExecutionFailureDiagnostic
		}
	}()
	if check(0) != nil {
		return errExecutionFailureDiagnostic
	}
	if n, err := file.Write(raw); err != nil || n != len(raw) || check(int64(len(raw))) != nil {
		return errExecutionFailureDiagnostic
	}
	return nil
}
