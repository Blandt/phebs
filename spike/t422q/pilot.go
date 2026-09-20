package t422q

import (
	"bufio"
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

const (
	PredictionSchema    = "t422q-jev-shadow-prediction-v1"
	JevQuestionContract = "t422q-jev-questions-v1"
	maxEpisodes         = 4096
	maxEpisodeBytes     = 16 << 10
)

type Prediction struct {
	Schema              string  `json:"schema"`
	EpisodeID           string  `json:"episode_id"`
	ReceiptGroup        string  `json:"receipt_group"`
	Model               string  `json:"model"`
	QuestionContract    string  `json:"question_contract"`
	ObservationTerminal float64 `json:"observation_terminal"`
	RepairRequired      float64 `json:"repair_required"`
	Classification      string  `json:"classification"`
	InputTokens         int64   `json:"input_tokens"`
	OutputTokens        int64   `json:"output_tokens"`
}

func DecodeEpisodes(reader io.Reader) ([]Episode, error) {
	if reader == nil {
		return nil, errors.New("episode reader is nil")
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), maxEpisodeBytes)
	seen := make(map[string]struct{})
	var episodes []Episode
	for scanner.Scan() {
		if len(episodes) == maxEpisodes {
			return nil, errors.New("episode input exceeds 4096-case bound")
		}
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			return nil, errors.New("episode input contains an empty line")
		}
		var episode Episode
		decoder := json.NewDecoder(bytes.NewReader(line))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&episode); err != nil {
			return nil, fmt.Errorf("decode episode %d: %w", len(episodes)+1, err)
		}
		if err := decoder.Decode(&struct{}{}); err != io.EOF {
			return nil, fmt.Errorf("episode %d has trailing JSON", len(episodes)+1)
		}
		if err := validateEpisode(episode); err != nil {
			return nil, fmt.Errorf("episode %d: %w", len(episodes)+1, err)
		}
		if _, duplicate := seen[episode.EpisodeID]; duplicate {
			return nil, fmt.Errorf("episode %d duplicates an episode id", len(episodes)+1)
		}
		seen[episode.EpisodeID] = struct{}{}
		episodes = append(episodes, episode)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read episodes: %w", err)
	}
	if len(episodes) == 0 {
		return nil, errors.New("episode input is empty")
	}
	return episodes, nil
}

