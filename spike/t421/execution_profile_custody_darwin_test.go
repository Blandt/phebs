//go:build darwin

package t421

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
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
	if !profileObservationSetComplete(flow) {
		t.Fatal("complete signer and protected-tool custody was refused")
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
			observed, handoff, err := capability.ConsumeForProfile(t.Context())
			if err == nil || observed != (executionObservedProfilePreimages{}) || handoff != nil {
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
	if observed, handoff, err := reloaded.ConsumeForProfile(t.Context()); err == nil || observed != (executionObservedProfilePreimages{}) || handoff != nil {
		t.Fatal("zero private capability reconstructed workspace authority")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	handoff := executionOperationalHandoffCapability{state: &executionOperationalHandoffCapabilityState{proof: &executionWorkspaceCustodyProof{}}}
	copyOfHandoff := handoff
	if observed, err := handoff.consumeWorkspace(ctx); err == nil || observed != (executionObservedProfilePreimages{}) {
		t.Fatal("canceled or repeated handoff consumption succeeded")
	}
	if observed, err := copyOfHandoff.consumeWorkspace(t.Context()); err == nil || observed != (executionObservedProfilePreimages{}) {
		t.Fatal("repeated handoff consumption succeeded")
	}
	handoff.state.mu.Lock()
	if handoff.state.proof != nil {
		t.Fatal("failed handoff did not transfer away its proof")
	}
	handoff.state.mu.Unlock()
}
