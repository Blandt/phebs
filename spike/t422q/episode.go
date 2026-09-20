// Package t422q projects authenticated ceremony receipts into source-free
// shadow-classification episodes. It is not part of ceremony execution.
package t422q

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/bmeddeb/phebs/spike/t4013"
)

const (
	EpisodeSchema = "t422q-episode-v1"
	StateSchema   = "phebs-ceremony-shadow-state-v1"
)

type Episode struct {
	Schema     string            `json:"schema"`
	EpisodeID  string            `json:"episode_id"`
	Provenance Provenance        `json:"provenance"`
	ModelInput ShadowState       `json:"model_input"`
	Facts      EpisodeFacts      `json:"episode_facts"`
	Resolution EpisodeResolution `json:"resolution"`
}

type Provenance struct {
	ReceiptGroup   string `json:"receipt_group"`
	ReceiptSchema  string `json:"receipt_schema"`
	MeasuredOn     string `json:"measured_on"`
	WaitOrdinal    int    `json:"wait_ordinal"`
	EpisodeOrdinal int    `json:"episode_ordinal"`
}

// ShadowState is the entire value sent to Jev. Receipt identities, digests,
// final outcomes, decisions, labels, and raw evidence are deliberately absent.
type ShadowState struct {
	Schema                   string `json:"schema"`
	Profile                  string `json:"profile"`
	WaitLabel                string `json:"wait_label"`
	Revision                 string `json:"revision"`
	Stage                    string `json:"stage,omitempty"`
	Class                    string `json:"class"`
	HTTPStatus               int    `json:"http_status,omitempty"`
	HTTPReason               string `json:"http_reason,omitempty"`
	RelationshipFailureClass string `json:"relationship_failure_class,omitempty"`
	ProgressChanges          int64  `json:"progress_changes,omitempty"`
	ElapsedMS                int64  `json:"elapsed_ms,omitempty"`
	DeadlineMS               int64  `json:"deadline_ms"`
}

type EpisodeFacts struct {
	Occurrences         int64  `json:"occurrences"`
	PendingObservations int64  `json:"pending_observations"`
	SameStagePending    int64  `json:"same_stage_pending"`
	ProgressResumed     bool   `json:"progress_resumed"`
	Completed           bool   `json:"completed"`
	TerminalSeen        bool   `json:"terminal_seen"`
	DurationBucket      string `json:"duration_bucket"`
	EndReason           string `json:"end_reason"`
}

type EpisodeResolution struct {
	WaitOutcome   string `json:"wait_outcome"`
	RunOutcome    string `json:"run_outcome"`
	FailurePhase  string `json:"failure_phase,omitempty"`
	FailureClass  string `json:"failure_class,omitempty"`
	FailureCode   string `json:"failure_code,omitempty"`
	Decision      string `json:"decision"`
	Reason        string `json:"reason"`
	Substantiated bool   `json:"substantiated"`
}

type diagnosticSignature struct {
	stage                    string
	class                    string
	httpStatus               int
	httpReason               string
	relationshipFailureClass string
}

func ProjectReceipt(receiptGroup string, receipt t4013.Receipt) ([]Episode, error) {
	if err := validateReceiptGroup(receiptGroup); err != nil {
		return nil, err
	}
	resolution := resolutionForReceipt(receipt)
	var episodes []Episode
	for waitIndex, wait := range receipt.ConvergenceWaits {
		projected := projectWait(receiptGroup, receipt.Schema, receipt.MeasuredOn, waitIndex, wait, resolution)
		episodes = append(episodes, projected...)
	}
	for index := range episodes {
		identity, err := canonicalEpisodeID(episodes[index])
		if err != nil {
			return nil, err
		}
		episodes[index].EpisodeID = identity
	}
	return episodes, nil
}

