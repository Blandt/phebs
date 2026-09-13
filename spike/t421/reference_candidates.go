//go:build darwin

package t421

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type executionReferenceCandidates struct {
	mu     sync.Mutex
	parent productionRoot
	root   productionRoot
	paths  [5]string
	infos  [5]os.FileInfo
	closed bool
}

func executionReferenceCandidateRoles() [5]string {
	return [5]string{"t422-author", "phebs", "zoekt-git-index", "buf", "phebs-focused-index"}
}

// prepareExecutionReferenceCandidatesV3 builds only the five supplied Go
// images required by V3. The images carry no authority until the existing
// ProtectReferenceToolV3 verifier copies and independently rebuilds them.
func prepareExecutionReferenceCandidatesV3(ctx context.Context, inputs *ExecutionGoBuildCustody, parent string) (_ *executionReferenceCandidates, retErr error) {
	if ctx == nil || ctx.Err() != nil || inputs == nil || filepath.Dir(inputs.Directory()) != parent ||
		!executionGitPrivateDirectory(parent) {
		return nil, ErrExecutionGoBuildCustody
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	inputs.mu.Lock()
	defer inputs.mu.Unlock()
	if inputs.check(ctx) != nil {
		return nil, ErrExecutionGoBuildCustody
	}
	parentRoot, err := openProductionRoot(parent)
	if err != nil {
		return nil, ErrExecutionGoBuildCustody
	}
	candidates := &executionReferenceCandidates{parent: parentRoot}
	defer func() {
		if retErr != nil {
			_ = candidates.Close()
			retErr = ErrExecutionGoBuildCustody
		}
	}()
	directory, err := os.MkdirTemp(parent, "t422-supplied-builds-")
	if err != nil {
		return candidates, ErrExecutionGoBuildCustody
	}
	candidates.root.path = directory
	for _, name := range []string{"home", "tmp", "cache"} {
		if err := os.Mkdir(filepath.Join(directory, name), 0o700); err != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
	}
	request := ReferenceToolRequest{
		GoRoot:      filepath.Join(inputs.Directory(), "sdk"),
		ModuleCache: filepath.Join(inputs.Directory(), "modules"),
	}
	environment := referenceBuildEnvironment(request, directory)
	for index, value := range environment {
		if strings.HasPrefix(value, "PATH=") {
			environment[index] = "PATH=" + inputs.git.Directory()
		}
	}
	environment = append(environment,
		"GIT_EXEC_PATH="+inputs.git.Directory(),
		"GIT_ALLOW_PROTOCOL=file",
		"GIT_TEMPLATE_DIR="+os.DevNull,
	)
	roles := executionReferenceCandidateRoles()
	for index, role := range roles {
		if inputs.check(ctx) != nil || !executionGitPrivateDirectory(directory) {
			return candidates, ErrExecutionGoBuildCustody
		}
		packagePath, _, _, _, _, err := referenceToolRole(role)
		if err != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
		output := filepath.Join(directory, role)
		buildRoot, args, checkOverlay, err := referenceToolBuildArgs(ctx, role, PlanV3Schema,
			inputs.reference.root.root, request.ModuleCache, directory, output, packagePath)
		if err != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
		if _, err := runReferenceGo(ctx, buildRoot, filepath.Join(request.GoRoot, "bin", "go"), environment, 64<<10, args...); err != nil ||
			inputs.check(ctx) != nil || checkOverlay() != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
		info, err := os.Lstat(output)
		if err != nil || !inputCustodyOwned(info) || !info.Mode().IsRegular() || info.Size() < 1 ||
			info.Size() > maxInputCustodyFileBytes || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
			return candidates, ErrExecutionGoBuildCustody
		}
		candidates.paths[index], candidates.infos[index] = output, info
	}
	candidates.root, err = openProductionRoot(directory)
	if err != nil || candidates.root.volume != candidates.parent.volume || inputs.check(ctx) != nil {
		return candidates, ErrExecutionGoBuildCustody
	}
	return candidates, nil
}

