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
	if flow.plan.Schema != PlanV3Schema || flow.closed || flow.used || flow.authored || !flow.authorStarted.IsZero() ||
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
	if flow.plan.Schema != PlanV3Schema || flow.closed || flow.used || flow.authored || !flow.authorStarted.IsZero() ||
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

// authorAAdmitted is the sole V3 transition from verified private authority to
// operational work. Binding transfer and reserved ordinal one are atomic with
// entry into the existing direct AuthorA path.
func (flow *ExecutionEpochOne) authorAAdmitted(
	ctx context.Context,
	finalAdmissionDeadline time.Time,
	binding ExecutionFreezeBinding,
	ordinals *executionEventOrdinals,
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
	if !flow.authorAReadyLocked(ctx) || flow.plan.Schema != PlanV3Schema || flow.executionFreezeBinding != nil ||
		flow.executionEventOrdinals != nil || binding.freeze.Schema != ExecutionFreezeV3Schema ||
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
	result, authorErr := flow.authorALockedAt(ctx, started)
	outcome := "passed"
	if authorErr != nil || !result.Completed {
		outcome = "stopped"
		if authorErr == nil {
			authorErr = ErrExecutionEpochOne
		}
	}
	finishErr := recorder.finish(flow.plan.PhaseOrder[0], outcome)
	return result, errors.Join(authorErr, finishErr)
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