func projectWait(
	receiptGroup string,
	receiptSchema string,
	measuredOn string,
	waitIndex int,
	wait t4013.ConvergenceWaitObservation,
	resolution EpisodeResolution,
) []Episode {
	pendingObservations := int64(0)
	for _, transition := range wait.InspectionTransitions {
		if transition.Class == "pending" {
			pendingObservations++
		}
	}
	waitResolution := resolution
	waitResolution.WaitOutcome = wait.Outcome
	var episodes []Episode
	if wait.ProgressRetryConflicts > 0 {
		first := wait.ProgressRetryConflictFirstWallMS
		last := wait.ProgressRetryConflictLastWallMS
		episodes = append(episodes, makeEpisode(
			receiptGroup,
			receiptSchema,
			measuredOn,
			waitIndex,
			len(episodes),
			ShadowState{
				Schema:     StateSchema,
				Profile:    wait.Profile,
				WaitLabel:  wait.Label,
				Revision:   wait.Revision,
				Class:      "status",
				HTTPStatus: 409,
				HTTPReason: "409_stale",
				DeadlineMS: wait.DeadlineMS,
			},
			EpisodeFacts{
				Occurrences:         wait.ProgressRetryConflicts,
				PendingObservations: pendingObservations,
				ProgressResumed:     wait.ProgressChanges > 0,
				Completed:           wait.Outcome == "converged",
				TerminalSeen:        waitIsTerminal(wait),
				DurationBucket:      durationBucket(last - first),
				EndReason:           waitEndReason(wait),
			},
			waitResolution,
		))
	}

	for index := 0; index < len(wait.InspectionTransitions); {
		transition := wait.InspectionTransitions[index]
		if !isDiagnostic(transition) || isSummarizedRetryConflict(wait, transition) {
			index++
			continue
		}
		signature := signatureOf(transition)
		occurrences := int64(1)
		sameStagePending := int64(0)
		progressResumed := false
		previousProgress := transition.ProgressSHA256
		lastWallMS := transition.WallMS
		nextIndex := index + 1
		for ; nextIndex < len(wait.InspectionTransitions); nextIndex++ {
			next := wait.InspectionTransitions[nextIndex]
			if next.Class == "pending" && next.Stage == signature.stage {
				sameStagePending++
				progressResumed = progressResumed || progressChanged(previousProgress, next.ProgressSHA256)
				previousProgress = next.ProgressSHA256
				lastWallMS = next.WallMS
				continue
			}
			if signatureOf(next) == signature {
				occurrences++
				progressResumed = progressResumed || progressChanged(previousProgress, next.ProgressSHA256)
				previousProgress = next.ProgressSHA256
				lastWallMS = next.WallMS
				continue
			}
			break
		}
		endReason := transitionBoundaryReason(wait, nextIndex, signature.stage)
		episodes = append(episodes, makeEpisode(
			receiptGroup,
			receiptSchema,
			measuredOn,
			waitIndex,
			len(episodes),
			ShadowState{
				Schema:                   StateSchema,
				Profile:                  wait.Profile,
				WaitLabel:                wait.Label,
				Revision:                 wait.Revision,
				Stage:                    transition.Stage,
				Class:                    transition.Class,
				HTTPStatus:               transition.HTTPStatus,
				HTTPReason:               transition.HTTPReason,
				RelationshipFailureClass: transition.RelationshipFailureClass,
				ProgressChanges:          transition.ProgressChanges,
				ElapsedMS:                transition.WallMS,
				DeadlineMS:               wait.DeadlineMS,
			},
			EpisodeFacts{
				Occurrences:         occurrences,
				PendingObservations: pendingObservations,
				SameStagePending:    sameStagePending,
				ProgressResumed:     progressResumed,
				Completed:           endReason == "recovered",
				TerminalSeen:        transition.Class == "terminal" || waitIsTerminal(wait),
				DurationBucket:      durationBucket(lastWallMS - transition.WallMS),
				EndReason:           endReason,
			},
			waitResolution,
		))
		index = nextIndex
	}
	return episodes
}

func makeEpisode(
	receiptGroup, receiptSchema, measuredOn string,
	waitIndex, episodeIndex int,
	state ShadowState,
	facts EpisodeFacts,
	resolution EpisodeResolution,
) Episode {
	return Episode{
		Schema: EpisodeSchema,
		Provenance: Provenance{
			ReceiptGroup:   receiptGroup,
			ReceiptSchema:  receiptSchema,
			MeasuredOn:     measuredOn,
			WaitOrdinal:    waitIndex,
			EpisodeOrdinal: episodeIndex,
		},
		ModelInput: state,
		Facts:      facts,
		Resolution: resolution,
	}
}

