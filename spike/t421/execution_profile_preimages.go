package t421

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
)

const (
	executionPressureCommandSetPreimageV1Schema = "t422-pressure-command-set-preimage-v1"
	executionRootVolumeBindingsPreimageV1Schema = "t422-root-volume-bindings-preimage-v1"
)

// These types are private canonical preimages. Paths and the raw attach device
// never enter ExecutionProfile or returned ceremony evidence.
type executionPressureCommandSetPreimageV1 struct {
	Schema       string                               `json:"schema"`
	Tool         ExecutionToolIdentity                `json:"tool"`
	ToolPath     string                               `json:"tool_path"`
	AttachDevice string                               `json:"attach_device"`
	Commands     []executionPressureCommandPreimageV1 `json:"commands"`
}

type executionPressureCommandPreimageV1 struct {
	Name             string   `json:"name"`
	WorkingDirectory string   `json:"working_directory"`
	Environment      []string `json:"environment"`
	NormalizedArgv   []string `json:"normalized_argv"`
}

type executionRootVolumeBindingsPreimageV1 struct {
	Schema   string                                 `json:"schema"`
	Bindings []executionRootVolumeBindingPreimageV1 `json:"bindings"`
}

type executionRootVolumeBindingPreimageV1 struct {
	RootRole       string `json:"root_role"`
	VolumeIdentity string `json:"volume_identity"`
}

// Four private fields carry three distinct preimage digests: the one actual
// command digest is deliberately shared by the first two fields.
type executionObservedProfilePreimages struct {
	commandsSHA256           string
	harnessCommandSetSHA256  string
	pressureCommandSetSHA256 string
	rootVolumeBindingsSHA256 string
}

func issueObservedProfilePreimages(
	commands []ExecutionCommandProfile,
	pressure executionPressureCommandSetPreimageV1,
	roots executionRootVolumeBindingsPreimageV1,
) (executionObservedProfilePreimages, error) {
	pressureValid := false
	if len(pressure.Commands) == 3 {
		retained := [2]executionPressureCommandPreimageV1{pressure.Commands[0], pressure.Commands[1]}
		rebuilt, rebuildErr := executionPressureCommandSetPreimage(pressure.Tool, pressure.ToolPath, pressure.Commands[0].WorkingDirectory, pressure.AttachDevice, retained)
		pressureValid = rebuildErr == nil && reflect.DeepEqual(rebuilt, pressure)
	}
	if len(commands) != 3 || commands[0].Name != "backup" || commands[1].Name != "restore" || commands[2].Name != "serve" ||
		!pressureValid ||
		roots.Schema != executionRootVolumeBindingsPreimageV1Schema || !validExecutionRootVolumeBindingRows(roots.Bindings) {
		return executionObservedProfilePreimages{}, errors.New("execution observed profile preimages unavailable or changed")
	}
	commandDigest, err := canonicalSHA256(commands)
	if err != nil {
		return executionObservedProfilePreimages{}, err
	}
	pressureDigest, err := canonicalSHA256(pressure)
	if err != nil {
		return executionObservedProfilePreimages{}, err
	}
	rootDigest, err := canonicalSHA256(roots)
	if err != nil {
		return executionObservedProfilePreimages{}, err
	}
	return executionObservedProfilePreimages{
		commandsSHA256: commandDigest, harnessCommandSetSHA256: commandDigest,
		pressureCommandSetSHA256: pressureDigest, rootVolumeBindingsSHA256: rootDigest,
	}, nil
}

// executionPressureCommandRecipe is the one pure recipe constructor used by
// both native execution and the private observed-profile preimage. Only its
// three path operands are normalized; the executable remains separately bound
// by held ExecutionSystemToolCustody.
func executionPressureCommandRecipe(root, name, device string) ([]string, executionPressureCommandPreimageV1, error) {
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return nil, executionPressureCommandPreimageV1{}, ErrExecutionEpochOne
	}
	image, mount := filepath.Join(root, "pressure.sparseimage"), filepath.Join(root, "mount")
	var args, normalized []string
	switch name {
	case "create":
		args = []string{"create", "-size", "96g", "-layout", "NONE", "-type", "SPARSE", "-fs", "APFS", "-volname", "phebs-t422-private", "-nospotlight", image}
		normalized = append(slices.Clone(args[:len(args)-1]), "@pressure-image")
	case "attach":
		args = []string{"attach", "-owners", "on", "-nobrowse", "-noautoopen", "-mountpoint", mount, "-plist", image}
		normalized = slices.Clone(args)
		normalized[6], normalized[8] = "@pressure-mount", "@pressure-image"
	case "detach":
		if !executionPressureDevice(device) {
			return nil, executionPressureCommandPreimageV1{}, ErrExecutionEpochOne
		}
		args, normalized = []string{"detach", device}, []string{"detach", "@pressure-device"}
	default:
		return nil, executionPressureCommandPreimageV1{}, ErrExecutionEpochOne
	}
	environment := []string{"PATH=/usr/bin:/bin", "LANG=C", "LC_ALL=C", "HOME=" + filepath.Join(root, "home"), "TMPDIR=" + filepath.Join(root, "tmp")}
	return args, executionPressureCommandPreimageV1{Name: name, WorkingDirectory: root, Environment: environment, NormalizedArgv: normalized}, nil
}

