//go:build darwin

package t421

import (
	"context"
	"os"
	"path/filepath"
	"sync"
)

// The capability owns the exact object graph and held descriptors admitted by
// bindRehearsal. It has no exported fields, serialization form or caller-facing constructor.
// A failed or canceled attempt spends it just as a successful attempt does.
type executionWorkspaceCustodyCapability struct {
	state *executionWorkspaceCustodyCapabilityState
}

type executionWorkspaceCustodyCapabilityState struct {
	mu    sync.Mutex
	proof *executionWorkspaceCustodyProof
}

type executionWorkspaceCustodyProof struct {
	volume  *executionPressureVolume
	flow    *ExecutionEpochOne
	profile *executionWorkspaceCustodyCapability
}

// This is the transferred workspace half of future final admission. Possession
// alone authorizes no operation; T42.2m must consume it together with the exact
// signed-freeze and checkout proofs before an operational handoff.
type executionOperationalHandoffCapability struct {
	state *executionOperationalHandoffCapabilityState
}

type executionOperationalHandoffCapabilityState struct {
	mu        sync.Mutex
	proof     *executionWorkspaceCustodyProof
	preimages executionObservedProfilePreimages
}

func newExecutionWorkspaceCustodyCapability(schema string, volume *executionPressureVolume, flow *ExecutionEpochOne) *executionWorkspaceCustodyCapability {
	if schema != PlanV3Schema || volume == nil || flow == nil {
		return nil
	}
	capability := &executionWorkspaceCustodyCapability{state: &executionWorkspaceCustodyCapabilityState{}}
	capability.state.proof = &executionWorkspaceCustodyProof{volume: volume, flow: flow, profile: capability}
	return capability
}

func (capability *executionWorkspaceCustodyCapability) ConsumeForProfile(ctx context.Context) (executionObservedProfilePreimages, *executionOperationalHandoffCapability, error) {
	if capability == nil || capability.state == nil {
		return executionObservedProfilePreimages{}, nil, errPressureVolume
	}
	capability.state.mu.Lock()
	proof := capability.state.proof
	capability.state.proof = nil
	capability.state.mu.Unlock()
	if proof == nil {
		return executionObservedProfilePreimages{}, nil, errPressureVolume
	}
	observed, err := proof.issueProfile(ctx)
	if err != nil {
		return executionObservedProfilePreimages{}, nil, err
	}
	return observed, &executionOperationalHandoffCapability{state: &executionOperationalHandoffCapabilityState{proof: proof, preimages: observed}}, nil
}

// consumeWorkspace performs the workspace part of final revalidation once and
// returns only the same privately issued preimages already held by this
// capability. It accepts no caller digest or caller-constructible verified bit.
func (capability *executionOperationalHandoffCapability) consumeWorkspace(ctx context.Context) (executionObservedProfilePreimages, error) {
	if capability == nil || capability.state == nil {
		return executionObservedProfilePreimages{}, errPressureVolume
	}
	capability.state.mu.Lock()
	proof := capability.state.proof
	preimages := capability.state.preimages
	capability.state.proof = nil
	capability.state.preimages = executionObservedProfilePreimages{}
	capability.state.mu.Unlock()
	if proof == nil {
		return executionObservedProfilePreimages{}, errPressureVolume
	}
	if err := proof.revalidate(ctx, preimages); err != nil {
		return executionObservedProfilePreimages{}, err
	}
	return preimages, nil
}

