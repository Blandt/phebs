//go:build darwin

package t421

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// executionInnerPreparation owns the complete pre-AuthorA object graph. Its
// live handoff and signer objects are intentionally private and nonserializable.
type executionInnerPreparation struct {
	mu sync.Mutex

	operational productionRoot
	volume      *executionPressureVolume
	ballast     *executionPressureBallast
	signer      *ExecutionSystemToolCustody
	git         *ExecutionGitCustody
	builds      *ExecutionGoBuildCustody
	candidates  *executionReferenceCandidates
	tools       [5]*ExecutionToolCustody
	surreal     *ExecutionToolCustody
	planInput   *ExecutionInputCustody
	author      *ExecutionAuthorCustody
	epochs      *ExecutionEpochConfigCustody
	flow        *ExecutionEpochOne
	profile     ExecutionProfile
	admission   ExecutionProfileAdmissionBinding
	handoff     *executionOperationalHandoffCapability
	projection  executionAuthorizationHandoffProjection
	claim       *executionSignerCeremonyClaimCustody
	key         *executionSignerKeyCustody
	candidate   executionFreezeCandidatePreparation

	closed bool
}

// prepareExecutionInnerPreparation performs only the bounded non-operational
// preparation admitted before AuthorA. The returned owner is nonnil after any
// created custody so failure evidence is not silently removed.
func prepareExecutionInnerPreparation(
	ctx context.Context,
	selection executionSelectionV1,
	parent *executionParentLiveness,
	outerDeadline time.Time,
) (*executionInnerPreparation, error) {
	prepared := &executionInnerPreparation{}
	refuse := func() (*executionInnerPreparation, error) { return prepared, ErrExecutionLauncher }
	if ctx == nil || ctx.Err() != nil || !validExecutionSelection(selection) || parent == nil || parent.alive == nil ||
		parent.alive.Err() != nil || !executionSessionIsolated(os.Getppid()) || !time.Now().Before(outerDeadline) {
		return refuse()
	}

	var err error
	prepared.operational, err = createExecutionOperationalRoot(selection)
	if err != nil {
		return refuse()
	}
	prepared.volume, err = prepareExecutionPressureVolume(ctx, prepared.operational.path)
	if err != nil {
		return refuse()
	}
	ctx, workspace, err := prepared.volume.borrowWorkspace(ctx)
	if err != nil {
		return refuse()
	}
	prepared.signer, err = HoldExecutionSystemTool(ctx, "ssh-keygen")
	if err != nil {
		return refuse()
	}
	prepared.git, err = ProtectExecutionGit(ctx, workspace, selection.GitBinary)
	if err != nil {
		return refuse()
	}
	prepared.builds, err = ProtectExecutionGoBuildInputs(ctx, workspace, ExecutionGoBuildRequest{
		Git: prepared.git, RepositoryRoot: selection.RepositoryRoot,
		PlanSourceCommit: selection.PlanSourceCommit, IntegratedMainCommit: selection.IntegratedMainCommit,
		SourceCommit: selection.SourceCommit, GoRoot: selection.GoRoot, ModuleCache: selection.ModuleCache,
	})
	if err != nil {
		return refuse()
	}
	prepared.candidates, err = prepareExecutionReferenceCandidatesV3(ctx, prepared.builds, workspace)
	if err != nil {
		return refuse()
	}
	roles := executionReferenceCandidateRoles()
	for index, role := range roles {
		path, pathErr := prepared.candidates.Path(ctx, role)
		if pathErr != nil {
			return refuse()
		}
		prepared.tools[index], err = prepared.builds.ProtectReferenceToolV3(ctx, workspace, role, path)
		if err != nil {
			return refuse()
		}
	}
	if err := prepared.candidates.Close(); err != nil {
		return refuse()
	}
	prepared.candidates = nil
	prepared.surreal, err = ProtectExecutionExternalTool(ctx, workspace, "surreal", selection.SurrealBinary)
	if err != nil {
		return refuse()
	}

	plan, err := BuildPlanV3WithLogicalStoreWork(selection.SourceCommit)
	if err != nil {
		return refuse()
	}
	raw, err := MarshalCanonical(plan)
	if err != nil {
		return refuse()
	}
	planPath := filepath.Join(workspace, "unsealed-plan-input.json")
	if err := writeExecutionInnerPlan(prepared.volume.workspace, planPath, raw); err != nil {
		return refuse()
	}
	prepared.planInput, err = ProtectExecutionInputs(ctx, workspace, []ExecutionInputCopy{{Name: "plan", Path: planPath, SHA256: SHA256(raw)}})
	if err != nil {
		return refuse()
	}
	prepared.author, err = PrepareExecutionAuthor(ctx, workspace, ExecutionAuthorRequest{
		Git: prepared.git, Builds: prepared.builds, Author: prepared.tools[0], Plan: prepared.planInput,
	})
	if err != nil {
		return refuse()
	}
	prepared.epochs, err = PrepareExecutionEpochConfigs(ctx, prepared.author)
	if err != nil {
		return refuse()
	}
	prepared.flow, err = PrepareExecutionEpochOne(ctx, prepared.epochs, prepared.tools[1], prepared.tools[2], prepared.surreal)
	if err != nil {
		return refuse()
	}
	if prepared.flow.bindProfileTools(ctx, prepared.tools[3], prepared.tools[4]) != nil ||
		prepared.flow.prepareProfileSigner(ctx, prepared.signer) != nil ||
		prepared.flow.bindProfileSignerNamespace(ctx, selection) != nil ||
		prepared.flow.bindProfileExecutor(ctx, parent) != nil {
		return refuse()
	}
	prepared.ballast, err = prepareExecutionPressureBallast(ctx, prepared.volume)
	if err != nil {
		return refuse()
	}
	if _, err := prepared.volume.samplePreparation(ctx); err != nil || prepared.volume.bindRehearsal(ctx, prepared.flow) != nil ||
		prepared.volume.observeProfileHost(ctx, prepared.flow) != nil || prepared.flow.prepareProfileEnvironment(ctx) != nil ||
		prepared.flow.prepareProfileRuntime(ctx) != nil {
		return refuse()
	}
	prepared.profile, prepared.admission, prepared.handoff, err = prepared.flow.issueExecutionProfile(ctx)
	if err != nil {
		return refuse()
	}
	executePath, executeDigest, err := parent.image.observe(ctx)
	if err != nil {
		return refuse()
	}
	prepared.projection, err = projectExecutionAuthorizationHandoff(executePath,
		filepath.Join(prepared.operational.path, executionAuthorizationSocketName), executeDigest,
		outerDeadline.UnixNano())
	if err != nil {
		return refuse()
	}
	namespace, err := prepared.flow.profileSignerNamespace.check(ctx)
	if err != nil {
		return refuse()
	}
	prepared.claim, err = claimExecutionSignerCeremony(ctx, namespace, selection.CeremonyID, prepared.operational.path)
	if err != nil {
		return refuse()
	}
	prepared.key, err = prepareExecutionSignerKey(ctx, prepared.claim, prepared.signer)
	if err != nil {
		return refuse()
	}
	prepared.candidate, err = prepared.handoff.prepareFreezeCandidate(ctx, prepared.key.fingerprint)
	if err != nil {
		return refuse()
	}
	return prepared, nil
}

