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
	Schema                                   string   `json:"schema"`
	Model                                    string   `json:"model"`
	QuestionContract                         string   `json:"question_contract"`
	DevelopmentReceiptGroups                 int      `json:"development_receipt_groups"`
	DevelopmentEpisodes                      int      `json:"development_episodes"`
	DevelopmentAdjudicatedEpisodes           int      `json:"development_adjudicated_episodes"`
	DevelopmentAbstainedEpisodes             int      `json:"development_abstained_episodes"`
	DevelopmentObservationTerminalPrevalence float64  `json:"development_observation_terminal_prevalence"`
	DevelopmentRepairRequiredPrevalence      float64  `json:"development_repair_required_prevalence"`
	TestReceiptGroups                        int      `json:"test_receipt_groups"`
	TestEpisodes                             int      `json:"test_episodes"`
	TestAdjudicatedEpisodes                  int      `json:"test_adjudicated_episodes"`
	TestAbstainedEpisodes                    int      `json:"test_abstained_episodes"`
	TestSafeLabels                           int      `json:"test_safe_labels"`
	TestUnsafeLabels                         int      `json:"test_unsafe_labels"`
	TestObservationTerminalFalseLabels       int      `json:"test_observation_terminal_false_labels"`
	TestObservationTerminalTrueLabels        int      `json:"test_observation_terminal_true_labels"`
	TestRepairRequiredFalseLabels            int      `json:"test_repair_required_false_labels"`
	TestRepairRequiredTrueLabels             int      `json:"test_repair_required_true_labels"`
	TestObservationTerminalBrierScore        float64  `json:"test_observation_terminal_brier_score"`
	TestObservationTerminalLogLoss           float64  `json:"test_observation_terminal_log_loss"`
	TestRepairRequiredBrierScore             float64  `json:"test_repair_required_brier_score"`
	TestRepairRequiredLogLoss                float64  `json:"test_repair_required_log_loss"`
	BaselineObservationTerminalBrierScore    float64  `json:"baseline_observation_terminal_brier_score"`
	BaselineObservationTerminalLogLoss       float64  `json:"baseline_observation_terminal_log_loss"`
	BaselineRepairRequiredBrierScore         float64  `json:"baseline_repair_required_brier_score"`
	BaselineRepairRequiredLogLoss            float64  `json:"baseline_repair_required_log_loss"`
	ObservationTerminalBrierSkill            float64  `json:"observation_terminal_brier_skill"`
	RepairRequiredBrierSkill                 float64  `json:"repair_required_brier_skill"`
	BenignCandidateReceiptGroups             int      `json:"benign_candidate_receipt_groups"`
	FalseBenignReceiptGroups                 int      `json:"false_benign_receipt_groups"`
	ConditionalZeroErrorOneSided95UpperBound *float64 `json:"conditional_zero_error_one_sided_95_upper_bound"`
	ShadowGO                                 bool     `json:"shadow_go"`
	Reasons                                  []string `json:"reasons"`
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
	QuestionContract    string   `json:"question_contract"`
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
			QuestionContract:    wire.QuestionContract,
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

type calibrationAxis struct {
	positive       int
	brier, logLoss float64
}

type calibrationGroup struct {
	total, adjudicated, abstained int
	unsafe                        int
	terminal, repair              calibrationAxis
	benign, falseBenign           bool
}

