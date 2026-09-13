//go:build darwin

package t421

import (
	"os"
	"strings"
	"testing"
)

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