func createExecutionOperationalRoot(selection executionSelectionV1) (productionRoot, error) {
	path, err := os.MkdirTemp("/private/tmp", "phebs-t422-")
	if err != nil {
		return productionRoot{}, ErrExecutionLauncher
	}
	remove := true
	defer func() {
		if remove {
			_ = os.Remove(path)
		}
	}()
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil || canonical != path || len(filepath.Join(path, executionAuthorizationSocketName)) > maxExecutionAuthSocketPathBytes {
		return productionRoot{}, ErrExecutionLauncher
	}
	selected := []string{selection.RepositoryRoot, selection.GoRoot, selection.ModuleCache, selection.GitBinary, selection.SurrealBinary, selection.SignerControlRoot}
	for _, other := range selected {
		if executionPathContains(path, other) || executionPathContains(other, path) {
			return productionRoot{}, ErrExecutionLauncher
		}
	}
	root, err := openProductionRoot(path)
	if err != nil || preflightExecutionAuthSocket(path) != nil {
		_ = closeExecutionOperationalRoot(root)
		return productionRoot{}, ErrExecutionLauncher
	}
	remove = false
	return root, nil
}

func writeExecutionInnerPlan(root productionRoot, path string, raw []byte) (retErr error) {
	if len(raw) == 0 || filepath.Dir(path) != root.path || pressureRootsUnchanged(root) != nil {
		return ErrExecutionLauncher
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return ErrExecutionLauncher
	}
	defer func() { retErr = errors.Join(retErr, file.Close()) }()
	if written, err := file.Write(raw); err != nil || written != len(raw) || file.Sync() != nil || root.file.Sync() != nil {
		return ErrExecutionLauncher
	}
	return nil
}

// Close releases only a preparation whose operational volume has already
// completed its existing ceremony teardown. Refusal keeps the full graph live
// for exact retained-custody diagnosis.
func (prepared *executionInnerPreparation) Close() error {
	if prepared == nil {
		return nil
	}
	prepared.mu.Lock()
	defer prepared.mu.Unlock()
	if prepared.closed {
		return nil
	}
	if prepared.volume != nil {
		prepared.volume.mu.Lock()
		released := prepared.volume.removed && !prepared.volume.borrowed
		prepared.volume.mu.Unlock()
		if !released {
			return errPressureVolume
		}
	}
	prepared.closed = true
	var result error
	result = errors.Join(result, prepared.key.Close(), prepared.claim.Close(), prepared.flow.Close(), prepared.epochs.Close(), prepared.author.Close(), prepared.planInput.Close())
	if prepared.surreal != nil {
		result = errors.Join(result, prepared.surreal.Close())
	}
	for index := len(prepared.tools) - 1; index >= 0; index-- {
		if prepared.tools[index] != nil {
			result = errors.Join(result, prepared.tools[index].Close())
		}
	}
	result = errors.Join(result, prepared.candidates.Close(), prepared.builds.Close(), prepared.git.Close(), prepared.signer.Close(), prepared.volume.Close())
	if result == nil {
		result = closeExecutionOperationalRoot(prepared.operational)
	}
	return result
}

func closeExecutionOperationalRoot(root productionRoot) error {
	if root.file == nil || root.path == "" || pressureRootsUnchanged(root) != nil || !pressureDirectoryEmpty(root.path) {
		return ErrExecutionLauncher
	}
	if root.file.Close() != nil || os.Remove(root.path) != nil {
		return ErrExecutionLauncher
	}
	return nil
}
