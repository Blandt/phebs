package t422q

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
)

const (
	HumanLabelSchema            = "t422q-human-label-v1"
	HumanLabelBasis             = "human_adjudication"
	CalibrationReportSchema     = "t422q-calibration-report-v1"
	CalibrationDevelopmentSplit = "development"
	CalibrationTestSplit        = "test"
	calibrationLogLossEpsilon   = 1e-15
)

type HumanLabel struct {
	Schema              string `json:"schema"`
	EpisodeID           string `json:"episode_id"`
	Split               string `json:"split"`
	ObservationTerminal *bool  `json:"observation_terminal"`
	RepairRequired      *bool  `json:"repair_required"`
	Basis               string `json:"basis"`
}

type CalibrationReport struct {
	Schema                         string   `json:"schema"`
	Model                          string   `json:"model"`
	DevelopmentReceiptGroups       int      `json:"development_receipt_groups"`
	DevelopmentAdjudicatedEpisodes int      `json:"development_adjudicated_episodes"`
	DevelopmentUnsafePrevalence    float64  `json:"development_unsafe_prevalence"`
	TestReceiptGroups              int      `json:"test_receipt_groups"`
	TestAdjudicatedEpisodes        int      `json:"test_adjudicated_episodes"`
	TestSafeLabels                 int      `json:"test_safe_labels"`
	TestUnsafeLabels               int      `json:"test_unsafe_labels"`
	TestHasBothClasses             bool     `json:"test_has_both_classes"`
	TestBrierScore                 float64  `json:"test_brier_score"`
	TestLogLoss                    float64  `json:"test_log_loss"`
	BaselineBrierScore             float64  `json:"baseline_brier_score"`
	BaselineLogLoss                float64  `json:"baseline_log_loss"`
	BrierSkill                     float64  `json:"brier_skill"`
	BenignCandidateReceiptGroups   int      `json:"benign_candidate_receipt_groups"`
	FalseBenignReceiptGroups       int      `json:"false_benign_receipt_groups"`
	ZeroErrorOneSided95UpperBound  *float64 `json:"zero_error_one_sided_95_upper_bound"`
	ShadowGO                       bool     `json:"shadow_go"`
	Reasons                        []string `json:"reasons"`
}

type humanLabelWire struct {
	Schema              string          `json:"schema"`
	EpisodeID           string          `json:"episode_id"`
	Split               string          `json:"split"`
	ObservationTerminal json.RawMessage `json:"observation_terminal"`
	RepairRequired      json.RawMessage `json:"repair_required"`
	Basis               string          `json:"basis"`
}

type predictionWire struct {
	Schema              string   `json:"schema"`
	EpisodeID           string   `json:"episode_id"`
	ReceiptGroup        string   `json:"receipt_group"`
	Model               string   `json:"model"`
	ObservationTerminal *float64 `json:"observation_terminal"`
	RepairRequired      *float64 `json:"repair_required"`
	Classification      string   `json:"classification"`
	InputTokens         *int64   `json:"input_tokens"`
	OutputTokens        *int64   `json:"output_tokens"`
}

