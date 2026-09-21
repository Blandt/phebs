package t422q

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
)

const (
	JevModel = "jev-1.13.0"

	ClassificationBenignCandidate   = "benign_candidate"
	ClassificationTerminalCandidate = "terminal_candidate"
	ClassificationRepairCandidate   = "repair_candidate"
	ClassificationReview            = "review"

	jevEndpoint         = "https://api.typesafe.ai/v1/systemone"
	maxJevRequestBytes  = 64 << 10
	maxJevResponseBytes = 4 << 10
)

type Result struct {
	Model               string
	ObservationTerminal float64
	RepairRequired      float64
	Classification      string
	InputTokens         int64
	OutputTokens        int64
}

type jevRequest struct {
	State     ShadowState  `json:"state"`
	Model     string       `json:"model"`
	Questions jevQuestions `json:"questions"`
}

type jevQuestions struct {
	ObservationTerminal jevQuestion `json:"observation_terminal"`
	RepairRequired      jevQuestion `json:"repair_required"`
}

type jevQuestion struct {
	Type         string      `json:"type"`
	Instructions string      `json:"instructions"`
	Criteria     jevCriteria `json:"criteria"`
}

type jevCriteria struct {
	True  string `json:"true"`
	False string `json:"false"`
}

type jevResponse struct {
	Model   string      `json:"model"`
	Answers *jevAnswers `json:"answers"`
	Usage   *jevUsage   `json:"usage"`
}

type jevAnswers struct {
	ObservationTerminal *jevAnswer `json:"observation_terminal"`
	RepairRequired      *jevAnswer `json:"repair_required"`
}

type jevAnswer struct {
	Type string   `json:"type"`
	Noul *float64 `json:"noul"`
}

type jevUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

func Evaluate(ctx context.Context, key string, state ShadowState) (Result, error) {
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	return evaluate(ctx, client, jevEndpoint, key, state)
}

func evaluate(ctx context.Context, client *http.Client, endpoint, key string, state ShadowState) (Result, error) {
	if client == nil {
		return Result{}, errors.New("jev HTTP client is nil")
	}
	if key == "" {
		return Result{}, errors.New("jev API key is empty")
	}
	if err := validateShadowState(state); err != nil {
		return Result{}, fmt.Errorf("refuse Jev state: %w", err)
	}
	raw, err := json.Marshal(newJevRequest(state))
	if err != nil {
		return Result{}, fmt.Errorf("encode Jev request: %w", err)
	}
	if len(raw) > maxJevRequestBytes {
		return Result{}, errors.New("jev request exceeds byte limit")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return Result{}, fmt.Errorf("create Jev request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Content-Type", "application/json")

	response, err := client.Do(request)
	if err != nil {
		return Result{}, fmt.Errorf("send Jev request: %w", err)
	}
	responseRaw, readErr := io.ReadAll(io.LimitReader(response.Body, maxJevResponseBytes+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		return Result{}, fmt.Errorf("read Jev response: %w", readErr)
	}
	if closeErr != nil {
		return Result{}, fmt.Errorf("close Jev response: %w", closeErr)
	}
	if len(responseRaw) > maxJevResponseBytes {
		return Result{}, errors.New("jev response exceeds byte limit")
	}
	if response.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("jev response status %d", response.StatusCode)
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return Result{}, errors.New("jev response content type is not application/json")
	}

	var decoded jevResponse
	decoder := json.NewDecoder(bytes.NewReader(responseRaw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&decoded); err != nil {
		return Result{}, fmt.Errorf("decode Jev response: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Result{}, errors.New("jev response has trailing JSON")
	}
	if err := validateJevResponse(decoded); err != nil {
		return Result{}, err
	}

	terminal := *decoded.Answers.ObservationTerminal.Noul
	repair := *decoded.Answers.RepairRequired.Noul
	return Result{
		Model:               decoded.Model,
		ObservationTerminal: terminal,
		RepairRequired:      repair,
		Classification:      classify(terminal, repair),
		InputTokens:         decoded.Usage.InputTokens,
		OutputTokens:        decoded.Usage.OutputTokens,
	}, nil
}

func newJevRequest(state ShadowState) jevRequest {
	return jevRequest{
		State: state,
		Model: JevModel,
		Questions: jevQuestions{
			ObservationTerminal: jevQuestion{
				Type:         "noul",
				Instructions: "Does this source-free decision-point state establish a real terminal ceremony observation?",
				Criteria: jevCriteria{
					True:  "The current observation establishes that the unchanged ceremony must stop because safe bounded progress is no longer possible.",
					False: "The current observation does not establish a terminal stop; unchanged bounded progress or review remains possible.",
				},
			},
			RepairRequired: jevQuestion{
				Type:         "noul",
				Instructions: "Does this source-free decision-point state establish that repair is required before the ceremony can safely progress?",
				Criteria: jevCriteria{
					True:  "Human, code, configuration, environment, or custody repair is required before safe progress can resume.",
					False: "No repair requirement is established; unchanged bounded progress or review remains possible.",
				},
			},
		},
	}
}

func validateJevResponse(response jevResponse) error {
	if response.Model != JevModel {
		return fmt.Errorf("jev response model %q, want %q", response.Model, JevModel)
	}
	if response.Answers == nil || response.Answers.ObservationTerminal == nil || response.Answers.RepairRequired == nil {
		return errors.New("jev response does not contain both answers")
	}
	if err := validateJevAnswer("observation_terminal", response.Answers.ObservationTerminal); err != nil {
		return err
	}
	if err := validateJevAnswer("repair_required", response.Answers.RepairRequired); err != nil {
		return err
	}
	if response.Usage == nil || response.Usage.InputTokens < 0 || response.Usage.OutputTokens < 0 {
		return errors.New("jev response usage is invalid")
	}
	return nil
}

func validateJevAnswer(name string, answer *jevAnswer) error {
	if answer.Type != "noul" || answer.Noul == nil || math.IsNaN(*answer.Noul) || math.IsInf(*answer.Noul, 0) || *answer.Noul < 0 || *answer.Noul > 1 {
		return fmt.Errorf("jev %s answer is invalid", name)
	}
	return nil
}

func classify(observationTerminal, repairRequired float64) string {
	switch {
	case observationTerminal <= 0.10 && repairRequired <= 0.10:
		return ClassificationBenignCandidate
	case observationTerminal >= 0.90:
		return ClassificationTerminalCandidate
	case repairRequired >= 0.90:
		return ClassificationRepairCandidate
	default:
		return ClassificationReview
	}
}
