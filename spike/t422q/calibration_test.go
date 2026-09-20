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
	wantLogLoss := ((-math.Log(.95)-math.Log(.9))/2 + 6*-math.Log(.95)) / 7
	wantBaselineLogLoss := (6*-math.Log(float64(5)/6) + -math.Log(float64(1)/6)) / 7
	wantBrier := .02125 / 7
	wantBaselineBrier := float64(31) / 252
	wantSkill := 1 - wantBrier/wantBaselineBrier
	wantUpper := 1 - math.Pow(.05, .2)
	if report.Schema != CalibrationReportSchema || report.Model != JevModel || report.QuestionContract != JevQuestionContract ||
		report.DevelopmentReceiptGroups != 2 || report.DevelopmentEpisodes != 4 ||
		report.DevelopmentAdjudicatedEpisodes != 4 || report.DevelopmentAbstainedEpisodes != 0 ||
		!calibrationNear(report.DevelopmentObservationTerminalPrevalence, float64(1)/6) ||
		!calibrationNear(report.DevelopmentRepairRequiredPrevalence, float64(1)/6) ||
		report.TestReceiptGroups != 7 || report.TestEpisodes != 8 || report.TestAdjudicatedEpisodes != 8 ||
		report.TestAbstainedEpisodes != 0 || report.TestSafeLabels != 6 || report.TestUnsafeLabels != 2 ||
		report.TestObservationTerminalFalseLabels != 7 || report.TestObservationTerminalTrueLabels != 1 ||
		report.TestRepairRequiredFalseLabels != 7 || report.TestRepairRequiredTrueLabels != 1 ||
		!calibrationNear(report.TestObservationTerminalBrierScore, wantBrier) ||
		!calibrationNear(report.TestRepairRequiredBrierScore, wantBrier) ||
		!calibrationNear(report.TestObservationTerminalLogLoss, wantLogLoss) ||
		!calibrationNear(report.TestRepairRequiredLogLoss, wantLogLoss) ||
		!calibrationNear(report.BaselineObservationTerminalBrierScore, wantBaselineBrier) ||
		!calibrationNear(report.BaselineRepairRequiredBrierScore, wantBaselineBrier) ||
		!calibrationNear(report.BaselineObservationTerminalLogLoss, wantBaselineLogLoss) ||
		!calibrationNear(report.BaselineRepairRequiredLogLoss, wantBaselineLogLoss) ||
		!calibrationNear(report.ObservationTerminalBrierSkill, wantSkill) ||
		!calibrationNear(report.RepairRequiredBrierSkill, wantSkill) ||
		report.BenignCandidateReceiptGroups != 5 || report.FalseBenignReceiptGroups != 0 ||
		report.ConditionalZeroErrorOneSided95UpperBound == nil || !calibrationNear(*report.ConditionalZeroErrorOneSided95UpperBound, wantUpper) ||
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
		{"missing terminal class", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range l {
				if l[index].Split == CalibrationTestSplit && l[index].ObservationTerminal != nil {
					*l[index].ObservationTerminal = false
				}
			}
			return e, p, l
		}, false, "test_observation_terminal_missing_both_classes"},
		{"nonpositive terminal skill", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range p {
				if l[index].Split != CalibrationTestSplit || l[index].ObservationTerminal == nil {
					continue
				}
				if *l[index].ObservationTerminal {
					p[index].ObservationTerminal = .1
				} else {
					p[index].ObservationTerminal = .2
				}
				p[index].Classification = classify(p[index].ObservationTerminal, p[index].RepairRequired)
			}
			return e, p, l
		}, false, "nonpositive_observation_terminal_brier_skill"},
		{"swapped axes", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range p {
				p[index].ObservationTerminal, p[index].RepairRequired = p[index].RepairRequired, p[index].ObservationTerminal
				p[index].Classification = classify(p[index].ObservationTerminal, p[index].RepairRequired)
			}
			return e, p, l
		}, false, "nonpositive_observation_terminal_brier_skill"},
		{"abstention coverage", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range l {
				if l[index].Split == CalibrationTestSplit && !*l[index].ObservationTerminal && !*l[index].RepairRequired {
					l[index].ObservationTerminal, l[index].RepairRequired = nil, nil
					break
				}
			}
			return e, p, l
		}, false, "test_abstentions_present"},
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
		{"wrong question contract", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			p[0].QuestionContract = "other"
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
		{"non-temporal split", func(e []Episode, p []Prediction, l []HumanLabel) ([]Episode, []Prediction, []HumanLabel) {
			for index := range l {
				if p[index].ReceiptGroup == "receipt_009" {
					l[index].Split = CalibrationDevelopmentSplit
				}
			}
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
		{"receipt_002", CalibrationDevelopmentSplit, .05, .95, &safe, &unsafe},
		{"receipt_002", CalibrationDevelopmentSplit, .05, .05, &safe, &safe},
		{"receipt_003", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_003", CalibrationTestSplit, .1, .1, &safe, &safe},
		{"receipt_004", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_005", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_006", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_007", CalibrationTestSplit, .05, .05, &safe, &safe},
		{"receipt_008", CalibrationTestSplit, .95, .05, &unsafe, &safe},
		{"receipt_009", CalibrationTestSplit, .05, .95, &safe, &unsafe},
	}
	episodes := make([]Episode, 0, len(rows))
	predictions := make([]Prediction, 0, len(rows))
	labels := make([]HumanLabel, 0, len(rows))
	for index, row := range rows {
		day := 1
		_, _ = fmt.Sscanf(row.receipt, "receipt_%03d", &day)
		episode := Episode{
			Schema:     EpisodeSchema,
			Provenance: Provenance{ReceiptGroup: row.receipt, ReceiptSchema: "fixture-v1", MeasuredOn: fmt.Sprintf("2026-08-%02d", day), WaitOrdinal: 0, EpisodeOrdinal: index},
			ModelInput: ShadowState{Schema: StateSchema, Profile: "semantic-262144-v1", WaitLabel: "stale-worker", Revision: "a", Stage: "extraction_publication", Class: "status", HTTPStatus: 409, HTTPReason: "409_stale", DeadlineMS: 1_000},
			Facts:      EpisodeFacts{Occurrences: 1, DurationBucket: "lt_1s", EndReason: "recovered"},
		}
		episode.EpisodeID, _ = canonicalEpisodeID(episode)
		episodes = append(episodes, episode)
		predictions = append(predictions, Prediction{
			Schema: PredictionSchema, EpisodeID: episode.EpisodeID, ReceiptGroup: row.receipt, Model: JevModel,
			QuestionContract:    JevQuestionContract,
			ObservationTerminal: row.terminalScore, RepairRequired: row.repairScore,
			Classification: classify(row.terminalScore, row.repairScore),
		})
		labels = append(labels, HumanLabel{
			Schema: HumanLabelSchema, EpisodeID: episode.EpisodeID, Split: row.split,
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