func (proof *executionWorkspaceCustodyProof) issueProfile(ctx context.Context) (executionObservedProfilePreimages, error) {
	var zero executionObservedProfilePreimages
	if proof == nil || proof.volume == nil || proof.flow == nil || ctx == nil || ctx.Err() != nil {
		return zero, errPressureVolume
	}
	v, flow := proof.volume, proof.flow
	v.mu.Lock()
	defer v.mu.Unlock()
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.epochs == nil || flow.epochs.author == nil {
		return zero, errPressureVolume
	}
	author, epochs := flow.epochs.author, flow.epochs
	author.mu.Lock()
	defer author.mu.Unlock()
	epochs.mu.Lock()
	defer epochs.mu.Unlock()
	if proof.profile == nil || flow.profileWorkspace != proof.profile || !v.rehearsalWorkspaceValidLocked(ctx, flow, true) || !profileObservationSetComplete(flow) {
		return zero, errPressureVolume
	}
	pressure, roots, err := v.profilePreimagesLocked(ctx, flow)
	if err != nil || !v.rehearsalWorkspaceValidLocked(ctx, flow, true) {
		return zero, errPressureVolume
	}
	observed, err := issueObservedProfilePreimages(flow.profileCommands, pressure, roots)
	if err != nil || ctx.Err() != nil {
		return zero, errPressureVolume
	}
	return observed, nil
}

func (proof *executionWorkspaceCustodyProof) revalidate(ctx context.Context, expected executionObservedProfilePreimages) error {
	if proof == nil || proof.volume == nil || proof.flow == nil || ctx == nil || ctx.Err() != nil {
		return errPressureVolume
	}
	v, flow := proof.volume, proof.flow
	v.mu.Lock()
	defer v.mu.Unlock()
	flow.mu.Lock()
	defer flow.mu.Unlock()
	if flow.epochs == nil || flow.epochs.author == nil {
		return errPressureVolume
	}
	author, epochs := flow.epochs.author, flow.epochs
	author.mu.Lock()
	defer author.mu.Unlock()
	epochs.mu.Lock()
	defer epochs.mu.Unlock()
	if proof.profile == nil || flow.profileWorkspace != proof.profile || !v.rehearsalWorkspaceValidLocked(ctx, flow, true) || !profileObservationSetComplete(flow) {
		return errPressureVolume
	}
	pressure, roots, err := v.profilePreimagesLocked(ctx, flow)
	if err != nil || !v.rehearsalWorkspaceValidLocked(ctx, flow, true) {
		return errPressureVolume
	}
	observed, err := issueObservedProfilePreimages(flow.profileCommands, pressure, roots)
	if err != nil || observed != expected || ctx.Err() != nil {
		return errPressureVolume
	}
	return nil
}

func profileObservationSetComplete(flow *ExecutionEpochOne) bool {
	return flow.plan.Schema == PlanV3Schema && flow.profileEnvironmentUsed && flow.profileEnvironment != nil && len(flow.profileCommands) == 3 &&
		flow.profileHostUsed && flow.profileHost != nil && flow.profileSystemUsed &&
		flow.profileSigner != nil && flow.profileTools[0] != nil && flow.profileTools[1] != nil &&
		flow.profileRuntime != nil && flow.profileRuntime.Complete && flow.profileRuntime.err == nil && flow.profileRuntime.releasable()
}

