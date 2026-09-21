//go:build darwin

package t421

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

var errExecutionReceiptAssembly = errors.New("execution receipt evidence is incomplete")

// Every field is an observation supplied by its owner. Assembly supplies only
// frozen metadata, digests and the deterministic decision; it never supplies
// missing measurements, authority, attempted cleanup or a failure diagnosis.
// Both successful and stopped executions use this same complete inventory.
type executionReceiptEvidence struct {
	Phases        []PhaseResult
	Measurements  []PhaseMeasurement
	Authorities   []AuthorityPhaseResult
	States        []ExactPhaseEvidence
	Transitions   []TransitionResult
	Queries       QueryEvidence
	Relationships RelationshipEvidence
	Revisions     []RevisionResult
	Teardown      ReceiptTeardown
}

// assembleExecutionReceipt validates an unsigned candidate. A returned value
// is not an authenticated receipt: the guarded package builder must sign and
// independently authenticate it before it can cross the return firewall.
func assembleExecutionReceipt(plan Plan, binding ExecutionFreezeBinding, evidence executionReceiptEvidence) (Receipt, error) {
	if !processAccountingPlanSemantics(plan.Schema) || validatePlan(plan, &plan.Revisions) != nil {
		return Receipt{}, errExecutionReceiptAssembly
	}
	outcomes, stopped, err := validateReceiptPhases(evidence.Phases, plan)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	authorities, err := executionReceiptAuthorities(plan, outcomes, evidence.Authorities)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	if err := validateRevisionResults(evidence.Revisions, outcomes, plan); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	source, err := executionSourceVerificationBytes(plan, binding, evidence.Revisions)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	freeze := binding.freeze
	value := Receipt{
		Schema: plan.ReceiptContract.Schema,
		Authority: ReceiptAuthority{
			PlanSchema: plan.Schema, PlanSHA256: binding.planSHA256, SourceCommit: plan.SourceCommit,
			SourceVerificationSHA256: SHA256(source), ExtractionRootSnapshots: authorities.ExtractionRoots,
			Snapshots: authorities.Snapshots, Results: authorities.Results,
		},
		Environment: ReceiptEnvironment{Fields: freeze.Host},
		ExecutionFreeze: ReceiptExecutionFreeze{
			Schema: freeze.Schema, SHA256: binding.freezeSHA256, SignerFingerprint: binding.expectedSignerFingerprint,
			AdmissionEventSHA256: binding.admissionEventSHA256, AdmissionEventOrdinal: binding.admissionEventOrdinal,
			Commits: freeze.Commits,
		},
		Implementation: ReceiptImplementation{
			IntegratedMainCommit: freeze.Commits.IntegratedMainCommit, T422SourceCommit: freeze.Commits.T422SourceCommit,
			CleanTree: true, DigestAlgorithm: plan.ToolPolicy.DigestAlgorithm, Tools: slices.Clone(freeze.Tools),
		},
		Inputs: slices.Clone(plan.Inputs), Measurements: evidence.Measurements, NonClaims: receiptNonClaims(plan.Claims),
		PhaseResults: evidence.Phases, StateResults: evidence.States, TransitionResults: evidence.Transitions,
		QueryResults: evidence.Queries, RelationshipResults: evidence.Relationships,
		RevisionResults: evidence.Revisions, Teardown: evidence.Teardown, SourceFree: true,
		Seal: ReceiptSeal{
			PolicySchema: plan.SealPolicy.Schema, SignerFingerprint: binding.expectedSignerFingerprint,
			SignerNamespaceSHA256:                binding.expectedSignerNamespaceSHA256,
			FreezeSignatureNamespace:             plan.SealPolicy.FreezeSignatureNamespace,
			SourceVerificationSignatureNamespace: plan.SealPolicy.SourceVerificationSignatureNamespace,
			ReturnedSignatureNamespace:           plan.SealPolicy.ReturnedSignatureNamespace,
			VerificationPosture:                  "freeze_preflight_source_and_returned_signatures_verified_by_external_bindings",
		},
	}
	for _, item := range []struct {
		input any
		out   *string
	}{
		{plan.Profile, &value.Authority.ProfileSHA256}, {plan.Oracle, &value.Authority.OracleSHA256},
		{plan.Revisions, &value.Authority.RevisionHistorySHA256}, {plan.MeterPolicy, &value.Authority.MeterPolicySHA256},
		{freeze.Host, &value.Environment.SHA256},
	} {
		*item.out, err = receiptSHA256(item.input)
		if err != nil {
			return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
		}
	}
	if err := validateReceiptFreezeBinding(value, plan, binding); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	clean, err := validateReceiptTeardown(value.Teardown, value.Measurements, plan, freeze, hasOwnedServerStart(value.StateResults))
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	value.Decision = ReceiptDecision{Outcome: "passed", Selected: "continue", RulePriority: 3,
		Reason: "all_exact_checks_passed", Substantiated: true,
		Gate2V2: plan.Claims.Gate2V2, ReleasePosture: plan.Claims.ReleasePosture}
	if stopped != nil {
		selected, priority, err := expectedStoppedDecision(*stopped, value.Measurements, plan)
		if err != nil {
			return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
		}
		value.Decision.Outcome, value.Decision.Selected, value.Decision.RulePriority, value.Decision.Reason =
			"stopped", selected, priority, stopped.Code
	}
	if !clean {
		value.Decision.Outcome, value.Decision.Selected, value.Decision.RulePriority, value.Decision.Reason =
			"stopped", "reduce", 4, "teardown_failed"
	}
	if err := validateReceiptEvidence(value, plan, binding, SHA256(source)); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	// One bounded canonical round trip detaches every nested input. The existing
	// validator has already enforced MaximumBytes, section and source-free rules.
	raw, err := MarshalCanonical(value)
	if err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	var detached Receipt
	if err := json.Unmarshal(raw, &detached); err != nil {
		return Receipt{}, fmt.Errorf("%w: %w", errExecutionReceiptAssembly, err)
	}
	return detached, nil
}

// A typed preflight stop has no operational authority at all. Do not fabricate
// a stopped cold state just to make the operational inventory composable.
func executionReceiptAuthorities(plan Plan, outcomes map[string]string, values []AuthorityPhaseResult) (executionReceiptAuthorityInventory, error) {
	if outcomes[plan.PhaseOrder[0]] != "stopped" {
		return composeExecutionReceiptAuthorityInventory(plan, values)
	}
	phases := plan.PhaseOrder[1 : len(plan.PhaseOrder)-1]
	if len(values) != len(phases) {
		return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
	}
	result := executionReceiptAuthorityInventory{Results: make([]AuthorityPhaseReference, len(phases))}
	for index, phase := range phases {
		if values[index].Phase != phase || !validExecutionAuthorityOutcome(values[index], true) || values[index].Outcome != "not_run" {
			return executionReceiptAuthorityInventory{}, errExecutionReceiptAuthority
		}
		result.Results[index] = AuthorityPhaseReference{Phase: phase, Outcome: "not_run"}
	}
	return result, nil
}
