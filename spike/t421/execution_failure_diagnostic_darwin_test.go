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
			err := retainExecutionFailureDiagnostic(root, "receipt_composition", recorder, original, receiptErr, "original whole process refusal", run)
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
				if mode != "joined" {
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
	err := retainExecutionFailureDiagnostic(root, "package_construction", nil, original, nil, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root.path, executionFailureSummaryName))
	if err != nil || len(raw) > executionFailureSummaryLimit || !bytes.Contains(raw, []byte("execution_failure_truncated=true")) || bytes.Contains(raw, []byte(strings.Repeat("original failure ", 129))) {
		t.Fatal("summary bound/truncation lost", err)
	}
	// An existing diagnostic cannot be retried or overwrite the original error.
	failedRetention := retainExecutionFailureDiagnostic(root, "package_construction", nil, original, nil, "", nil)
	combined := errors.Join(original, failedRetention)
	if !errors.Is(combined, original) || !errors.Is(combined, errExecutionFailureDiagnostic) {
		t.Fatal("retention replaced original failure")
	}
	fresh := executionAuthorizationTestRoot(t)
	if retainExecutionFailureDiagnostic(fresh, "package_emit", nil, nil, nil, "", nil) == nil {
		t.Fatal("success wrote diagnostics")
	}
	entries, err := os.ReadDir(fresh.path)
	if err != nil || len(entries) != 0 {
		t.Fatal("success root changed", err)
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
	if err := retainExecutionFailureDiagnostic(root, "receipt_composition", recorder, ErrExecutionEpochOne, nil, "", nil); err != nil {
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