func (candidates *executionReferenceCandidates) Path(ctx context.Context, role string) (string, error) {
	if candidates == nil {
		return "", ErrExecutionGoBuildCustody
	}
	candidates.mu.Lock()
	defer candidates.mu.Unlock()
	if ctx == nil || ctx.Err() != nil || candidates.closed || candidates.root.file == nil || candidates.parent.file == nil {
		return "", ErrExecutionGoBuildCustody
	}
	parent, parentErr := os.Lstat(candidates.parent.path)
	root, rootErr := os.Lstat(candidates.root.path)
	held, heldErr := candidates.root.file.Stat()
	if parentErr != nil || !os.SameFile(parent, candidates.parent.info) || !executionGitPrivateDirectory(candidates.parent.path) ||
		rootErr != nil || heldErr != nil ||
		!inputCustodySame(root, held) || candidates.root.volume != candidates.parent.volume {
		return "", ErrExecutionGoBuildCustody
	}
	for index, candidateRole := range executionReferenceCandidateRoles() {
		if role != candidateRole {
			continue
		}
		info, err := os.Lstat(candidates.paths[index])
		if err != nil || !inputCustodySame(info, candidates.infos[index]) {
			return "", ErrExecutionGoBuildCustody
		}
		return candidates.paths[index], nil
	}
	return "", ErrExecutionGoBuildCustody
}

// Close removes only this helper's fresh joined build scratch. Protected tool
// copies are owned by their existing ExecutionToolCustody values.
func (candidates *executionReferenceCandidates) Close() error {
	if candidates == nil {
		return nil
	}
	candidates.mu.Lock()
	defer candidates.mu.Unlock()
	if candidates.closed {
		return nil
	}
	candidates.closed = true
	var result error
	if candidates.root.file != nil {
		root, rootErr := os.Lstat(candidates.root.path)
		held, heldErr := candidates.root.file.Stat()
		if rootErr != nil || heldErr != nil || !os.SameFile(root, held) {
			result = ErrExecutionGoBuildCustody
		}
		result = errors.Join(result, candidates.root.file.Close())
	} else if candidates.root.path != "" && filepath.Dir(candidates.root.path) != candidates.parent.path {
		result = ErrExecutionGoBuildCustody
	}
	if candidates.parent.file != nil {
		parent, parentErr := os.Lstat(candidates.parent.path)
		if parentErr != nil || !os.SameFile(parent, candidates.parent.info) || !executionGitPrivateDirectory(candidates.parent.path) {
			result = ErrExecutionGoBuildCustody
		}
		result = errors.Join(result, candidates.parent.file.Close())
	}
	if result == nil && candidates.root.path != "" {
		result = os.RemoveAll(candidates.root.path)
	}
	if result != nil {
		return ErrExecutionGoBuildCustody
	}
	return nil
}

// bindCheckout issues the private checkout/build proof only from the retained
// immutable source custody and the complete observed tool inventory.
func (custody *ExecutionGoBuildCustody) bindCheckout(ctx context.Context, policy ToolPolicy, tools []ExecutionToolIdentity) (ExecutionCommits, CheckoutAdmissionBinding, error) {
	if custody == nil {
		return ExecutionCommits{}, CheckoutAdmissionBinding{}, ErrExecutionGoBuildCustody
	}
	custody.mu.Lock()
	defer custody.mu.Unlock()
	if custody.check(ctx) != nil || validateExecutionTools(tools, policy, custody.commits.T422SourceCommit) != nil {
		return ExecutionCommits{}, CheckoutAdmissionBinding{}, ErrExecutionGoBuildCustody
	}
	toolsSHA256, err := canonicalSHA256(tools)
	if err != nil {
		return ExecutionCommits{}, CheckoutAdmissionBinding{}, ErrExecutionGoBuildCustody
	}
	return custody.commits, CheckoutAdmissionBinding{commits: custody.commits, toolsSHA256: toolsSHA256, verified: true}, nil
}