func DecodeHumanLabels(reader io.Reader) ([]HumanLabel, error) {
	if reader == nil {
		return nil, errors.New("human-label reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxEpisodeBytes)
	seen := make(map[string]struct{})
	var labels []HumanLabel
	for scanner.Scan() {
		if len(labels) == maxEpisodes {
			return nil, errors.New("human-label input exceeds 4096-case bound")
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return nil, errors.New("human-label input contains an empty line")
		}
		label, err := decodeHumanLabel(line)
		if err != nil {
			return nil, fmt.Errorf("decode human label %d: %w", len(labels)+1, err)
		}
		if _, duplicate := seen[label.EpisodeID]; duplicate {
			return nil, fmt.Errorf("human label %d duplicates an episode id", len(labels)+1)
		}
		seen[label.EpisodeID] = struct{}{}
		labels = append(labels, label)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read human labels: %w", err)
	}
	if len(labels) == 0 {
		return nil, errors.New("human-label input is empty")
	}
	return labels, nil
}

func DecodePredictions(reader io.Reader) ([]Prediction, error) {
	if reader == nil {
		return nil, errors.New("prediction reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxEpisodeBytes)
	seen := make(map[string]struct{})
	var predictions []Prediction
	for scanner.Scan() {
		if len(predictions) == maxEpisodes {
			return nil, errors.New("prediction input exceeds 4096-case bound")
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return nil, errors.New("prediction input contains an empty line")
		}
		var wire predictionWire
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&wire); err != nil {
			return nil, fmt.Errorf("decode prediction %d: %w", len(predictions)+1, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("prediction %d has trailing JSON", len(predictions)+1)
		}
		if wire.ObservationTerminal == nil || wire.RepairRequired == nil || wire.InputTokens == nil || wire.OutputTokens == nil {
			return nil, fmt.Errorf("prediction %d omits a required numeric field", len(predictions)+1)
		}
		prediction := Prediction{
			Schema: wire.Schema, EpisodeID: wire.EpisodeID, ReceiptGroup: wire.ReceiptGroup, Model: wire.Model,
			ObservationTerminal: *wire.ObservationTerminal, RepairRequired: *wire.RepairRequired,
			Classification: wire.Classification, InputTokens: *wire.InputTokens, OutputTokens: *wire.OutputTokens,
		}
		if err := validateCalibrationPrediction(prediction); err != nil {
			return nil, fmt.Errorf("prediction %d: %w", len(predictions)+1, err)
		}
		if _, duplicate := seen[prediction.EpisodeID]; duplicate {
			return nil, fmt.Errorf("prediction %d duplicates an episode id", len(predictions)+1)
		}
		seen[prediction.EpisodeID] = struct{}{}
		predictions = append(predictions, prediction)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read predictions: %w", err)
	}
	if len(predictions) == 0 {
		return nil, errors.New("prediction input is empty")
	}
	return predictions, nil
}

func decodeHumanLabel(raw []byte) (HumanLabel, error) {
	var wire humanLabelWire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&wire); err != nil {
		return HumanLabel{}, err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return HumanLabel{}, errors.New("human label has trailing JSON")
	}
	terminal, err := decodeNullableBool("observation_terminal", wire.ObservationTerminal)
	if err != nil {
		return HumanLabel{}, err
	}
	repair, err := decodeNullableBool("repair_required", wire.RepairRequired)
	if err != nil {
		return HumanLabel{}, err
	}
	label := HumanLabel{
		Schema: wire.Schema, EpisodeID: wire.EpisodeID, Split: wire.Split,
		ObservationTerminal: terminal, RepairRequired: repair, Basis: wire.Basis,
	}
	if err := validateHumanLabel(label); err != nil {
		return HumanLabel{}, err
	}
	return label, nil
}

func decodeNullableBool(name string, raw json.RawMessage) (*bool, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("human label omits %s", name)
	}
	if bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var value bool
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("human-label %s is not bool or null", name)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("human-label %s has trailing JSON", name)
	}
	return &value, nil
}

func validateHumanLabel(label HumanLabel) error {
	if label.Schema != HumanLabelSchema || !validSHA256(label.EpisodeID) || label.Basis != HumanLabelBasis {
		return errors.New("human label schema, episode id, or basis is invalid")
	}
	if label.Split != CalibrationDevelopmentSplit && label.Split != CalibrationTestSplit {
		return errors.New("human-label split is invalid")
	}
	if (label.ObservationTerminal == nil) != (label.RepairRequired == nil) {
		return errors.New("human label must adjudicate both questions or abstain from both")
	}
	return nil
}

type calibrationGroup struct {
	count, unsafe       int
	brier, logLoss      float64
	benign, falseBenign bool
}

func EvaluateCalibration(episodes []Episode, predictions []Prediction, labels []HumanLabel) (CalibrationReport, error) {
	if len(episodes) == 0 || len(episodes) > maxEpisodes || len(predictions) == 0 || len(predictions) > maxEpisodes ||
		len(labels) == 0 || len(labels) > maxEpisodes {
		return CalibrationReport{}, errors.New("calibration inputs must each contain 1 to 4096 rows")
	}
	episodeByID := make(map[string]Episode, len(episodes))
	for index, episode := range episodes {
		if err := validateEpisode(episode); err != nil {
			return CalibrationReport{}, fmt.Errorf("episode %d: %w", index+1, err)
		}
		if _, duplicate := episodeByID[episode.EpisodeID]; duplicate {
			return CalibrationReport{}, fmt.Errorf("episode %d duplicates an episode id", index+1)
		}
		episodeByID[episode.EpisodeID] = episode
	}
	predictionByID := make(map[string]Prediction, len(predictions))
	for index, prediction := range predictions {
		if err := validateCalibrationPrediction(prediction); err != nil {
			return CalibrationReport{}, fmt.Errorf("prediction %d: %w", index+1, err)
		}
		episode, exists := episodeByID[prediction.EpisodeID]
		if !exists {
			return CalibrationReport{}, fmt.Errorf("prediction %d has no episode", index+1)
		}
		if prediction.ReceiptGroup != episode.Provenance.ReceiptGroup {
			return CalibrationReport{}, fmt.Errorf("prediction %d receipt group differs from its episode", index+1)
		}
		if _, duplicate := predictionByID[prediction.EpisodeID]; duplicate {
			return CalibrationReport{}, fmt.Errorf("prediction %d duplicates an episode id", index+1)
		}
		predictionByID[prediction.EpisodeID] = prediction
	}
	labelByID := make(map[string]HumanLabel, len(labels))
	receiptSplit := make(map[string]string)
	for index, label := range labels {
		if err := validateHumanLabel(label); err != nil {
			return CalibrationReport{}, fmt.Errorf("human label %d: %w", index+1, err)
		}
		episode, exists := episodeByID[label.EpisodeID]
		if !exists {
			return CalibrationReport{}, fmt.Errorf("human label %d has no episode", index+1)
		}
		if _, duplicate := labelByID[label.EpisodeID]; duplicate {
			return CalibrationReport{}, fmt.Errorf("human label %d duplicates an episode id", index+1)
		}
		if split, exists := receiptSplit[episode.Provenance.ReceiptGroup]; exists && split != label.Split {
			return CalibrationReport{}, fmt.Errorf("receipt group %q crosses calibration splits", episode.Provenance.ReceiptGroup)
		}
		receiptSplit[episode.Provenance.ReceiptGroup] = label.Split
		labelByID[label.EpisodeID] = label
	}
	if len(episodeByID) != len(predictionByID) || len(episodeByID) != len(labelByID) {
		return CalibrationReport{}, errors.New("episode, prediction, and human-label sets differ")
	}

	development := make(map[string]*calibrationGroup)
	test := make(map[string]*calibrationGroup)
	developmentEpisodes, testEpisodes, testSafe, testUnsafe := 0, 0, 0, 0
	for _, episode := range episodes {
		prediction, predicted := predictionByID[episode.EpisodeID]
		label, labelled := labelByID[episode.EpisodeID]
		if !predicted || !labelled {
			return CalibrationReport{}, errors.New("episode, prediction, and human-label sets differ")
		}
		if label.ObservationTerminal == nil {
			continue
		}
		unsafe := *label.ObservationTerminal || *label.RepairRequired
		score := max(prediction.ObservationTerminal, prediction.RepairRequired)
		groups := development
		if label.Split == CalibrationTestSplit {
			groups = test
			testEpisodes++
			if unsafe {
				testUnsafe++
			} else {
				testSafe++
			}
		} else {
			developmentEpisodes++
		}
		group := groups[episode.Provenance.ReceiptGroup]
		if group == nil {
			group = &calibrationGroup{}
			groups[episode.Provenance.ReceiptGroup] = group
		}
		group.count++
		if unsafe {
			group.unsafe++
		}
		delta := score
		if unsafe {
			delta = 1 - score
		}
		group.brier += delta * delta
		group.logLoss += binaryLogLoss(unsafe, score)
		if label.Split == CalibrationTestSplit && prediction.Classification == ClassificationBenignCandidate {
			group.benign = true
			group.falseBenign = group.falseBenign || unsafe
		}
	}
	if len(development) == 0 || len(test) == 0 {
		return CalibrationReport{}, errors.New("calibration requires adjudicated development and test receipt groups")
	}

	developmentPrevalence := receiptEqualMean(development, func(group *calibrationGroup) float64 {
		return float64(group.unsafe) / float64(group.count)
	})
	testBrier := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		return group.brier / float64(group.count)
	})
	testLogLoss := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		return group.logLoss / float64(group.count)
	})
	baselineBrier := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		unsafe := float64(group.unsafe)
		safe := float64(group.count - group.unsafe)
		return (unsafe*math.Pow(1-developmentPrevalence, 2) + safe*math.Pow(developmentPrevalence, 2)) / float64(group.count)
	})
	baselineLogLoss := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		loss := 0.0
		if group.unsafe > 0 {
			loss += float64(group.unsafe) * binaryLogLoss(true, developmentPrevalence)
		}
		if safe := group.count - group.unsafe; safe > 0 {
			loss += float64(safe) * binaryLogLoss(false, developmentPrevalence)
		}
		return loss / float64(group.count)
	})
	brierSkill := 0.0
	if baselineBrier > 0 {
		brierSkill = 1 - testBrier/baselineBrier
	}
	benignGroups, falseBenignGroups := 0, 0
	for _, name := range sortedCalibrationGroups(test) {
		group := test[name]
		if group.benign {
			benignGroups++
		}
		if group.falseBenign {
			falseBenignGroups++
		}
	}
	var upperBound *float64
	if benignGroups > 0 && falseBenignGroups == 0 {
		value := 1 - math.Pow(0.05, 1/float64(benignGroups))
		upperBound = &value
	}
	reasons := make([]string, 0, 4)
	if testSafe == 0 || testUnsafe == 0 {
		reasons = append(reasons, "test_missing_both_unsafe_classes")
	}
	if brierSkill <= 0 {
		reasons = append(reasons, "nonpositive_brier_skill")
	}
	if benignGroups < 5 {
		reasons = append(reasons, "fewer_than_five_benign_candidate_receipt_groups")
	}
	if falseBenignGroups != 0 {
		reasons = append(reasons, "false_benign_receipt_groups_present")
	}
	return CalibrationReport{
		Schema: CalibrationReportSchema, Model: JevModel,
		DevelopmentReceiptGroups: len(development), DevelopmentAdjudicatedEpisodes: developmentEpisodes,
		DevelopmentUnsafePrevalence: developmentPrevalence,
		TestReceiptGroups:           len(test), TestAdjudicatedEpisodes: testEpisodes,
		TestSafeLabels: testSafe, TestUnsafeLabels: testUnsafe, TestHasBothClasses: testSafe > 0 && testUnsafe > 0,
		TestBrierScore: testBrier, TestLogLoss: testLogLoss,
		BaselineBrierScore: baselineBrier, BaselineLogLoss: baselineLogLoss, BrierSkill: brierSkill,
		BenignCandidateReceiptGroups: benignGroups, FalseBenignReceiptGroups: falseBenignGroups,
		ZeroErrorOneSided95UpperBound: upperBound, ShadowGO: len(reasons) == 0, Reasons: reasons,
	}, nil
}