func canonicalEpisodeID(episode Episode) (string, error) {
	episode.EpisodeID = ""
	raw, err := json.Marshal(episode)
	if err != nil {
		return "", err
	}
	identity := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(identity[:]), nil
}

func resolutionForReceipt(receipt t4013.Receipt) EpisodeResolution {
	resolution := EpisodeResolution{
		RunOutcome:    receipt.Outcome,
		Decision:      receipt.Decision.Selected,
		Reason:        receipt.Decision.Reason,
		Substantiated: receipt.Decision.Substantiated,
	}
	if len(receipt.Failures) > 0 {
		resolution.FailurePhase = receipt.Failures[0].Phase
		resolution.FailureClass = receipt.Failures[0].Class
		resolution.FailureCode = receipt.Failures[0].Code
	}
	return resolution
}

func validateReceiptGroup(value string) error {
	if len(value) == 0 || len(value) > 64 {
		return errors.New("receipt group must contain 1 to 64 closed characters")
	}
	for _, valueRune := range value {
		if valueRune != '-' && valueRune != '_' && (valueRune < 'a' || valueRune > 'z') &&
			(valueRune < '0' || valueRune > '9') {
			return errors.New("receipt group must contain only lowercase letters, digits, hyphens, or underscores")
		}
	}
	return nil
}

func signatureOf(value t4013.ConvergenceTransitionObservation) diagnosticSignature {
	return diagnosticSignature{
		stage:                    value.Stage,
		class:                    value.Class,
		httpStatus:               value.HTTPStatus,
		httpReason:               value.HTTPReason,
		relationshipFailureClass: value.RelationshipFailureClass,
	}
}

func isDiagnostic(value t4013.ConvergenceTransitionObservation) bool {
	return value.Class != "" && value.Class != "pending" && value.Class != "complete"
}

func isSummarizedRetryConflict(
	wait t4013.ConvergenceWaitObservation,
	value t4013.ConvergenceTransitionObservation,
) bool {
	return wait.ProgressRetryConflicts > 0 && progressRetryConflictStage(value.Stage) && value.Class == "status" &&
		value.HTTPStatus == 409 && value.HTTPReason == "409_stale"
}

func progressRetryConflictStage(stage string) bool {
	return stage == "observation_publication" || stage == "extraction_publication" ||
		stage == "caller_generation"
}

func progressChanged(previous, current string) bool {
	return previous != "" && current != "" && previous != current
}

func transitionBoundaryReason(
	wait t4013.ConvergenceWaitObservation,
	nextIndex int,
	stage string,
) string {
	if nextIndex >= len(wait.InspectionTransitions) {
		return waitEndReason(wait)
	}
	next := wait.InspectionTransitions[nextIndex]
	switch {
	case next.Class == "complete":
		return "recovered"
	case next.Class == "terminal":
		return "terminal"
	case next.Stage != stage:
		return "stage_change"
	default:
		return "signature_change"
	}
}

func waitEndReason(wait t4013.ConvergenceWaitObservation) string {
	if wait.Outcome == "converged" || wait.LastStage == "complete" {
		return "recovered"
	}
	return "wait_end"
}

func waitIsTerminal(wait t4013.ConvergenceWaitObservation) bool {
	return strings.HasSuffix(wait.Outcome, "_terminal") ||
		strings.HasSuffix(wait.Outcome, "_bound_refusal")
}

func durationBucket(durationMS int64) string {
	switch {
	case durationMS < 1_000:
		return "lt_1s"
	case durationMS < 10_000:
		return "lt_10s"
	case durationMS < 60_000:
		return "lt_60s"
	case durationMS < 240_000:
		return "lt_240s"
	case durationMS < 300_000:
		return "lt_300s"
	default:
		return "ge_300s"
	}
}
