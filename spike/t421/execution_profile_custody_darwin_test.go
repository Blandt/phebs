//go:build darwin

package t421

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/spike/t4013"
)

func TestExecutionPressureCommandRecipe(t *testing.T) {
	root := "/private/tmp/t422-pressure-fixture"
	device := "/dev/disk4"
	wantEnvironment := []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "HOME=" + root + "/home", "TMPDIR=" + root + "/tmp"}
	rows := [2]executionPressureCommandPreimageV1{}
	for index, test := range []struct {
		name       string
		wantArgs   []string
		normalized []string
	}{
		{"create", []string{"create", "-size", "96g", "-layout", "NONE", "-type", "SPARSE", "-fs", "APFS", "-volname", "phebs-t422-private", "-nospotlight", root + "/pressure.sparseimage"},
			[]string{"create", "-size", "96g", "-layout", "NONE", "-type", "SPARSE", "-fs", "APFS", "-volname", "phebs-t422-private", "-nospotlight", "@pressure-image"}},
		{"attach", []string{"attach", "-owners", "on", "-nobrowse", "-noautoopen", "-mountpoint", root + "/mount", "-plist", root + "/pressure.sparseimage"},
			[]string{"attach", "-owners", "on", "-nobrowse", "-noautoopen", "-mountpoint", "@pressure-mount", "-plist", "@pressure-image"}},
		{"detach", []string{"detach", device}, []string{"detach", "@pressure-device"}},
	} {
		args, row, err := executionPressureCommandRecipe(root, test.name, device)
		if err != nil || !reflect.DeepEqual(args, test.wantArgs) || row.Name != test.name || row.WorkingDirectory != root ||
			!reflect.DeepEqual(row.Environment, wantEnvironment) || !reflect.DeepEqual(row.NormalizedArgv, test.normalized) {
			t.Fatalf("%s recipe differs: %#v %#v %v", test.name, args, row, err)
		}
		if index < 2 {
			rows[index] = row
		}
	}
	identity := ExecutionToolIdentity{Role: "hdiutil", FileType: regularFileType, SHA256: SHA256([]byte("actual-hdiutil")), Version: "host", Provenance: "external-executed-file-v1"}
	set, err := executionPressureCommandSetPreimage(identity, "/usr/bin/hdiutil", root, device, rows)
	if err != nil || set.Schema != executionPressureCommandSetPreimageV1Schema || set.Tool != identity || set.ToolPath != "/usr/bin/hdiutil" ||
		set.AttachDevice != device || len(set.Commands) != 3 || set.Commands[2].NormalizedArgv[1] != "@pressure-device" {
		t.Fatal("exact actual pressure command set refused", set, err)
	}
	for _, mutate := range []func(*[2]executionPressureCommandPreimageV1, *string){
		func(rows *[2]executionPressureCommandPreimageV1, _ *string) {
			rows[0].NormalizedArgv[12] = root + "/pressure.sparseimage"
		},
		func(_ *[2]executionPressureCommandPreimageV1, device *string) { *device = "@pressure-device" },
	} {
		changed := [2]executionPressureCommandPreimageV1{clonePressureRow(rows[0]), clonePressureRow(rows[1])}
		raw := device
		mutate(&changed, &raw)
		if got, err := executionPressureCommandSetPreimage(identity, "/usr/bin/hdiutil", root, raw, changed); err == nil || !reflect.DeepEqual(got, executionPressureCommandSetPreimageV1{}) {
			t.Fatal("mutated actual pressure recipe issued a preimage")
		}
	}
}