func validateCalibrationPrediction(prediction Prediction) error {
	if prediction.Schema != PredictionSchema || !validSHA256(prediction.EpisodeID) ||
		validateReceiptGroup(prediction.ReceiptGroup) != nil {
		return errors.New("prediction schema, episode id, or receipt group is invalid")
	}
	if prediction.Model != JevModel {
		return fmt.Errorf("prediction model %q, want %q", prediction.Model, JevModel)
	}
	if math.IsNaN(prediction.ObservationTerminal) || math.IsInf(prediction.ObservationTerminal, 0) ||
		prediction.ObservationTerminal < 0 || prediction.ObservationTerminal > 1 ||
		math.IsNaN(prediction.RepairRequired) || math.IsInf(prediction.RepairRequired, 0) ||
		prediction.RepairRequired < 0 || prediction.RepairRequired > 1 ||
		prediction.Classification != classify(prediction.ObservationTerminal, prediction.RepairRequired) ||
		prediction.InputTokens < 0 || prediction.OutputTokens < 0 {
		return errors.New("prediction scores, classification, or usage are invalid")
	}
	return nil
}

func receiptEqualMean(groups map[string]*calibrationGroup, value func(*calibrationGroup) float64) float64 {
	total := 0.0
	for _, name := range sortedCalibrationGroups(groups) {
		total += value(groups[name])
	}
	return total / float64(len(groups))
}

func sortedCalibrationGroups(groups map[string]*calibrationGroup) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func binaryLogLoss(unsafe bool, score float64) float64 {
	score = max(calibrationLogLossEpsilon, min(1-calibrationLogLossEpsilon, score))
	if unsafe {
		return -math.Log(score)
	}
	return -math.Log1p(-score)
}