func EncodeEpisodes(writer io.Writer, episodes []Episode) error {
	if writer == nil {
		return errors.New("episode writer is nil")
	}
	if len(episodes) == 0 || len(episodes) > maxEpisodes {
		return errors.New("episode output must contain 1 to 4096 cases")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for index, episode := range episodes {
		if err := validateEpisode(episode); err != nil {
			return fmt.Errorf("episode %d: %w", index+1, err)
		}
		if err := encoder.Encode(episode); err != nil {
			return fmt.Errorf("encode episode %d: %w", index+1, err)
		}
	}
	return nil
}

func ClassifyEpisodes(ctx context.Context, key string, episodes []Episode) ([]Prediction, error) {
	if len(episodes) == 0 || len(episodes) > maxEpisodes {
		return nil, errors.New("classification requires 1 to 4096 episodes")
	}
	if ctx == nil || key == "" {
		return nil, errors.New("classification context or Jev API key is absent")
	}
	seen := make(map[string]struct{}, len(episodes))
	for index, episode := range episodes {
		if err := validateEpisode(episode); err != nil {
			return nil, fmt.Errorf("episode %d: %w", index+1, err)
		}
		if _, duplicate := seen[episode.EpisodeID]; duplicate {
			return nil, fmt.Errorf("episode %d duplicates an episode id", index+1)
		}
		seen[episode.EpisodeID] = struct{}{}
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	predictions := make([]Prediction, 0, len(episodes))
	for index, episode := range episodes {
		requestContext, cancel := context.WithTimeout(ctx, 30*time.Second)
		result, err := evaluate(requestContext, client, jevEndpoint, key, episode.ModelInput)
		cancel()
		if err != nil {
			return nil, fmt.Errorf("classify episode %d: %w", index+1, err)
		}
		predictions = append(predictions, Prediction{
			Schema:              PredictionSchema,
			EpisodeID:           episode.EpisodeID,
			ReceiptGroup:        episode.Provenance.ReceiptGroup,
			Model:               result.Model,
			QuestionContract:    JevQuestionContract,
			ObservationTerminal: result.ObservationTerminal,
			RepairRequired:      result.RepairRequired,
			Classification:      result.Classification,
			InputTokens:         result.InputTokens,
			OutputTokens:        result.OutputTokens,
		})
	}
	return predictions, nil
}

func EncodePredictions(writer io.Writer, predictions []Prediction) error {
	if writer == nil {
		return errors.New("prediction writer is nil")
	}
	if len(predictions) == 0 || len(predictions) > maxEpisodes {
		return errors.New("prediction output must contain 1 to 4096 rows")
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for index, prediction := range predictions {
		if err := validateCalibrationPrediction(prediction); err != nil {
			return fmt.Errorf("prediction %d: %w", index+1, err)
		}
		if err := encoder.Encode(prediction); err != nil {
			return fmt.Errorf("encode prediction %d: %w", index+1, err)
		}
	}
	return nil
}

func validateEpisode(episode Episode) error {
	if episode.Schema != EpisodeSchema {
		return errors.New("episode schema is not supported")
	}
	if !validSHA256(episode.EpisodeID) {
		return errors.New("episode id is not canonical sha256")
	}
	wantID, err := canonicalEpisodeID(episode)
	if err != nil || episode.EpisodeID != wantID {
		return errors.New("episode id does not bind its canonical content")
	}
	if err := validateReceiptGroup(episode.Provenance.ReceiptGroup); err != nil {
		return err
	}
	if episode.Provenance.ReceiptSchema == "" || !validDate(episode.Provenance.MeasuredOn) ||
		episode.Provenance.WaitOrdinal < 0 ||
		episode.Provenance.EpisodeOrdinal < 0 {
		return errors.New("episode provenance is incomplete")
	}
	if err := validateShadowState(episode.ModelInput); err != nil {
		return err
	}
	if episode.Facts.Occurrences <= 0 || episode.Facts.PendingObservations < 0 ||
		episode.Facts.SameStagePending < 0 || episode.Facts.SameStagePending > episode.Facts.PendingObservations ||
		episode.Facts.DurationBucket == "" || episode.Facts.EndReason == "" {
		return errors.New("episode facts are incomplete or outside their bounds")
	}
	return nil
}

func validateShadowState(state ShadowState) error {
	profiles := []string{"structural-2m-v1", "semantic-262144-v1"}
	waitLabels := []string{
		"cold", "warm-noop", "delta-b", "return-a", "interruption-restart",
		"stale-worker", "pressure", "archive-restore", "collection",
		"authorized-query-semantic", "authorized-query-structural",
		"interruption-backup", "pressure-recovery",
	}
	revisions := []string{"a", "b", "a-return"}
	stages := []string{
		"", "repository_visibility", "repository_index", "source_generation",
		"search_generation", "observation_publication", "extraction_publication",
		"relationship_publication", "service_census", "caller_generation", "complete",
	}
	classes := []string{"transport", "status", "response", "control", "terminal"}
	if state.Schema != StateSchema || !slices.Contains(profiles, state.Profile) ||
		!slices.Contains(waitLabels, state.WaitLabel) || !slices.Contains(revisions, state.Revision) ||
		!slices.Contains(stages, state.Stage) || !slices.Contains(classes, state.Class) ||
		state.DeadlineMS <= 0 || state.ElapsedMS < 0 || state.ProgressChanges < 0 ||
		state.ElapsedMS > state.DeadlineMS {
		return errors.New("episode model input is incomplete or outside its closed bounds")
	}
	if state.Stage == "" && (state.Class != "status" || state.HTTPStatus != 409 ||
		state.HTTPReason != "409_stale" || state.RelationshipFailureClass != "" || state.ElapsedMS != 0) {
		return errors.New("stage-free model input is not the closed retry-conflict aggregate")
	}
	if state.Class == "status" {
		if !validHTTPDiagnostic(state.HTTPStatus, state.HTTPReason) {
			return errors.New("episode model input HTTP diagnostic is invalid")
		}
	} else if state.HTTPStatus != 0 || state.HTTPReason != "" {
		return errors.New("non-status model input contains an HTTP diagnostic")
	}
	if state.RelationshipFailureClass != "" &&
		(state.Stage != "relationship_publication" || !slices.Contains([]string{
			"current_control", "authority_incomplete", "authority_mismatch",
			"successor_absent", "successor_settled_without_current",
		}, state.RelationshipFailureClass)) {
		return errors.New("episode relationship failure class is invalid")
	}
	return nil
}

func validHTTPDiagnostic(status int, reason string) bool {
	want := map[string]int{
		"409_stale": 409, "409_control_absent": 409,
		"500_store": 500, "500_projection": 500,
		"500_projection_control": 500, "500_projection_publication": 500,
		"500_projection_planning": 500, "500_projection_schedule": 500,
		"500_projection_response": 500, "401_unauthorized": 401,
		"403_forbidden": 403, "404_not_found": 404, "503_unavailable": 503,
	}
	if reason == "status_other" {
		return status >= 100 && status <= 599
	}
	return want[reason] == status && status != 0
}

func validDate(value string) bool {
	parsed, err := time.Parse("2006-01-02", value)
	return err == nil && parsed.Format("2006-01-02") == value
}

func validSHA256(value string) bool {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+64 {
		return false
	}
	decoded, err := hex.DecodeString(value[len(prefix):])
	return err == nil && len(decoded) == 32 && prefix+hex.EncodeToString(decoded) == value
}
