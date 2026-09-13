//go:build darwin

package t421

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestExecutionEpochSequenceRefusesUnavailableInputs(t *testing.T) {
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	for _, test := range []struct {
		name   string
		ctx    context.Context
		flow   *ExecutionEpochOne
		volume *executionPressureVolume
	}{
		{"nil_context", nil, &ExecutionEpochOne{}, &executionPressureVolume{}},
		{"canceled_context", canceled, &ExecutionEpochOne{}, &executionPressureVolume{}},
		{"missing_flow", t.Context(), nil, &executionPressureVolume{}},
		{"missing_volume", t.Context(), &ExecutionEpochOne{}, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			var before []PhaseMeasurement
			if test.flow != nil {
				admitted, _, err := newExecutionEventOrdinals().consumeFinalAdmission()
				if err != nil {
					t.Fatal(err)
				}
				now := time.Now()
				recorder, err := newExecutionPhaseEventRecorder(admitted, frozenPhaseOrder(), now, now.Add(time.Hour))
				if err != nil || recorder.beginAt("preflight", now) != nil || recorder.finish("preflight", "passed") != nil {
					t.Fatal("could not prepare the unit-test event prefix", err)
				}
				test.flow.executionPhaseEvents = recorder
				before, err = test.flow.executionPhaseEventEvidence()
				if err != nil {
					t.Fatal(err)
				}
			}
			if _, err := test.flow.authorAAdmitted(test.ctx, time.Time{}, ExecutionFreezeBinding{}, nil); !errors.Is(err, ErrExecutionEpochOne) {
				t.Fatal("unavailable admission was accepted", err)
			}
			result, err := runExecutionEpochSequence(test.ctx, test.flow, test.volume)
			if !errors.Is(err, ErrExecutionEpochOne) || result == nil || result.current != nil || result.phaseEventEvidence() != nil {
				t.Fatal("invalid input acquired an owner or phase evidence", result, err)
			}
			if test.flow != nil && (test.flow.used || test.flow.authored) {
				t.Fatal("invalid input consumed the flow")
			}
			if test.flow != nil {
				after, err := test.flow.executionPhaseEventEvidence()
				if err != nil || !reflect.DeepEqual(after, before) {
					t.Fatal("invalid input advanced the phase recorder", after, err)
				}
			}
		})
	}
}

func TestExecutionEpochSequenceRetainsDetachedFailedPrefix(t *testing.T) {
	admitted, _, err := newExecutionEventOrdinals().consumeFinalAdmission()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	recorder, err := newExecutionPhaseEventRecorder(admitted, frozenPhaseOrder(), now, now.Add(time.Hour))
	if err != nil || recorder.beginAt("preflight", now) != nil || recorder.finish("preflight", "passed") != nil {
		t.Fatal("could not prepare the unit-test event prefix", err)
	}
	// No epoch/tool/dispatch owners exist, so the real start must refuse before
	// launching anything while preserving the attempted cold-phase event row.
	flow := &ExecutionEpochOne{executionPhaseEvents: recorder}
	result, err := runExecutionEpochSequence(t.Context(), flow, &executionPressureVolume{})
	if !errors.Is(err, ErrExecutionEpochOne) || result == nil || result.current != nil || flow.used {
		t.Fatal("missing admission acquired an owner", result, err)
	}
	evidence := result.phaseEventEvidence()
	if len(evidence) != len(frozenPhaseOrder()) || evidence[1].Phase != "cold" ||
		evidence[1].StartEventOrdinal <= evidence[0].FinishEventOrdinal ||
		evidence[1].FinishEventOrdinal <= evidence[1].StartEventOrdinal || evidence[1].Metrics.WallMS == 0 {
		t.Fatal("failed cold attempt lost its actual event prefix", evidence)
	}
	for _, row := range evidence[2:] {
		if !reflect.DeepEqual(row, PhaseMeasurement{Phase: row.Phase}) {
			t.Fatal("unattempted suffix acquired evidence", row)
		}
	}
	want := evidence[1]
	evidence[1].Phase, evidence[1].Metrics.WallMS = "changed", 0
	if !reflect.DeepEqual(result.phaseEventEvidence()[1], want) {
		t.Fatal("returned phase evidence mutated the retained prefix")
	}
}

func TestExecutionEpochSequenceStopPreservesUnjoinedOwner(t *testing.T) {
	for _, result := range []*executionEpochSequenceResult{nil, {}} {
		if _, err := result.stop(t.Context()); !errors.Is(err, ErrExecutionEpochOne) {
			t.Fatal("stop accepted a missing current owner", err)
		}
	}
	run := &ExecutionEpochOneRun{flow: &ExecutionEpochOne{}, stop: make(chan struct{}), done: make(chan struct{})}
	sequence := &executionEpochSequenceResult{current: run}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	for range 2 {
		result, err := sequence.stop(ctx)
		if !errors.Is(err, ErrExecutionEpochOne) || result.RootJoined || result.SessionEmpty || sequence.current != run {
			t.Fatal("canceled stop discarded or falsely joined its owner", result, err)
		}
		select {
		case <-run.stop:
		default:
			t.Fatal("stop was not forwarded to the current owner")
		}
		select {
		case <-run.done:
			t.Fatal("cancellation fabricated owner completion")
		default:
		}
	}
}

func TestExecutionEpochSequenceKeepsExactProductionOrder(t *testing.T) {
	raw, err := os.ReadFile("execution_epoch_sequence_darwin.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	start := strings.Index(text, "func runExecutionEpochSequence(")
	if start < 0 {
		t.Fatal("production sequence function is missing")
	}
	end := strings.Index(text[start:], "func joinedExecutionEpochResult(")
	if end < 0 {
		t.Fatal("production sequence function is missing")
	}
	text = text[start : start+end]
	position := 0
	for _, call := range []string{
		"flow.StartPhysicalB(ctx)", "run.Health(ctx)", "run.ColdToWarm(ctx)", "run.ObserveWarm(ctx)", "run.PhysicalB(ctx)",
		"prior.StartLogicalB(ctx)", "run.Health(ctx)", "run.LogicalB(ctx)",
		"prior.StartReturnACheckpoint(ctx)", "run.Health(ctx)", "run.ReturnA(ctx)", "run.StaleLease(ctx)",
		"prior.CheckpointRestartBackup(ctx)", "run.Health(ctx)", "run.RecoverCheckpoint(ctx)", "run.Pressure(ctx, volume)",
		"run.BackupAndStop(ctx)", "run.RestoreBackup(ctx)", "prior.StartRestored(ctx)", "run.Health(ctx)",
		"run.CompleteArchive(ctx)", "run.CollectRestored(ctx)", "run.QueryRestored(ctx)", "volume.finishRestored(ctx, run)", "run.Wait(ctx)",
	} {
		next := strings.Index(text[position:], call)
		if next < 0 {
			t.Fatalf("production call %q is absent or out of order", call)
		}
		position += next + len(call)
	}
}