func executionPressureCommandSetPreimage(identity ExecutionToolIdentity, toolPath, root, attachDevice string, retained [2]executionPressureCommandPreimageV1) (executionPressureCommandSetPreimageV1, error) {
	if identity.Role != "hdiutil" || identity.FileType != regularFileType || !validExecutionSHA256(identity.SHA256) || identity.Version == "" ||
		identity.Provenance != "external-executed-file-v1" || identity.BuildVCSRevision != "" || identity.BuildVCSModified ||
		identity.ModulePath != "" || identity.ModuleVersion != "" || identity.ModuleSum != "" || identity.BuildRecipeSHA256 != "" ||
		!filepath.IsAbs(toolPath) || toolPath != executionSystemToolPath("hdiutil") || !executionPressureDevice(attachDevice) {
		return executionPressureCommandSetPreimageV1{}, ErrExecutionEpochOne
	}
	rows := make([]executionPressureCommandPreimageV1, 3)
	for index, name := range []string{"create", "attach", "detach"} {
		_, row, err := executionPressureCommandRecipe(root, name, attachDevice)
		if err != nil || index < 2 && !equalExecutionPressureCommandPreimages(row, retained[index]) {
			return executionPressureCommandSetPreimageV1{}, ErrExecutionEpochOne
		}
		rows[index] = row
	}
	return executionPressureCommandSetPreimageV1{Schema: executionPressureCommandSetPreimageV1Schema,
		Tool: identity, ToolPath: toolPath, AttachDevice: attachDevice, Commands: rows}, nil
}

func executionPressureDevice(device string) bool {
	if !strings.HasPrefix(device, "/dev/disk") || len(device) > 32 {
		return false
	}
	for _, number := range strings.Split(strings.TrimPrefix(device, "/dev/disk"), "s") {
		if number == "" || len(number) > 1 && number[0] == '0' {
			return false
		}
		for _, digit := range number {
			if digit < '0' || digit > '9' {
				return false
			}
		}
	}
	return strings.Count(device, "s") <= 2
}

func validExecutionRootVolumeBindingRows(bindings []executionRootVolumeBindingPreimageV1) bool {
	roles := executionRootVolumeBindingRoles()
	if len(bindings) != len(roles) {
		return false
	}
	for index, binding := range bindings {
		if binding.RootRole != roles[index] || !validExecutionSHA256(binding.VolumeIdentity) ||
			index > 0 && binding.VolumeIdentity != bindings[1].VolumeIdentity {
			return false
		}
	}
	return bindings[0].VolumeIdentity != bindings[1].VolumeIdentity
}

func executionRootVolumeBindingRoles() [9]string {
	return [9]string{
		"pressure-image-on-admitted-backing-volume-v1",
		"authored-source-on-mounted-pressure-volume-v1",
		"config-and-catalog-on-mounted-pressure-volume-v1",
		"server-data-on-mounted-pressure-volume-v1",
		"archive-on-mounted-pressure-volume-v1",
		"private-home-on-mounted-pressure-volume-v1",
		"private-temp-on-mounted-pressure-volume-v1",
		"tool-outputs-on-mounted-pressure-volume-v1",
		"pressure-ballast-on-mounted-pressure-volume-v1",
	}
}

func executionRootVolumeBindingsPreimage(backing, data, ballast [2]int32) (executionRootVolumeBindingsPreimageV1, error) {
	if backing == ([2]int32{}) || data == ([2]int32{}) || ballast == ([2]int32{}) || backing == data || ballast != data {
		return executionRootVolumeBindingsPreimageV1{}, errors.New("execution root volume bindings unavailable or changed")
	}
	roles := executionRootVolumeBindingRoles()
	bindings := make([]executionRootVolumeBindingPreimageV1, len(roles))
	for index, role := range roles {
		volume := data
		if index == 0 {
			volume = backing
		} else if index == len(roles)-1 {
			volume = ballast
		}
		bindings[index] = executionRootVolumeBindingPreimageV1{RootRole: role, VolumeIdentity: executionFSIDIdentity(volume)}
	}
	return executionRootVolumeBindingsPreimageV1{Schema: executionRootVolumeBindingsPreimageV1Schema, Bindings: bindings}, nil
}

func executionRootVolumeBindingsFromObservation(host executionHostObservation, observed [9][2]int32) (executionRootVolumeBindingsPreimageV1, error) {
	want := [9][2]int32{host.FSIDs[0], host.FSIDs[1], host.FSIDs[1], host.FSIDs[1], host.FSIDs[1], host.FSIDs[1], host.FSIDs[1], host.FSIDs[1], host.FSIDs[2]}
	if observed != want || host.Host.BackingVolumeIdentity != executionFSIDIdentity(host.FSIDs[0]) ||
		host.Host.DataVolumeIdentity != executionFSIDIdentity(host.FSIDs[1]) || host.Host.BallastVolumeIdentity != executionFSIDIdentity(host.FSIDs[2]) {
		return executionRootVolumeBindingsPreimageV1{}, errors.New("execution root volume observations unavailable or changed")
	}
	return executionRootVolumeBindingsPreimage(host.FSIDs[0], host.FSIDs[1], host.FSIDs[2])
}

func equalExecutionPressureCommandPreimages(a, b executionPressureCommandPreimageV1) bool {
	return reflect.DeepEqual(a, b)
}