func TestExecutionPressureCommandStartPaths(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(parent, "pressure")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"home", "tmp"} {
		if err := os.Mkdir(filepath.Join(rootPath, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	root, err := openProductionRoot(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = root.file.Close() })
	v := &executionPressureVolume{root: root}
	for index, name := range []string{"home", "tmp"} {
		v.environmentInfos[index], err = os.Lstat(filepath.Join(rootPath, name))
		if err != nil {
			t.Fatal(err)
		}
	}
	_, row, err := executionPressureCommandRecipe(rootPath, "detach", "/dev/disk4")
	if err != nil || !v.pressureCommandPathsUnchanged(row) {
		t.Fatal("exact held cwd and HOME/TMP paths refused", err)
	}
	for _, mutate := range []func(*executionPressureCommandPreimageV1){
		func(row *executionPressureCommandPreimageV1) { row.WorkingDirectory += "-other" },
		func(row *executionPressureCommandPreimageV1) { row.Environment[3] += "-other" },
	} {
		changed := clonePressureRow(row)
		mutate(&changed)
		if v.pressureCommandPathsUnchanged(changed) {
			t.Fatal("changed command path identity reached Start")
		}
	}
	for _, name := range []string{"home", "tmp"} {
		path := filepath.Join(rootPath, name)
		held := path + "-held"
		if err := os.Rename(path, held); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if v.pressureCommandPathsUnchanged(row) {
			t.Fatalf("replaced %s path reached Start", name)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(held, path); err != nil {
			t.Fatal(err)
		}
		if !v.pressureCommandPathsUnchanged(row) {
			t.Fatalf("restored %s path did not recover exact identity", name)
		}
	}
	heldRoot := rootPath + "-held"
	if err := os.Rename(rootPath, heldRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if v.pressureCommandPathsUnchanged(row) {
		t.Fatal("replaced working directory reached Start")
	}
}

func TestExecutionHeldDirectoryVolumeIdentityAndReplacement(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "held")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	volume, err := inputCustodyVolume(file)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := heldDirectoryVolume(file, info, path, volume); err != nil || got != volume {
		t.Fatal("held directory identity refused", got, err)
	}
	moved := path + "-held"
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := heldDirectoryVolume(file, info, path, volume); err == nil || got != ([2]int32{}) {
		t.Fatal("path replacement retained held-directory authority")
	}
}

func TestExecutionWorkspaceCapabilityIsV3Only(t *testing.T) {
	volume, flow := &executionPressureVolume{}, &ExecutionEpochOne{}
	for _, schema := range []string{"", PlanSchema, PlanV2Schema} {
		if got := newExecutionWorkspaceCustodyCapability(schema, volume, flow); got != nil {
			t.Fatal("V1/V2 bind issued prospective V3 profile capability", schema)
		}
	}
	got := newExecutionWorkspaceCustodyCapability(PlanV3Schema, volume, flow)
	if got == nil || got.state == nil || got.state.proof == nil || got.state.proof.volume != volume || got.state.proof.flow != flow || got.state.proof.profile != got {
		t.Fatal("V3 capability did not capture exact owner graph")
	}
}

func TestExecutionProfileObservationSetRequiresCompleteToolCustody(t *testing.T) {
	flow := &ExecutionEpochOne{
		plan:                   Plan{Schema: PlanV3Schema},
		profileEnvironmentUsed: true,
		profileEnvironment:     &executionRuntimeEnvironmentObservation{},
		profileCommands:        make([]ExecutionCommandProfile, 3),
		profileHostUsed:        true,
		profileHost:            &executionHostObservation{},
		profileSystemUsed:      true,
		profileSigner:          &ExecutionSystemToolCustody{},
		profileRuntime:         &executionRuntimeObservation{Complete: true},
	}
	if profileObservationSetComplete(flow) {
		t.Fatal("profile completed without Buf/focused custody")
	}
	flow.profileTools[0] = &ExecutionToolCustody{}
	if profileObservationSetComplete(flow) {
		t.Fatal("profile completed with only one protected tool")
	}
	flow.profileTools[1] = &ExecutionToolCustody{}
	if !profilePreimageObservationSetComplete(flow) || profileObservationSetComplete(flow) {
		t.Fatal("scoped preimage custody was not distinguished from complete issuer custody")
	}
	flow.profileExecutor = &executionProfileExecutorCustody{}
	if !profileObservationSetComplete(flow) {
		t.Fatal("complete observed profile custody was refused")
	}
}

func TestExecutionProfileMountedInputsExcludeOuterExecutor(t *testing.T) {
	values := [7]*ExecutionToolCustody{}
	for index := range values {
		values[index] = &ExecutionToolCustody{}
	}
	flow := &ExecutionEpochOne{
		epochs: &ExecutionEpochConfigCustody{author: &ExecutionAuthorCustody{request: ExecutionAuthorRequest{Author: values[0]}}},
		phebs:  values[1], zoekt: values[2], surreal: values[3], profileExecutor: &executionProfileExecutorCustody{},
		profileTools: [2]*ExecutionToolCustody{values[4], values[5]},
	}
	if got := profileMountedInputTools(flow); !slices.Equal(got, values[:6]) {
		t.Fatal("outer launcher executor was relabeled as a mounted pressure-volume input")
	}
}

func TestExecutionProfileExecutorMatchesHeldLauncherImage(t *testing.T) {
	newFixture := func(t *testing.T) *executionProfileExecutorCustody {
		t.Helper()
		parent, _ := inputCustodyTestFixture(t)
		copy := inputCustodyTestSpec(t, "t422-execute", "/usr/bin/true", true)
		input, err := inputCustodyTestProtect(t, t.Context(), parent, []ExecutionInputCopy{copy})
		if err != nil {
			t.Fatal(err)
		}
		path, err := input.Check(t.Context(), copy.Name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = file.Close() })
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		livenessFile, err := os.Open(os.DevNull)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = livenessFile.Close() })
		identity := ExecutionToolIdentity{Role: copy.Name, FileType: regularFileType, SHA256: copy.SHA256}
		alive, cancel := context.WithCancel(t.Context())
		t.Cleanup(cancel)
		launcher := &executionProfileLauncherCustody{parent: &executionParentLiveness{
			file: livenessFile,
			image: &executionHeldImage{file: file, info: info, path: path,
				pathSHA256: strings.TrimPrefix(SHA256([]byte(path)), "sha256:"), digest: copy.SHA256},
			inner: t4013.NativeProcessRecord{PID: os.Getpid()}, outer: t4013.NativeProcessRecord{PID: os.Getppid()},
			alive: alive, cancel: cancel, done: make(chan error, 1),
		}}
		return &executionProfileExecutorCustody{launcher: launcher, identity: identity}
	}
	for _, test := range []struct {
		name   string
		mutate func(*executionProfileExecutorCustody)
		wantOK bool
	}{
		{name: "exact", wantOK: true},
		{name: "image_digest", mutate: func(value *executionProfileExecutorCustody) {
			value.launcher.parent.image.digest = SHA256([]byte("other"))
		}},
		{name: "executor_digest", mutate: func(value *executionProfileExecutorCustody) {
			value.identity.SHA256 = SHA256([]byte("other"))
		}},
		{name: "executor_role", mutate: func(value *executionProfileExecutorCustody) { value.identity.Role = "phebs" }},
		{name: "image_path", mutate: func(value *executionProfileExecutorCustody) {
			value.launcher.parent.image.path += "-other"
		}},
		{name: "other_inner", mutate: func(value *executionProfileExecutorCustody) { value.launcher.parent.inner.PID++ }},
		{name: "canceled_liveness", mutate: func(value *executionProfileExecutorCustody) { value.launcher.parent.cancel() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := newFixture(t)
			if test.mutate != nil {
				test.mutate(executor)
			}
			_, err := executor.check(t.Context())
			if (err == nil) != test.wantOK {
				t.Fatal("executor/launcher cross-check result", err)
			}
		})
	}
	newFlow := func(builds *ExecutionGoBuildCustody) *ExecutionEpochOne {
		return &ExecutionEpochOne{plan: Plan{Schema: PlanV3Schema},
			epochs: &ExecutionEpochConfigCustody{author: &ExecutionAuthorCustody{request: ExecutionAuthorRequest{Builds: builds}}}}
	}
	executor := newFixture(t)
	builds := &ExecutionGoBuildCustody{}
	late := newFlow(builds)
	late.workspace = &productionRoot{}
	if err := late.bindProfileExecutor(t.Context(), executor.launcher.parent); err == nil || late.profileExecutor != nil {
		t.Fatal("post-workspace executor binding succeeded")
	}

	t.Run("held image close races observation safely", func(t *testing.T) {
		executor := newFixture(t)
		image := executor.launcher.parent.image
		start := make(chan struct{})
		var workers sync.WaitGroup
		for range 8 {
			workers.Add(1)
			go func() {
				defer workers.Done()
				<-start
				for range 100 {
					_, _ = executor.check(t.Context())
				}
			}()
		}
		close(start)
		if err := image.Close(); err != nil {
			t.Fatal(err)
		}
		workers.Wait()
		if _, err := executor.check(t.Context()); err == nil {
			t.Fatal("closed held image retained launcher authority")
		}
	})

	t.Run("eligible failed bind is spent", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			ctx    func(*testing.T) context.Context
			mutate func(*executionParentLiveness)
		}{
			{name: "go custody precheck", ctx: func(t *testing.T) context.Context { return t.Context() }},
			{name: "nil caller", ctx: func(*testing.T) context.Context { return nil }},
			{name: "canceled caller", ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx
			}},
			{name: "canceled launcher", ctx: func(t *testing.T) context.Context { return t.Context() }, mutate: func(value *executionParentLiveness) {
				value.cancel()
			}},
		} {
			t.Run(test.name, func(t *testing.T) {
				executor := newFixture(t)
				if test.mutate != nil {
					test.mutate(executor.launcher.parent)
				}
				flow := newFlow(&ExecutionGoBuildCustody{})
				if err := flow.bindProfileExecutor(test.ctx(t), executor.launcher.parent); err == nil || flow.profileExecutor == nil {
					t.Fatal("failed eligible bind did not publish spent sentinel", err)
				}
				spent := flow.profileExecutor
				if err := flow.bindProfileExecutor(t.Context(), newFixture(t).launcher.parent); err == nil || flow.profileExecutor != spent {
					t.Fatal("failed bind was retried or replaced", err)
				}
				if _, err := flow.profileExecutor.check(t.Context()); err == nil {
					t.Fatal("spent sentinel issued executor authority")
				}
			})
		}
	})
}

