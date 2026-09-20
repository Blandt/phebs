package t422q

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bmeddeb/phebs/spike/t4013"
)

func TestProjectReceiptCollapsesV31AndV32RetryConflicts(t *testing.T) {
	t.Run("v31 repeated rows become one episode", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join("..", "t4013", "t4013u-v31-diagnostic-limit-evidence.json"))
		if err != nil {
			t.Fatal(err)
		}
		var fixture struct {
			Wait t4013.ConvergenceWaitObservation `json:"diagnostic_limit_wait"`
		}
		if err := json.Unmarshal(raw, &fixture); err != nil {
			t.Fatal(err)
		}
		receipt := t4013.Receipt{
			Schema:           t4013.ReceiptSchemaV31,
			MeasuredOn:       "2026-08-20",
			Outcome:          "stopped",
			ConvergenceWaits: []t4013.ConvergenceWaitObservation{fixture.Wait},
			Failures: []t4013.FailureObservation{{
				Phase: "stale_worker", Class: "oracle", Code: "convergence_transition_limit_exceeded",
			}},
			Decision: t4013.DecisionObservation{Selected: "unclassified"},
		}
		episodes, err := ProjectReceipt("receipt_001", receipt)
		if err != nil {
			t.Fatal(err)
		}
		if len(episodes) != 1 {
			t.Fatalf("got %d episodes, want 1", len(episodes))
		}
		episode := episodes[0]
		if episode.ModelInput.Stage != "extraction_publication" ||
			episode.ModelInput.Class != "status" ||
			episode.ModelInput.HTTPStatus != 409 ||
			episode.ModelInput.HTTPReason != "status_other" ||
			episode.Facts.Occurrences != 15 ||
			episode.Facts.PendingObservations != 17 ||
			episode.Facts.SameStagePending != 15 ||
			!episode.Facts.ProgressResumed ||
			episode.Facts.DurationBucket != "ge_300s" ||
			episode.Resolution.WaitOutcome != "diagnostic_limit" {
			t.Fatalf("unexpected V31 projection: %#v", episode)
		}
		encoded, err := json.Marshal(episode.ModelInput)
		if err != nil {
			t.Fatal(err)
		}
		for _, forbidden := range []string{"sha256", "decision", "outcome", "receipt"} {
			if strings.Contains(string(encoded), forbidden) {
				t.Fatalf("model input contains forbidden field %q: %s", forbidden, encoded)
			}
		}
	})

	t.Run("v32 aggregate is not double counted or stage attributed", func(t *testing.T) {
		wait := t4013.ConvergenceWaitObservation{
			Profile:                          "semantic-262144-v1",
			Label:                            "stale-worker",
			Revision:                         "a",
			Outcome:                          "converged",
			LastStage:                        "complete",
			Attempts:                         10,
			ProgressChanges:                  2,
			ProgressRetryConflicts:           6,
			ProgressRetryConflictFirstWallMS: 1_000,
			ProgressRetryConflictLastWallMS:  401_000,
			DeadlineMS:                       900_000,
			InspectionTransitions: []t4013.ConvergenceTransitionObservation{
				{Stage: "extraction_publication", Class: "status", HTTPStatus: 409, HTTPReason: "409_stale", WallMS: 1_000},
				{Stage: "extraction_publication", Class: "pending", WallMS: 2_000},
				{Stage: "extraction_publication", Class: "status", HTTPStatus: 409, HTTPReason: "409_stale", WallMS: 3_000},
				{Stage: "repository_index", Class: "status", HTTPStatus: 409, HTTPReason: "409_stale", WallMS: 3_500},
				{Stage: "complete", Class: "complete", WallMS: 4_000},
			},
		}
		episodes, err := ProjectReceipt("receipt_002", t4013.Receipt{
			Schema: t4013.ReceiptSchemaV32, MeasuredOn: "2026-08-21", Outcome: "completed",
			ConvergenceWaits: []t4013.ConvergenceWaitObservation{wait},
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(episodes) != 2 || episodes[0].Facts.Occurrences != 6 ||
			episodes[0].ModelInput.Stage != "" || episodes[0].ModelInput.HTTPReason != "409_stale" ||
			episodes[1].ModelInput.Stage != "repository_index" || episodes[1].ModelInput.HTTPReason != "409_stale" {
			t.Fatalf("unexpected V32 projection: %#v", episodes)
		}
	})
}