func EvaluateCalibration(episodes []Episode, predictions []Prediction, labels []HumanLabel) (CalibrationReport, error) {
	if len(episodes) == 0 || len(episodes) > maxEpisodes || len(predictions) == 0 || len(predictions) > maxEpisodes ||
		len(labels) == 0 || len(labels) > maxEpisodes {
		return CalibrationReport{}, errors.New("calibration inputs must each contain 1 to 4096 rows")
	}
	episodeByID := make(map[string]Episode, len(episodes))
	receiptDate := make(map[string]string)
	for index, episode := range episodes {
		if err := validateEpisode(episode); err != nil {
			return CalibrationReport{}, fmt.Errorf("episode %d: %w", index+1, err)
		}
		if _, duplicate := episodeByID[episode.EpisodeID]; duplicate {
			return CalibrationReport{}, fmt.Errorf("episode %d duplicates an episode id", index+1)
		}
		if measuredOn, exists := receiptDate[episode.Provenance.ReceiptGroup]; exists && measuredOn != episode.Provenance.MeasuredOn {
			return CalibrationReport{}, fmt.Errorf("receipt group %q crosses measured dates", episode.Provenance.ReceiptGroup)
		}
		receiptDate[episode.Provenance.ReceiptGroup] = episode.Provenance.MeasuredOn
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
	latestDevelopment, earliestTest := "", ""
	for receiptGroup, split := range receiptSplit {
		measuredOn := receiptDate[receiptGroup]
		if split == CalibrationDevelopmentSplit && measuredOn > latestDevelopment {
			latestDevelopment = measuredOn
		}
		if split == CalibrationTestSplit && (earliestTest == "" || measuredOn < earliestTest) {
			earliestTest = measuredOn
		}
	}
	if latestDevelopment == "" || earliestTest == "" {
		return CalibrationReport{}, errors.New("calibration requires development and test receipt groups")
	}
	if latestDevelopment >= earliestTest {
		return CalibrationReport{}, errors.New("calibration development dates must precede every test date")
	}

	development := make(map[string]*calibrationGroup)
	test := make(map[string]*calibrationGroup)
	developmentEpisodes, developmentAdjudicated, developmentAbstained := 0, 0, 0
	testEpisodes, testAdjudicated, testAbstained, testSafe, testUnsafe := 0, 0, 0, 0, 0
	testTerminalTrue, testTerminalFalse, testRepairTrue, testRepairFalse := 0, 0, 0, 0
	for _, episode := range episodes {
		prediction, predicted := predictionByID[episode.EpisodeID]
		label, labelled := labelByID[episode.EpisodeID]
		if !predicted || !labelled {
			return CalibrationReport{}, errors.New("episode, prediction, and human-label sets differ")
		}
		groups := development
		if label.Split == CalibrationTestSplit {
			groups = test
			testEpisodes++
		} else {
			developmentEpisodes++
		}
		group := groups[episode.Provenance.ReceiptGroup]
		if group == nil {
			group = &calibrationGroup{}
			groups[episode.Provenance.ReceiptGroup] = group
		}
		group.total++
		if label.ObservationTerminal == nil {
			group.abstained++
			if label.Split == CalibrationTestSplit {
				testAbstained++
			} else {
				developmentAbstained++
			}
			continue
		}
		group.adjudicated++
		terminal, repair := *label.ObservationTerminal, *label.RepairRequired
		unsafe := terminal || repair
		addCalibrationAxis(&group.terminal, terminal, prediction.ObservationTerminal)
		addCalibrationAxis(&group.repair, repair, prediction.RepairRequired)
		if unsafe {
			group.unsafe++
		}
		if label.Split == CalibrationTestSplit {
			testAdjudicated++
			if unsafe {
				testUnsafe++
			} else {
				testSafe++
			}
			if terminal {
				testTerminalTrue++
			} else {
				testTerminalFalse++
			}
			if repair {
				testRepairTrue++
			} else {
				testRepairFalse++
			}
			if prediction.Classification == ClassificationBenignCandidate {
				group.benign = true
				group.falseBenign = group.falseBenign || unsafe
			}
		} else {
			developmentAdjudicated++
		}
	}
	if len(development) == 0 || len(test) == 0 || adjudicatedCalibrationGroups(development) == 0 ||
		adjudicatedCalibrationGroups(test) == 0 {
		return CalibrationReport{}, errors.New("calibration requires adjudicated development and test receipt groups")
	}

	developmentTerminalPrevalence := receiptEqualMean(development, func(group *calibrationGroup) float64 {
		return float64(group.terminal.positive) / float64(group.adjudicated)
	})
	developmentRepairPrevalence := receiptEqualMean(development, func(group *calibrationGroup) float64 {
		return float64(group.repair.positive) / float64(group.adjudicated)
	})
	testTerminalBrier := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		return group.terminal.brier / float64(group.adjudicated)
	})
	testTerminalLogLoss := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		return group.terminal.logLoss / float64(group.adjudicated)
	})
	testRepairBrier := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		return group.repair.brier / float64(group.adjudicated)
	})
	testRepairLogLoss := receiptEqualMean(test, func(group *calibrationGroup) float64 {
		return group.repair.logLoss / float64(group.adjudicated)
	})
	baselineTerminalBrier, baselineTerminalLogLoss := baselineScores(test, developmentTerminalPrevalence, func(group *calibrationGroup) int {
		return group.terminal.positive
	})
	baselineRepairBrier, baselineRepairLogLoss := baselineScores(test, developmentRepairPrevalence, func(group *calibrationGroup) int {
		return group.repair.positive
	})
	terminalSkill := brierSkill(testTerminalBrier, baselineTerminalBrier)
	repairSkill := brierSkill(testRepairBrier, baselineRepairBrier)
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
	reasons := make([]string, 0, 8)
	if developmentAbstained != 0 {
		reasons = append(reasons, "development_abstentions_present")
	}
	if testAbstained != 0 {
		reasons = append(reasons, "test_abstentions_present")
	}
	if testTerminalFalse == 0 || testTerminalTrue == 0 {
		reasons = append(reasons, "test_observation_terminal_missing_both_classes")
	}
	if testRepairFalse == 0 || testRepairTrue == 0 {
		reasons = append(reasons, "test_repair_required_missing_both_classes")
	}
	if terminalSkill <= 0 {
		reasons = append(reasons, "nonpositive_observation_terminal_brier_skill")
	}
	if repairSkill <= 0 {
		reasons = append(reasons, "nonpositive_repair_required_brier_skill")
	}
	if benignGroups < 5 {
		reasons = append(reasons, "fewer_than_five_benign_candidate_receipt_groups")
	}
	if falseBenignGroups != 0 {
		reasons = append(reasons, "false_benign_receipt_groups_present")
	}
	return CalibrationReport{
		Schema: CalibrationReportSchema, Model: JevModel, QuestionContract: JevQuestionContract,
		DevelopmentReceiptGroups: len(development), DevelopmentEpisodes: developmentEpisodes,
		DevelopmentAdjudicatedEpisodes: developmentAdjudicated, DevelopmentAbstainedEpisodes: developmentAbstained,
		DevelopmentObservationTerminalPrevalence: developmentTerminalPrevalence,
		DevelopmentRepairRequiredPrevalence:      developmentRepairPrevalence,
		TestReceiptGroups:                        len(test), TestEpisodes: testEpisodes,
		TestAdjudicatedEpisodes: testAdjudicated, TestAbstainedEpisodes: testAbstained,
		TestSafeLabels: testSafe, TestUnsafeLabels: testUnsafe,
		TestObservationTerminalFalseLabels: testTerminalFalse, TestObservationTerminalTrueLabels: testTerminalTrue,
		TestRepairRequiredFalseLabels: testRepairFalse, TestRepairRequiredTrueLabels: testRepairTrue,
		TestObservationTerminalBrierScore: testTerminalBrier, TestObservationTerminalLogLoss: testTerminalLogLoss,
		TestRepairRequiredBrierScore: testRepairBrier, TestRepairRequiredLogLoss: testRepairLogLoss,
		BaselineObservationTerminalBrierScore: baselineTerminalBrier,
		BaselineObservationTerminalLogLoss:    baselineTerminalLogLoss,
		BaselineRepairRequiredBrierScore:      baselineRepairBrier,
		BaselineRepairRequiredLogLoss:         baselineRepairLogLoss,
		ObservationTerminalBrierSkill:         terminalSkill, RepairRequiredBrierSkill: repairSkill,
		BenignCandidateReceiptGroups: benignGroups, FalseBenignReceiptGroups: falseBenignGroups,
		ConditionalZeroErrorOneSided95UpperBound: upperBound, ShadowGO: len(reasons) == 0, Reasons: reasons,
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
	if prediction.QuestionContract != JevQuestionContract {
		return fmt.Errorf("prediction question contract %q, want %q", prediction.QuestionContract, JevQuestionContract)
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

func addCalibrationAxis(axis *calibrationAxis, positive bool, score float64) {
	if positive {
		axis.positive++
		delta := 1 - score
		axis.brier += delta * delta
	} else {
		axis.brier += score * score
	}
	axis.logLoss += binaryLogLoss(positive, score)
}

func adjudicatedCalibrationGroups(groups map[string]*calibrationGroup) int {
	count := 0
	for _, group := range groups {
		if group.adjudicated > 0 {
			count++
		}
	}
	return count
}

func baselineScores(
	groups map[string]*calibrationGroup,
	prevalence float64,
	positives func(*calibrationGroup) int,
) (float64, float64) {
	brier := receiptEqualMean(groups, func(group *calibrationGroup) float64 {
		positive := positives(group)
		negative := group.adjudicated - positive
		negativeDelta := 1 - prevalence
		return (float64(positive)*negativeDelta*negativeDelta +
			float64(negative)*prevalence*prevalence) / float64(group.adjudicated)
	})
	logLoss := receiptEqualMean(groups, func(group *calibrationGroup) float64 {
		positive := positives(group)
		negative := group.adjudicated - positive
		return (float64(positive)*binaryLogLoss(true, prevalence) +
			float64(negative)*binaryLogLoss(false, prevalence)) / float64(group.adjudicated)
	})
	return brier, logLoss
}

func brierSkill(score, baseline float64) float64 {
	if baseline == 0 {
		return 0
	}
	return 1 - score/baseline
}

func receiptEqualMean(groups map[string]*calibrationGroup, value func(*calibrationGroup) float64) float64 {
	total, count := 0.0, 0
	for _, name := range sortedCalibrationGroups(groups) {
		if groups[name].adjudicated == 0 {
			continue
		}
		total += value(groups[name])
		count++
	}
	return total / float64(count)
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