type observedProfileIssuerFixture struct {
	plan          Plan
	tools         []ExecutionToolIdentity
	host          ExecutionHost
	configDigests []string
	configDigest  string
	environment   executionRuntimeEnvironmentObservation
	commands      []ExecutionCommandProfile
	runtime       *executionRuntimeObservation
	phebsPath     string
	directory     string
	preimages     executionObservedProfilePreimages
	namespace     executionSignerNamespaceBinding
}

func newObservedProfileIssuerFixture(t *testing.T) observedProfileIssuerFixture {
	t.Helper()
	plan := accountingTestPlan(t)
	commits := executionFreezeTestCommits()
	commits.T422SourceCommit = plan.SourceCommit
	tools, host := executionFreezeTestTools(plan, commits), executionFreezeTestHost()
	admitted := executionProfileTestAdmission(t, plan, tools, host)
	namespaceRoot := filepath.Join(t.TempDir(), "signer")
	if err := os.Mkdir(namespaceRoot, 0o700); err != nil {
		t.Fatal("create signer namespace", err)
	}
	namespaceRoot, err := filepath.EvalSymlinks(namespaceRoot)
	if err != nil {
		t.Fatal("canonicalize signer namespace", err)
	}
	custody, err := holdExecutionSignerNamespace(context.Background(), namespaceRoot)
	if err != nil {
		t.Fatal("hold signer namespace", err)
	}
	t.Cleanup(func() { _ = custody.Close() })
	namespace, err := custody.check(context.Background())
	if err != nil {
		t.Fatal("check signer namespace", err)
	}
	admitted.signerNamespaceSHA256 = namespace.digest
	profile, commandsSHA256, err := assembleExecutionProfile(plan, tools, host, admitted)
	if err != nil {
		t.Fatal("assemble rebound profile", err)
	}
	admitted.commandsSHA256 = commandsSHA256
	admitted.invocationSHA256 = profile.InvocationSHA256
	admitted.profileSHA256, err = canonicalSHA256(profile)
	if err != nil {
		t.Fatal("hash rebound profile", err)
	}
	profile, err = expectedExecutionProfile(plan, tools, host, admitted)
	if err != nil {
		t.Fatal("build expected profile", err)
	}
	environment := executionRuntimeEnvironmentObservation{
		Recovery: slices.Clone(profile.Environment.BaseVariables), Server: executionProfileServerEnvironment(profile.Environment),
	}
	environment.RecoverySHA256 = executionEnvironmentSHA256(environment.Recovery)
	environment.ServerSHA256 = executionEnvironmentSHA256(environment.Server)
	path, directory := "/private/tmp/t422-phebs", "/private/tmp/t422-data"
	raw, err := json.Marshal(exactExecutionRuntimeFacts(profile))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, '\n')
	stdout := &checkoutCommandOutput{}
	_, _ = stdout.buffer.Write(raw)
	commandSHA256, err := executionRuntimeCommandSHA256(path, directory)
	if err != nil {
		t.Fatal(err)
	}
	phebsIndex := slices.IndexFunc(tools, func(value ExecutionToolIdentity) bool { return value.Role == "phebs" })
	return observedProfileIssuerFixture{
		plan: plan, tools: tools, host: host, namespace: namespace,
		configDigests: slices.Clone(admitted.epochConfigBytesSHA256), configDigest: admitted.configBytesSHA256,
		environment: environment, commands: frozenExecutionCommands(), phebsPath: path, directory: directory,
		preimages: executionObservedProfilePreimages{
			commandsSHA256: admitted.commandsSHA256, harnessCommandSetSHA256: admitted.commandsSHA256,
			pressureCommandSetSHA256: admitted.pressureCommandSetSHA256, rootVolumeBindingsSHA256: admitted.rootVolumeBindingsSHA256,
		},
		runtime: &executionRuntimeObservation{
			Identity: tools[phebsIndex], Path: path, Directory: directory, Deadline: time.Now().Add(time.Minute),
			CommandSHA256: commandSHA256, RawSHA256: SHA256(raw), Facts: exactExecutionRuntimeFacts(profile),
			Observed: true, Complete: true, PID: os.Getpid(), RootStarted: true, RootJoined: true, SessionEmpty: true,
			waited: make(chan error, 1), stdout: stdout, stderr: &checkoutCommandOutput{},
		},
	}
}

