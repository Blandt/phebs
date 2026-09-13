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
	mu               sync.Mutex
	parent           productionRoot
	root             productionRoot
	paths            [5]string
	infos            [5]os.FileInfo
	cleanupUncertain bool
	closed           bool
	closeErr         error
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
	root, err := openProductionRoot(directory)
	if err != nil {
		return candidates, ErrExecutionGoBuildCustody
	}
	candidates.root = root
	if candidates.checkRoots() != nil {
		return candidates, ErrExecutionGoBuildCustody
	}
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
		if inputs.check(ctx) != nil || candidates.checkRoots() != nil {
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
		if candidates.checkRoots() != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
		// A generic command failure does not establish an empty private session.
		// Retain scratch unless the supervised command proves complete success.
		candidates.cleanupUncertain = true
		if _, err := runReferenceGo(ctx, buildRoot, filepath.Join(request.GoRoot, "bin", "go"), environment, 64<<10, args...); err != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
		candidates.cleanupUncertain = false
		if inputs.check(ctx) != nil || candidates.checkRoots() != nil || checkOverlay() != nil {
			return candidates, ErrExecutionGoBuildCustody
		}
		info, err := os.Lstat(output)
		if err != nil || !inputCustodyOwned(info) || !info.Mode().IsRegular() || info.Size() < 1 ||
			info.Size() > maxInputCustodyFileBytes || info.Mode().Perm()&0o111 == 0 || info.Mode().Perm()&0o022 != 0 {
			return candidates, ErrExecutionGoBuildCustody
		}
		candidates.paths[index], candidates.infos[index] = output, info
	}
	if candidates.checkRoots() != nil || inputs.check(ctx) != nil {
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
	if ctx == nil || ctx.Err() != nil || candidates.closed || candidates.cleanupUncertain || candidates.checkRoots() != nil {
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

func (candidates *executionReferenceCandidates) checkRoots() error {
	if candidates.root.info == nil || candidates.parent.info == nil ||
		filepath.Dir(candidates.root.path) != candidates.parent.path || candidates.root.volume != candidates.parent.volume ||
		pressureRootsUnchanged(candidates.parent, candidates.root) != nil {
		return ErrExecutionGoBuildCustody
	}
	return nil
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
		return candidates.closeErr
	}
	candidates.closed = true
	var result error
	if candidates.cleanupUncertain {
		result = ErrExecutionGoBuildCustody
	}
	if candidates.root.path != "" {
		result = errors.Join(result, candidates.checkRoots())
	} else if candidates.parent.file != nil {
		result = errors.Join(result, pressureRootsUnchanged(candidates.parent))
	}
	for _, file := range []*os.File{candidates.root.file, candidates.parent.file} {
		if file != nil {
			result = errors.Join(result, file.Close())
		}
	}
	if result == nil && candidates.root.path != "" {
		result = os.RemoveAll(candidates.root.path)
	}
	if result != nil {
		candidates.closeErr = ErrExecutionGoBuildCustody
	}
	return candidates.closeErr
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
