package t422q

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
)

func TestCalibrationEvaluator(t *testing.T) {
	episodes, predictions, labels := calibrationFixture()
	var rawPredictions strings.Builder
	if err := EncodePredictions(&rawPredictions, predictions); err != nil {
		t.Fatal(err)
	}
	decodedPredictions, err := DecodePredictions(strings.NewReader(rawPredictions.String()))
	if err != nil || len(decodedPredictions) != len(predictions) {
		t.Fatal("valid prediction JSONL did not decode", err)
	}
	rawLabels := calibrationLabelJSONL(t, labels)
	decoded, err := DecodeHumanLabels(strings.NewReader(rawLabels))
	if err != nil || len(decoded) != len(labels) {
		t.Fatal("valid human-label JSONL did not decode", err)
	}
	report, err := EvaluateCalibration(episodes, decodedPredictions, decoded)
	if err != nil {
		t.Fatal(err)
	}
	wantLogLoss := ((-math.Log(.95)-math.Log(.9))/2 + 5*-math.Log(.95)) / 6
	wantBaselineLogLoss := -(float64(1)/6*math.Log(float64(1)/6) + float64(5)/6*math.Log(float64(5)/6))
	wantUpper := 1 - math.Pow(.05, .2)
	if report.Schema != CalibrationReportSchema || report.Model != JevModel || report.DevelopmentReceiptGroups != 2 || report.DevelopmentAdjudicatedEpisodes != 4 ||
		!calibrationNear(report.DevelopmentUnsafePrevalence, float64(1)/6) ||
		report.TestReceiptGroups != 6 || report.TestAdjudicatedEpisodes != 7 || report.TestSafeLabels != 6 || report.TestUnsafeLabels != 1 ||
		!report.TestHasBothClasses || !calibrationNear(report.TestBrierScore, .003125) ||
		!calibrationNear(report.TestLogLoss, wantLogLoss) || !calibrationNear(report.BaselineBrierScore, float64(5)/36) ||
		!calibrationNear(report.BaselineLogLoss, wantBaselineLogLoss) || !calibrationNear(report.BrierSkill, .9775) ||
		report.BenignCandidateReceiptGroups != 5 || report.FalseBenignReceiptGroups != 0 ||
		report.ZeroErrorOneSided95UpperBound == nil || !calibrationNear(*report.ZeroErrorOneSided95UpperBound, wantUpper) ||
		!report.ShadowGO || len(report.Reasons) != 0 {
		t.Fatalf("GO report differs: %+v", report)
	}
	if got, want := binaryLogLoss(true, 0), -math.Log(calibrationLogLossEpsilon); got != want || math.IsInf(got, 0) ||
		!calibrationNear(binaryLogLoss(false, 1), -math.Log1p(-(1-calibrationLogLossEpsilon))) {
		t.Fatal("boundary log loss is not finitely clipped")
	}

	predictionFirstLine := strings.Split(strings.TrimSpace(rawPredictions.String()), "\n")[0]
	for _, test := range []struct {
		name string
		raw  string
	}{
		{"unknown field", strings.TrimSuffix(predictionFirstLine, "}") + `,"extra":true}`},
		{"missing zero field", strings.Replace(predictionFirstLine, `"input_tokens":0,`, "", 1)},
		{"oversized row", predictionFirstLine + strings.Repeat(" ", maxEpisodeBytes)},
		{"duplicate id", rawPredictions.String() + predictionFirstLine + "\n"},
	} {
		t.Run("predictions/"+test.name, func(t *testing.T) {
			if _, err := DecodePredictions(strings.NewReader(test.raw)); err == nil {
				t.Fatal("invalid prediction JSONL decoded")
			}
		})
	}

	firstLine := strings.Split(strings.TrimSpace(rawLabels), "\n")[0]
	for _, test := range []struct {
		name string
		raw  string
	}{
		{"unknown field", strings.TrimSuffix(firstLine, "}") + `,"extra":true}`},
		{"missing nullable field", fmt.Sprintf(`{"schema":%q,"episode_id":%q,"split":"test","observation_terminal":null,"basis":%q}`, HumanLabelSchema, labels[0].EpisodeID, HumanLabelBasis)},
		{"partial abstention", fmt.Sprintf(`{"schema":%q,"episode_id":%q,"split":"test","observation_terminal":true,"repair_required":null,"basis":%q}`, HumanLabelSchema, labels[0].EpisodeID, HumanLabelBasis)},
		{"duplicate id", rawLabels + firstLine + "\n"},
	} {
		t.Run("labels/"+test.name, func(t *testing.T) {
			if _, err := DecodeHumanLabels(strings.NewReader(test.raw)); err == nil {
				t.Fatal("invalid human-label JSONL decoded")
			}
		})
	}

	for _, test := range []struct {
		name       string
		mutate     func([]Episode, []Prediction, []HumanLabel) ([]Episode, []Prediction, []HumanLabel)
		wantError  bool
		wantReason string
	}{
		{"false benign", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range p {
				if l[index].Split == CalibrationTestSplit && l[index].ObservationTerminal != nil && (*l[index].ObservationTerminal || *l[index].RepairRequired) {
					p[index].ObservationTerminal, p[index].RepairRequired = .05, .05
					p[index].Classification = classify(.05, .05)
				}
			}
			return e, p, l
		}, false, "false_benign_receipt_groups_present"},
		{"insufficient benign groups", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range p {
				if p[index].ReceiptGroup == "receipt_007" {
					p[index].ObservationTerminal, p[index].RepairRequired = .11, .05
					p[index].Classification = classify(.11, .05)
				}
			}
			return e, p, l
		}, false, "fewer_than_five_benign_candidate_receipt_groups"},
		{"missing test class", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range l {
				if l[index].Split == CalibrationTestSplit && l[index].ObservationTerminal != nil {
					*l[index].ObservationTerminal, *l[index].RepairRequired = false, false
				}
			}
			return e, p, l
		}, false, "test_missing_both_unsafe_classes"},
		{"nonpositive skill", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range p {
				if l[index].Split != CalibrationTestSplit || l[index].ObservationTerminal == nil {
					continue
				}
				if *l[index].ObservationTerminal || *l[index].RepairRequired {
					p[index].ObservationTerminal, p[index].RepairRequired = .11, 0
				} else {
					p[index].ObservationTerminal, p[index].RepairRequired = .1, .1
				}
				p[index].Classification = classify(p[index].ObservationTerminal, p[index].RepairRequired)
			}
			return e, p, l
		}, false, "nonpositive_brier_skill"},
		{"prediction set", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			return e, p[:len(p)-1], l
		}, true, ""},
		{"duplicate episode", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			return append(e, e[0]), p, l
		}, true, ""},
		{"duplicate prediction", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			return e, append(p, p[0]), l
		}, true, ""},
		{"duplicate label", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			return e, p, append(l, l[0])
		}, true, ""},
		{"wrong model", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			p[0].Model = "jev-other"
			return e, p, l
		}, true, ""},
		{"invalid score", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			p[0].ObservationTerminal = math.NaN()
			return e, p, l
		}, true, ""},
		{"receipt split", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			l[2].Split = CalibrationTestSplit
			return e, p, l
		}, true, ""},
	} {
		t.Run("evaluation/"+test.name, func(t *testing.T) {
			e, p, l := calibrationFixture()
			e, p, l = test.mutate(e, p, l)
			got, err := EvaluateCalibration(e, p, l)
			if test.wantError {
				if err == nil {
					t.Fatalf("invalid join produced report: %+v", got)
				}
				return
			}
			if err != nil || got.ShadowGO || !slices.Contains(got.Reasons, test.wantReason) {
				t.Fatalf("refusal = %+v, %v", got, err)
			}
		})
	}
}

