package t421

import (
	"errors"
	"reflect"
	"slices"
	"strings"
)

var errExecutionReceiptAuthority = errors.New("execution receipt authority is incomplete")

// executionReceiptAuthorityInventory is the canonical source-free authority
// projection retained by Receipt. Repeated phase states and extraction roots
// are stored once and referenced by digest.
type executionReceiptAuthorityInventory struct {
	ExtractionRoots []ExtractionRootSnapshot
	Snapshots       []AuthoritySnapshot
	Results         []AuthorityPhaseReference
}

// composeExecutionReceiptAuthorityInventory converts actual phase authorities
// into the canonical deduplicated receipt representation. It never creates an
// authority value: every snapshot and reference comes from the supplied joined
// observations.
func composeExecutionReceiptAuthorityInventory(
	plan Plan,
	values []AuthorityPhaseResult,
) (executionReceiptAuthorityInventory, error) {
	if plan.Schema != PlanV3Schema || validatePlan(plan, &plan.Revisions) != nil ||
		!slices.Equal(plan.PhaseOrder, frozenPhaseOrder()) {
		return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
	}
	phases := plan.PhaseOrder[1 : len(plan.PhaseOrder)-1]
	if len(values) != len(phases) {
		return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
	}

	rootByDigest := make(map[string]ExtractionRootSnapshot, len(values))
	snapshotByDigest := make(map[string]AuthoritySnapshot, len(values))
	result := executionReceiptAuthorityInventory{Results: make([]AuthorityPhaseReference, len(values))}
	outcomes := make(map[string]string, len(values))
	stopped := false
	cloned := make([]AuthorityPhaseResult, len(values))
	for index, value := range values {
		if value.Phase != phases[index] || !validExecutionAuthorityOutcome(value, stopped) {
			return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
		}
		if value.Outcome == "stopped" {
			stopped = true
		}
		outcomes[value.Phase] = value.Outcome
		result.Results[index] = AuthorityPhaseReference{Phase: value.Phase, Outcome: value.Outcome}
		if value.Outcome == "not_run" {
			cloned[index] = value
			continue
		}

		value = cloneExecutionAuthorityResult(value)
		if !validExecutionAuthorityRoots(value, rootByDigest) {
			return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
		}
		digest, err := authoritySnapshotSHA256(value.AuthorityState)
		if err != nil || !validDigest(digest) {
			return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
		}
		if prior, exists := snapshotByDigest[digest]; exists && prior.AuthorityState != value.AuthorityState {
			return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
		}
		snapshotByDigest[digest] = AuthoritySnapshot{SHA256: digest, AuthorityState: value.AuthorityState}
		result.Results[index].SnapshotSHA256 = digest
		cloned[index] = value
	}

	result.ExtractionRoots = make([]ExtractionRootSnapshot, 0, len(rootByDigest))
	for _, value := range rootByDigest {
		result.ExtractionRoots = append(result.ExtractionRoots, value)
	}
	slices.SortFunc(result.ExtractionRoots, func(left, right ExtractionRootSnapshot) int {
		return strings.Compare(left.SHA256, right.SHA256)
	})
	result.Snapshots = make([]AuthoritySnapshot, 0, len(snapshotByDigest))
	for _, value := range snapshotByDigest {
		result.Snapshots = append(result.Snapshots, value)
	}
	slices.SortFunc(result.Snapshots, func(left, right AuthoritySnapshot) int {
		return strings.Compare(left.SHA256, right.SHA256)
	})

	roundTrip, err := resolveAuthorityResults(result.ExtractionRoots, result.Snapshots, result.Results, phases, outcomes)
	if err != nil || !reflect.DeepEqual(roundTrip, cloned) {
		return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
	}
	return result, nil
}

func validExecutionAuthorityOutcome(value AuthorityPhaseResult, stopped bool) bool {
	switch value.Outcome {
	case "passed":
		return !stopped
	case "stopped":
		return !stopped
	case "not_run":
		return stopped && reflect.DeepEqual(value, AuthorityPhaseResult{Phase: value.Phase, Outcome: "not_run"})
	default:
		return false
	}
}

func validExecutionAuthorityRoots(
	value AuthorityPhaseResult,
	rootByDigest map[string]ExtractionRootSnapshot,
) bool {
	if value.ExtractionRootsSHA256 == "" {
		return value.ExtractionRoots == nil
	}
	if len(value.ExtractionRoots) == 0 || !validDigest(value.ExtractionRootsSHA256) {
		return false
	}
	digest, err := receiptSHA256(value.ExtractionRoots)
	if err != nil || digest != value.ExtractionRootsSHA256 {
		return false
	}
	snapshot := ExtractionRootSnapshot{SHA256: digest, Roots: value.ExtractionRoots}
	if prior, exists := rootByDigest[digest]; exists && !reflect.DeepEqual(prior, snapshot) {
		return false
	}
	rootByDigest[digest] = snapshot
	return true
}

func cloneExecutionAuthorityResult(value AuthorityPhaseResult) AuthorityPhaseResult {
	value.ExtractionRoots = slices.Clone(value.ExtractionRoots)
	for index := range value.ExtractionRoots {
		value.ExtractionRoots[index].PartitionResults = slices.Clone(value.ExtractionRoots[index].PartitionResults)
	}
	return value
}