func (value observedProfileIssuerFixture) issue() (ExecutionProfile, ExecutionProfileAdmissionBinding, error) {
	return issueObservedExecutionProfile(value.plan, value.tools, value.host, value.configDigests, value.configDigest,
		value.environment, value.commands, value.runtime, value.phebsPath, value.directory, value.preimages, value.namespace)
}

func TestExecutionObservedProfileIssuerCompleteAndMutations(t *testing.T) {
	fixture := newObservedProfileIssuerFixture(t)
	if err := validateExecutionTools(fixture.tools, fixture.plan.ToolPolicy, fixture.plan.SourceCommit); err != nil {
		t.Fatal("fixture tools", err)
	}
	if err := validateExecutionHost(fixture.host, fixture.plan); err != nil {
		t.Fatal("fixture host", err)
	}
	profile, admission, err := fixture.issue()
	if err != nil || profile.Schema != ExecutionProfileV3Schema || !admission.verifiedBeforeOperationalWork || admission.verifiedBeforeWork ||
		admission.profileSHA256 == "" || admission.invocationSHA256 == "" || len(admission.epochConfigBytesSHA256) != 5 {
		t.Fatal("complete observed issuer refused", profile.Schema, admission, err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*observedProfileIssuerFixture)
	}{
		{name: "tool", mutate: func(value *observedProfileIssuerFixture) { value.tools = value.tools[1:] }},
		{name: "host", mutate: func(value *observedProfileIssuerFixture) { value.host.GOOS = "linux" }},
		{name: "config_row", mutate: func(value *observedProfileIssuerFixture) { value.configDigests[0] = SHA256([]byte("other")) }},
		{name: "config_digest", mutate: func(value *observedProfileIssuerFixture) { value.configDigest = SHA256([]byte("other")) }},
		{name: "environment_value", mutate: func(value *observedProfileIssuerFixture) { value.environment.Recovery[0] += "-other" }},
		{name: "environment_digest", mutate: func(value *observedProfileIssuerFixture) { value.environment.ServerSHA256 = SHA256([]byte("other")) }},
		{name: "command", mutate: func(value *observedProfileIssuerFixture) { value.commands[0].Name = "other" }},
		{name: "runtime_fact", mutate: func(value *observedProfileIssuerFixture) { value.runtime.Facts.StoreGenerationMaxAttempts++ }},
		{name: "runtime_raw", mutate: func(value *observedProfileIssuerFixture) { value.runtime.stdout.buffer.WriteByte('x') }},
		{name: "runtime_join", mutate: func(value *observedProfileIssuerFixture) { value.runtime.RootJoined = false }},
		{name: "phebs_path", mutate: func(value *observedProfileIssuerFixture) { value.phebsPath += "-other" }},
		{name: "command_preimage", mutate: func(value *observedProfileIssuerFixture) { value.preimages.commandsSHA256 = SHA256([]byte("other")) }},
		{name: "harness_preimage", mutate: func(value *observedProfileIssuerFixture) {
			value.preimages.harnessCommandSetSHA256 = SHA256([]byte("other"))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newObservedProfileIssuerFixture(t)
			test.mutate(&fixture)
			if got, binding, err := fixture.issue(); err == nil || !reflect.DeepEqual(got, ExecutionProfile{}) || !reflect.DeepEqual(binding, ExecutionProfileAdmissionBinding{}) {
				t.Fatal("mutated observation issued a profile", err)
			}
		})
	}
}