// Caller holds volume, flow, author and epochs locks. Each named role is read
// from its production-held descriptor and current path; the config role checks
// both config and catalog roots before collapsing their common FSID.
func (v *executionPressureVolume) profilePreimagesLocked(ctx context.Context, flow *ExecutionEpochOne) (executionPressureCommandSetPreimageV1, executionRootVolumeBindingsPreimageV1, error) {
	var pressure executionPressureCommandSetPreimageV1
	var roots executionRootVolumeBindingsPreimageV1
	if ctx == nil || ctx.Err() != nil || v.pressureCommandCount != 2 || v.device == "" || v.device != v.attachDevice ||
		v.ballast == nil || v.ballast.file == nil || v.ballast.failed || v.ballast.removed || flow.profileHost == nil {
		return pressure, roots, errPressureVolume
	}
	signerIdentity, signerPath, err := flow.profileSigner.Check(ctx, "ssh-keygen")
	if err != nil || flow.profileSignerImage != (executionProfileSystemImage{Identity: signerIdentity, Path: signerPath}) {
		return pressure, roots, errPressureVolume
	}
	for index, role := range [2]string{"buf", "phebs-focused-index"} {
		if _, _, err := flow.profileTools[index].Check(ctx, role); err != nil {
			return pressure, roots, errPressureVolume
		}
	}
	identity, toolPath, err := v.tool.Check(ctx, "hdiutil")
	if err != nil {
		return pressure, roots, errPressureVolume
	}
	pressure, err = executionPressureCommandSetPreimage(identity, toolPath, v.root.path, v.attachDevice, v.pressureCommands)
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	author, epochs := flow.epochs.author, flow.epochs
	backing, err := heldProductionRootVolume(v.parent)
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	source, err := heldProductionRootVolume(author.roots[1])
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	data, err := heldProductionRootVolume(epochs.roots[0])
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	backup, err := heldProductionRootVolume(epochs.roots[3])
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	home, err := heldProductionRootVolume(epochs.roots[1])
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	temporary, err := heldProductionRootVolume(epochs.roots[2])
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	toolOutput, err := heldBuildDirectoryVolume(author.request.Builds)
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	if _, err := v.ballast.sample(); err != nil {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	ballast := v.workspace.volume // sample read this value through the held ballast inode.
	inputs := []*ExecutionInputCustody{author.request.Plan, author.request.Git.input, epochs.catalogs, epochs.configs}
	for _, tool := range []*ExecutionToolCustody{author.request.Author, flow.phebs, flow.zoekt, flow.surreal, flow.profileTools[0], flow.profileTools[1]} {
		inputs = append(inputs, tool.input)
	}
	inputVolumes := make([][2]int32, len(inputs))
	for index, input := range inputs {
		volume, inputErr := heldInputDirectoryVolume(input)
		if inputErr != nil || volume != data {
			return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
		}
		inputVolumes[index] = volume
	}
	config, catalog := inputVolumes[3], inputVolumes[2]
	if catalog != config {
		return executionPressureCommandSetPreimageV1{}, roots, errPressureVolume
	}
	roots, err = executionRootVolumeBindingsFromObservation(*flow.profileHost,
		[9][2]int32{backing, source, config, data, backup, home, temporary, toolOutput, ballast})
	if err != nil {
		return executionPressureCommandSetPreimageV1{}, executionRootVolumeBindingsPreimageV1{}, errPressureVolume
	}
	return pressure, roots, nil
}

func heldProductionRootVolume(root productionRoot) ([2]int32, error) {
	if pressureRootsUnchanged(root) != nil {
		return [2]int32{}, errPressureVolume
	}
	return root.volume, nil // pressureRootsUnchanged just re-read this FSID.
}

func heldInputDirectoryVolume(input *ExecutionInputCustody) ([2]int32, error) {
	if input == nil {
		return [2]int32{}, errPressureVolume
	}
	input.mu.Lock()
	defer input.mu.Unlock()
	if input.closed || input.err != nil || input.root == nil {
		return [2]int32{}, errPressureVolume
	}
	return heldDirectoryVolume(input.root, input.rootInfo, input.directory, input.volume)
}

func heldBuildDirectoryVolume(builds *ExecutionGoBuildCustody) ([2]int32, error) {
	if builds == nil {
		return [2]int32{}, errPressureVolume
	}
	builds.mu.Lock()
	defer builds.mu.Unlock()
	if builds.closed || builds.err != nil || builds.root == nil {
		return [2]int32{}, errPressureVolume
	}
	return heldDirectoryVolume(builds.root, nil, builds.directory, builds.volume)
}

func heldDirectoryVolume(file *os.File, original os.FileInfo, path string, expected [2]int32) ([2]int32, error) {
	if file == nil || !filepath.IsAbs(path) {
		return [2]int32{}, errPressureVolume
	}
	held, err := file.Stat()
	current, pathErr := os.Lstat(path)
	canonical, canonicalErr := filepath.EvalSymlinks(path)
	volume, volumeErr := inputCustodyVolume(file)
	if err != nil || pathErr != nil || canonicalErr != nil || volumeErr != nil || canonical != path ||
		!os.SameFile(held, current) || original != nil && !os.SameFile(original, held) || !inputCustodyOwned(current) ||
		!current.IsDir() || current.Mode().Perm() != 0o700 || volume != expected {
		return [2]int32{}, errPressureVolume
	}
	return volume, nil
}