func calibrationFixture() ([]Episode, []Prediction, []HumanLabel) {
	type row struct {
		receipt, split string
		terminalScore  float64
		repairScore    float64
		terminalLabel  *bool
		repairLabel    *bool
	}
	safe, unsafe := false, true
	rows := []row{
		{"receipt_001", CalibrationDevelopmentSplit, .05, .05, &safe, &safe},
		{"receipt_002", CalibrationDevelopmentSplit, .95, .05, &unsafe, &safe},
		{"receipt_002", CalibrationDevelopmentSplit, .05, .05, &safe, &safe},
		{"receipt_002", CalibrationDevelopmentSplit, .05, .05, &safe, &safe},
		{"receipt_003", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_003", CalibrationTestSplit, .1, .1, &safe, &safe},
		{"receipt_004", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_005", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_006", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_007", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_008", CalibrationTestSplit, .95, .05, &unsafe, &safe},
		{"receipt_009", CalibrationTestSplit, .05, .05, nil, nil},
	}
	episodes := make([]Episode, 0, len(rows))
	predictions := make([]Prediction, 0, len(rows))
	labels := make([]HumanLabel, 0, len(rows))
	for index, row := range rows {
		id := "sha256:" + fmt.Sprintf("%064x", index+1)
		episodes = append(episodes, Episode{
			Schema: EpisodeSchema, EpisodeID: id,
			Provenance: Provenance{ReceiptGroup: row.receipt, ReceiptSchema: "fixture-v1", WaitOrdinal: 0, EpisodeOrdinal: index},
			ModelInput: ShadowState{Schema: StateSchema, Profile: "fixture", WaitLabel: "wait", Revision: "a", Class: "status", DeadlineMS: 1_000},
			Facts:      EpisodeFacts{Occurrences: 1, DurationBucket: "lt_1s", EndReason: "recovered"},
		})
		predictions = append(predictions, Prediction{
			Schema: PredictionSchema, EpisodeID: id, ReceiptGroup: row.receipt, Model: JevModel,
			ObservationTerminal: row.terminalScore, RepairRequired: row.repairScore,
			Classification: classify(row.terminalScore, row.repairScore),
		})
		labels = append(labels, HumanLabel{
			Schema: HumanLabelSchema, EpisodeID: id, Split: row.split,
			ObservationTerminal: row.terminalLabel, RepairRequired: row.repairLabel, Basis: HumanLabelBasis,
		})
	}
	return episodes, predictions, labels
}

func calibrationLabelJSONL(t *testing.T, labels []HumanLabel) string {
	t.Helper()
	var output strings.Builder
	encoder := json.NewEncoder(&output)
	for _, label := range labels {
		if err := encoder.Encode(label); err != nil {
			t.Fatal(err)
		}
	}
	return output.String()
}

func calibrationNear(got, want float64) bool {
	return math.Abs(got-want) < 1e-12
}