func clonePressureRow(row executionPressureCommandPreimageV1) executionPressureCommandPreimageV1 {
	row.Environment = append([]string(nil), row.Environment...)
	row.NormalizedArgv = append([]string(nil), row.NormalizedArgv...)
	return row
}

func TestExecutionWorkspaceCapabilitiesAreOneShot(t *testing.T) {
	capability := executionWorkspaceCustodyCapability{state: &executionWorkspaceCustodyCapabilityState{proof: &executionWorkspaceCustodyProof{}}}
	copyOfCapability := capability
	var group sync.WaitGroup
	group.Add(2)
	for _, selected := range []*executionWorkspaceCustodyCapability{&capability, &copyOfCapability} {
		go func(capability *executionWorkspaceCustodyCapability) {
			defer group.Done()
			observed, err := capability.ConsumePreimages(t.Context())
			if err == nil || observed != (executionObservedProfilePreimages{}) {
				t.Error("invalid capability issued profile custody")
			}
		}(selected)
	}
	group.Wait()
	capability.state.mu.Lock()
	if capability.state.proof != nil {
		t.Fatal("failed concurrent consumption did not spend capability")
	}
	capability.state.mu.Unlock()
	typeOfCapability := reflect.TypeFor[executionWorkspaceCustodyCapability]()
	for index := range typeOfCapability.NumField() {
		if typeOfCapability.Field(index).IsExported() {
			t.Fatal("private capability exposed serializable custody")
		}
	}
	var reloaded executionWorkspaceCustodyCapability
	if observed, err := reloaded.ConsumePreimages(t.Context()); err == nil || observed != (executionObservedProfilePreimages{}) {
		t.Fatal("zero private capability reconstructed workspace authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	spent := &executionWorkspaceCustodyCapability{state: &executionWorkspaceCustodyCapabilityState{proof: &executionWorkspaceCustodyProof{}}}
	flow := &ExecutionEpochOne{profileWorkspace: spent}
	if profile, admission, handoff, err := flow.issueExecutionProfile(ctx); err == nil || !reflect.DeepEqual(profile, ExecutionProfile{}) ||
		!reflect.DeepEqual(admission, ExecutionProfileAdmissionBinding{}) || handoff != nil {
		t.Fatal("canceled profile issuance succeeded")
	}
	if profile, admission, handoff, err := flow.issueExecutionProfile(t.Context()); err == nil || !reflect.DeepEqual(profile, ExecutionProfile{}) ||
		!reflect.DeepEqual(admission, ExecutionProfileAdmissionBinding{}) || handoff != nil {
		t.Fatal("canceled issuance did not spend workspace custody")
	}
	nilSpent := &executionWorkspaceCustodyCapability{state: &executionWorkspaceCustodyCapabilityState{proof: &executionWorkspaceCustodyProof{}}}
	nilFlow := &ExecutionEpochOne{profileWorkspace: nilSpent}
	//nolint:staticcheck // Deliberately exercise nil-context refusal at the private issuer boundary.
	if profile, admission, handoff, err := nilFlow.issueExecutionProfile(nil); err == nil || !reflect.DeepEqual(profile, ExecutionProfile{}) ||
		!reflect.DeepEqual(admission, ExecutionProfileAdmissionBinding{}) || handoff != nil {
		t.Fatal("nil-context profile issuance succeeded")
	}
	if _, _, _, err := nilFlow.issueExecutionProfile(t.Context()); err == nil {
		t.Fatal("nil-context issuance did not spend workspace custody")
	}
	full := executionOperationalHandoffCapability{state: &executionOperationalHandoffCapabilityState{
		proof: &executionWorkspaceCustodyProof{}, profile: ExecutionProfile{Schema: ExecutionProfileV3Schema},
		admission: ExecutionProfileAdmissionBinding{schema: ExecutionProfileV3Schema, verifiedBeforeOperationalWork: true},
	}}
	fullCopy := full
	if profile, admission, err := full.consumeProfile(ctx); err == nil || !reflect.DeepEqual(profile, ExecutionProfile{}) || !reflect.DeepEqual(admission, ExecutionProfileAdmissionBinding{}) {
		t.Fatal("canceled complete handoff consumption succeeded")
	}
	if profile, admission, err := fullCopy.consumeProfile(t.Context()); err == nil || !reflect.DeepEqual(profile, ExecutionProfile{}) || !reflect.DeepEqual(admission, ExecutionProfileAdmissionBinding{}) {
		t.Fatal("shallow copy reconstructed spent complete handoff")
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[executionOperationalHandoffCapability](), reflect.TypeFor[ExecutionProfileAdmissionBinding]()} {
		for index := range typ.NumField() {
			if typ.Field(index).IsExported() {
				t.Fatal("caller-constructible profile authority field", typ, typ.Field(index).Name)
			}
		}
	}
}
