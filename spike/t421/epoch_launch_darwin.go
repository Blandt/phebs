//go:build darwin

package t421

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"time"
)

// Native profile/authorization state exists only where its issuers exist.
type executionEpochPlatform struct {
	profileSignerNamespace     *executionSignerNamespaceCustody
	profileTools               [2]*ExecutionToolCustody    // Optional Buf/focused protected copies; no dispatch permission.
	profileSigner              *ExecutionSystemToolCustody // Borrowed outer-owned signer; never a mounted input owner.
	profileSignerImage         executionProfileSystemImage
	profileSystemUsed          bool
	profileExecutor            *executionProfileExecutorCustody
	profileSignerNamespaceUsed bool
	profileHost                *executionHostObservation
	profileHostUsed            bool
	profileWorkspace           *executionWorkspaceCustodyCapability
	executionWholeResources    *executionWholeResources
	executionColdAuthorActive  bool
	executionFreezeBinding     *ExecutionFreezeBinding
	executionEventOrdinals     *admittedExecutionEventOrdinals
}

// bindProfileTools retains the two already reference-admitted, non-dispatched
// images before AuthorA. Omitted holders preserve the scoped rehearsal API;
// this pair alone does not issue a complete profile admission.
func (flow *ExecutionEpochOne) bindProfileTools(ctx context.Context, buf, focused *ExecutionToolCustody) error {
	if flow == nil || ctx == nil || ctx.Err() != nil || flow.epochs == nil || flow.epochs.author == nil || buf == nil || focused == nil {
		return ErrExecutionEpochOne
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	author, epochs := flow.epochs.author, flow.epochs
	author.mu.Lock()
	defer author.mu.Unlock()
	epochs.mu.Lock()
	defer epochs.mu.Unlock()
	if !processAccountingPlanSemantics(flow.plan.Schema) || flow.closed || flow.used || flow.authored || !flow.authorStarted.IsZero() ||
		flow.workspace != nil || flow.profileTools != ([2]*ExecutionToolCustody{}) ||
		author.closed || author.err != nil || author.active || author.borrowedBy != nil || author.next != 0 ||
		epochs.closed || epochs.err != nil || epochs.active || epochs.released != 0 || author.request.Builds == nil {
		return ErrExecutionEpochOne
	}
	builds := author.request.Builds
	if !builds.mu.TryLock() {
		return ErrExecutionEpochOne
	}
	defer builds.mu.Unlock()
	if builds.closed || builds.err != nil {
		return ErrExecutionEpochOne
	}
	selected := [2]*ExecutionToolCustody{buf, focused}
	for index, role := range [2]string{"buf", "phebs-focused-index"} {
		tool := selected[index]
		if tool.referenceInputs != author.request.Builds || filepath.Dir(tool.Directory()) != author.parent {
			return ErrExecutionEpochOne
		}
		if _, _, err := tool.Check(ctx, role); err != nil {
			return err
		}
	}
	flow.profileTools = selected
	return nil
}

// bindProfileSignerNamespace spends one preparation slot and retains the
// selected external signer registry root before pressure-workspace binding.
// It creates no key, claim, signature, socket, checkout, or handoff authority.
func (flow *ExecutionEpochOne) bindProfileSignerNamespace(ctx context.Context, selection executionSelectionV1) error {
	if flow == nil || flow.epochs == nil || flow.epochs.author == nil {
		return ErrExecutionEpochOne
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	author, epochs := flow.epochs.author, flow.epochs
	author.mu.Lock()
	defer author.mu.Unlock()
	epochs.mu.Lock()
	defer epochs.mu.Unlock()
	if !processAccountingPlanSemantics(flow.plan.Schema) || flow.closed || flow.used || flow.authored || !flow.authorStarted.IsZero() ||
		flow.workspace != nil || flow.profileSignerNamespaceUsed || !validExecutionSelection(selection) ||
		author.closed || author.err != nil || author.active || author.borrowedBy != nil || author.next != 0 ||
		epochs.closed || epochs.err != nil || epochs.active || epochs.released != 0 {
		return ErrExecutionEpochOne
	}
	flow.profileSignerNamespaceUsed = true
	custody, err := holdExecutionSignerNamespace(ctx, selection.SignerControlRoot)
	if err != nil {
		return ErrExecutionEpochOne
	}
	flow.profileSignerNamespace = custody
	return nil
}

// authorAAdmitted is the sole V3/V4 transition from verified private authority
// to operational work. Binding transfer and reserved ordinal one are atomic
// with entry into the existing direct AuthorA path.
func (flow *ExecutionEpochOne) authorAAdmitted(
	ctx context.Context,
	finalAdmissionDeadline time.Time,
	binding ExecutionFreezeBinding,
	ordinals *executionEventOrdinals,
	executePath string,
) (ExecutionAuthorResult, error) {
	if flow == nil || ctx == nil {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	outerDeadline, hasOuterDeadline := ctx.Deadline()
	if ctx.Err() != nil || !hasOuterDeadline ||
		finalAdmissionDeadline.After(outerDeadline) || !time.Now().Before(finalAdmissionDeadline) || ordinals == nil {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	planRaw, planErr := MarshalCanonical(flow.plan)
	if !flow.authorAReadyLocked(ctx) || !processAccountingPlanSemantics(flow.plan.Schema) || flow.executionFreezeBinding != nil ||
		flow.executionEventOrdinals != nil || binding.freeze.Schema != flow.plan.ToolPolicy.ExecutionFreezeSchema ||
		binding.admissionEventOrdinal != 1 || !validDigest(binding.freezeSHA256) ||
		planErr != nil || binding.planSHA256 != SHA256(planRaw) || !time.Now().Before(finalAdmissionDeadline) {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	admitted, ordinal, err := ordinals.consumeFinalAdmission()
	if err != nil || ordinal != 1 || admitted == nil {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	started := time.Now()
	recorder, err := newExecutionPhaseEventRecorder(admitted, flow.plan.PhaseOrder, started, outerDeadline)
	if err != nil || recorder.beginAt(flow.plan.PhaseOrder[0], started) != nil {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	retained := binding
	retained.freeze = cloneExecutionFreezeForBinding(binding.freeze)
	flow.executionFreezeBinding = &retained
	flow.executionEventOrdinals = admitted
	flow.executionPhaseEvents = recorder
	flow.executionEvidenceEvents = make(map[string]uint64, 48)
	flow.executionEvidenceTimes = make(map[string]time.Time, 48)
	resources, resourceErr := startExecutionWholeResources(ctx, executePath)
	flow.executionWholeResources = resources
	preflightErr := resourceErr
	if preflightErr == nil {
		preflightErr = flow.sampleAdmittedPreflightLocked(resources.ctx)
	}
	preflightErr = errors.Join(preflightErr, resources.finish())
	if preflightErr != nil {
		ordinal, _ := recorder.event("preflight")
		flow.executionEvidenceEvents["failure:preflight"] = ordinal
		return ExecutionAuthorResult{}, errors.Join(preflightErr, recorder.finish("preflight", "stopped"))
	}
	if recorder.finish("preflight", "passed") != nil {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	// Author A has always been admitted/accounted in phase two. Its cold
	// phase remains active until the real server's cold gate finishes.
	coldStarted := time.Now()
	if recorder.beginAt("cold", coldStarted) != nil || resources.begin("cold") != nil {
		return ExecutionAuthorResult{}, ErrExecutionEpochOne
	}
	var result ExecutionAuthorResult
	authorErr := resources.sampleDiskRoot(flow.workspace, false)
	if authorErr == nil {
		result, authorErr = flow.authorALockedAt(resources.ctx, coldStarted)
	}
	if authorErr != nil || !result.Completed {
		authorErr = errors.Join(authorErr, resources.finish())
		ordinal, _ := recorder.event("cold")
		flow.executionEvidenceEvents["failure:cold"] = ordinal
		return result, errors.Join(ErrExecutionEpochOne, authorErr, recorder.finish("cold", "stopped"))
	}
	flow.executionColdAuthorActive = true
	return result, nil
}

// One bounded actual phase-one observation, after admission and before any
// author/server start. Preparation snapshots cannot supply this timed sample.
// The existing root and no-child owner checks are retained across the walk.
func (flow *ExecutionEpochOne) sampleAdmittedPreflightLocked(ctx context.Context) error {
	if flow.workspaceBytes == nil || flow.workspace == nil || !flow.authorAReadyLocked(ctx) {
		return ErrExecutionEpochOne
	}
	confirm := func() bool {
		if !flow.authorAReadyLocked(ctx) {
			return false
		}
		author, epochs := flow.epochs.author, flow.epochs
		author.mu.Lock()
		epochs.mu.Lock()
		valid := !author.active && !author.closed && author.err == nil && author.borrowedBy == nil && author.next == 0 && !epochs.active && !epochs.closed && epochs.err == nil && epochs.released == 0
		epochs.mu.Unlock()
		author.mu.Unlock()
		state, err := flow.store.Snapshot()
		return valid && err == nil && state.Store.Phase == 2 && state.Opened == 0 && state.TerminalEOF == 0
	}
	value, err := flow.workspaceBytes.SampleConfirmed(ctx, 1, confirm)
	if err != nil || value.LogicalBytes > flow.plan.WorkEnvelope.MaximumDataLogicalBytes || value.AllocatedBytes > flow.plan.SafetyEnvelope.MaximumDataAllocatedBytes {
		return errors.Join(ErrExecutionEpochOne, err)
	}
	return nil
}

func validateObservedExecutionRuntime(observed *executionRuntimeObservation, plan Plan, profile ExecutionProfile, tools []ExecutionToolIdentity, path, directory string) error {
	index := slices.IndexFunc(tools, func(tool ExecutionToolIdentity) bool { return tool.Role == "phebs" })
	if observed == nil || index < 0 || observed.Identity != tools[index] || observed.Path != path || observed.Directory != directory ||
		!observed.RootStarted || !observed.RootJoined || !observed.SessionEmpty || !observed.Observed || !observed.Complete ||
		observed.PID <= 0 || observed.err != nil || observed.waited == nil || observed.stdout == nil || observed.stderr == nil ||
		observed.stdout.err != nil || observed.stderr.err != nil || observed.stderr.buffer.Len() != 0 ||
		observed.Deadline.IsZero() || !time.Now().Before(observed.Deadline) {
		return ErrExecutionEpochOne
	}
	raw, err := json.Marshal(observed.Facts)
	if err != nil {
		return ErrExecutionEpochOne
	}
	raw = append(raw, '\n')
	decoded, err := decodeExecutionRuntimeFacts(raw)
	commandSHA256, commandErr := executionRuntimeCommandSHA256(path, directory)
	if err != nil || commandErr != nil || !reflect.DeepEqual(decoded, observed.Facts) ||
		!bytes.Equal(raw, observed.stdout.buffer.Bytes()) || SHA256(raw) != observed.RawSHA256 ||
		commandSHA256 != observed.CommandSHA256 || validateExecutionRuntimeFacts(observed.Facts, plan, profile) != nil {
		return ErrExecutionEpochOne
	}
	return nil
}
