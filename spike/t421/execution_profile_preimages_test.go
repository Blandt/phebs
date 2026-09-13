package t421

import (
	"reflect"
	"testing"
)

func TestExecutionObservedProfilePreimages(t *testing.T) {
	commands := frozenExecutionCommands()
	var retained [2]executionPressureCommandPreimageV1
	for index, name := range []string{"create", "attach"} {
		_, retained[index], _ = executionPressureCommandRecipe("/private/t422-pressure", name, "/dev/disk4")
	}
	pressure, err := executionPressureCommandSetPreimage(
		ExecutionToolIdentity{Role: "hdiutil", FileType: regularFileType, SHA256: SHA256([]byte("hdiutil")), Version: "host", Provenance: "external-executed-file-v1"},
		"/usr/bin/hdiutil", "/private/t422-pressure", "/dev/disk4", retained,
	)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := executionRootVolumeBindingsPreimage([2]int32{1, -2}, [2]int32{3, -4}, [2]int32{3, -4})
	if err != nil {
		t.Fatal(err)
	}
	got, err := issueObservedProfilePreimages(commands, pressure, roots)
	wantCommands, _ := canonicalSHA256(commands)
	wantPressure, _ := canonicalSHA256(pressure)
	wantRoots, _ := canonicalSHA256(roots)
	commandRaw, _ := MarshalCanonical(commands)
	pressureRaw, _ := MarshalCanonical(pressure)
	rootRaw, _ := MarshalCanonical(roots)
	const commandJSON = `[
  {
    "name": "backup",
    "tool_role": "phebs",
    "environment_class": "recovery",
    "normalized_argv": [
      "backup",
      "-config",
      "@config",
      "-output",
      "@backup"
    ]
  },
  {
    "name": "restore",
    "tool_role": "phebs",
    "environment_class": "recovery",
    "normalized_argv": [
      "restore",
      "-config",
      "@config",
      "-backup",
      "@backup"
    ]
  },
  {
    "name": "serve",
    "tool_role": "phebs",
    "environment_class": "server",
    "normalized_argv": [
      "serve",
      "-config",
      "@config"
    ]
  }
]
`
	const pressureJSON = `{
  "schema": "t422-pressure-command-set-preimage-v1",
  "tool": {
    "role": "hdiutil",
    "file_type": "regular",
    "sha256": "sha256:bb4c7f331191c7d62603891f59364b10053cc4258cb608de1a65e776c9a7388b",
    "version": "host",
    "provenance": "external-executed-file-v1",
    "build_vcs_modified": false
  },
  "tool_path": "/usr/bin/hdiutil",
  "attach_device": "/dev/disk4",
  "commands": [
    {
      "name": "create",
      "working_directory": "/private/t422-pressure",
      "environment": [
        "PATH=/usr/bin:/bin",
        "LANG=C",
        "LC_ALL=C",
        "HOME=/private/t422-pressure/home",
        "TMPDIR=/private/t422-pressure/tmp"
      ],
      "normalized_argv": [
        "create",
        "-size",
        "96g",
        "-layout",
        "NONE",
        "-type",
        "SPARSE",
        "-fs",
        "APFS",
        "-volname",
        "phebs-t422-private",
        "-nospotlight",
        "@pressure-image"
      ]
    },
    {
      "name": "attach",
      "working_directory": "/private/t422-pressure",
      "environment": [
        "PATH=/usr/bin:/bin",
        "LANG=C",
        "LC_ALL=C",
        "HOME=/private/t422-pressure/home",
        "TMPDIR=/private/t422-pressure/tmp"
      ],
      "normalized_argv": [
        "attach",
        "-owners",
        "on",
        "-nobrowse",
        "-noautoopen",
        "-mountpoint",
        "@pressure-mount",
        "-plist",
        "@pressure-image"
      ]
    },
    {
      "name": "detach",
      "working_directory": "/private/t422-pressure",
      "environment": [
        "PATH=/usr/bin:/bin",
        "LANG=C",
        "LC_ALL=C",
        "HOME=/private/t422-pressure/home",
        "TMPDIR=/private/t422-pressure/tmp"
      ],
      "normalized_argv": [
        "detach",
        "@pressure-device"
      ]
    }
  ]
}
`
	const rootJSON = `{
  "schema": "t422-root-volume-bindings-preimage-v1",
  "bindings": [
    {
      "root_role": "pressure-image-on-admitted-backing-volume-v1",
      "volume_identity": "sha256:3ad3dd869e4b5825bb4f49bbf5c8d1cf3a4a0bb90f124324bc139c86ca38b093"
    },
    {
      "root_role": "authored-source-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "config-and-catalog-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "server-data-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "archive-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "private-home-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "private-temp-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "tool-outputs-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    },
    {
      "root_role": "pressure-ballast-on-mounted-pressure-volume-v1",
      "volume_identity": "sha256:f7fd9c4d7a9c1bada127cb5f43aa6004c2879b516e4e6cecbe30751a5f3cc0f9"
    }
  ]
}
`
	if string(commandRaw) != commandJSON || string(pressureRaw) != pressureJSON || string(rootRaw) != rootJSON ||
		wantCommands != "sha256:ad215f78b36cfb6a25a67c23814568169c7759ae5c5e75416edbee6441232c2d" ||
		wantPressure != "sha256:58f078b277525ae842586e0cb7fd9a05581ba1c706cbdecc38cd594a26da48ad" ||
		wantRoots != "sha256:10f90b8a5596652ede61f0a5926e6d7f41067f37ed7085e23684129cc171c658" {
		t.Fatal("canonical observed-profile preimage bytes or digest changed")
	}
	if err != nil || got.commandsSHA256 != wantCommands || got.harnessCommandSetSHA256 != wantCommands ||
		got.pressureCommandSetSHA256 != wantPressure || got.rootVolumeBindingsSHA256 != wantRoots {
		t.Fatal("actual preimages were not hashed exactly once into the required fields", got, err)
	}
	if !reflect.DeepEqual(roots.Bindings, []executionRootVolumeBindingPreimageV1{
		{RootRole: "pressure-image-on-admitted-backing-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{1, -2})},
		{RootRole: "authored-source-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "config-and-catalog-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "server-data-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "archive-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "private-home-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "private-temp-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "tool-outputs-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
		{RootRole: "pressure-ballast-on-mounted-pressure-volume-v1", VolumeIdentity: executionFSIDIdentity([2]int32{3, -4})},
	}) {
		t.Fatal("root-role order or FSID projection changed")
	}
	for _, mutate := range []func(*executionPressureCommandSetPreimageV1, *executionRootVolumeBindingsPreimageV1){
		func(p *executionPressureCommandSetPreimageV1, _ *executionRootVolumeBindingsPreimageV1) {
			p.Schema = "wrong"
		},
		func(p *executionPressureCommandSetPreimageV1, _ *executionRootVolumeBindingsPreimageV1) {
			p.Commands[1].Name = "wrong"
		},
		func(_ *executionPressureCommandSetPreimageV1, r *executionRootVolumeBindingsPreimageV1) {
			r.Schema = "wrong"
		},
		func(_ *executionPressureCommandSetPreimageV1, r *executionRootVolumeBindingsPreimageV1) {
			r.Bindings = r.Bindings[:8]
		},
		func(_ *executionPressureCommandSetPreimageV1, r *executionRootVolumeBindingsPreimageV1) {
			r.Bindings[4].RootRole = "wrong"
		},
	} {
		p, r := pressure, roots
		p.Commands, r.Bindings = append([]executionPressureCommandPreimageV1(nil), pressure.Commands...), append([]executionRootVolumeBindingPreimageV1(nil), roots.Bindings...)
		mutate(&p, &r)
		if got, err := issueObservedProfilePreimages(commands, p, r); err == nil || got != (executionObservedProfilePreimages{}) {
			t.Fatal("invalid preimage issued digests")
		}
	}
	for index := range roots.Bindings {
		for _, field := range []string{"role", "identity"} {
			changed := roots
			changed.Bindings = append([]executionRootVolumeBindingPreimageV1(nil), roots.Bindings...)
			if field == "role" {
				changed.Bindings[index].RootRole += "-other"
			} else if index == 0 {
				changed.Bindings[index].VolumeIdentity = changed.Bindings[1].VolumeIdentity
			} else {
				changed.Bindings[index].VolumeIdentity = SHA256([]byte("other-volume"))
			}
			if got, err := issueObservedProfilePreimages(commands, pressure, changed); err == nil || got != (executionObservedProfilePreimages{}) {
				t.Fatalf("changed root role %d %s issued profile preimages", index, field)
			}
		}
	}
	backing, data := [2]int32{1, -2}, [2]int32{3, -4}
	host := executionHostObservation{FSIDs: [3][2]int32{backing, data, data}, Host: ExecutionHost{
		BackingVolumeIdentity: executionFSIDIdentity(backing), DataVolumeIdentity: executionFSIDIdentity(data), BallastVolumeIdentity: executionFSIDIdentity(data),
	}}
	observedRoots := [9][2]int32{backing, data, data, data, data, data, data, data, data}
	if got, err := executionRootVolumeBindingsFromObservation(host, observedRoots); err != nil || !reflect.DeepEqual(got, roots) {
		t.Fatal("exact nine-role observation refused", err)
	}
	for index := range observedRoots {
		changed := observedRoots
		changed[index][0]++
		if got, err := executionRootVolumeBindingsFromObservation(host, changed); err == nil || !reflect.DeepEqual(got, executionRootVolumeBindingsPreimageV1{}) {
			t.Fatal("changed current FSID issued root bindings", index)
		}
	}
}

func TestExecutionRootVolumeBindingsRefuseInvalidTopology(t *testing.T) {
	for _, values := range [][3][2]int32{
		{},
		{{1, 2}, {1, 2}, {1, 2}},
		{{1, 2}, {3, 4}, {5, 6}},
	} {
		if got, err := executionRootVolumeBindingsPreimage(values[0], values[1], values[2]); err == nil || !reflect.DeepEqual(got, executionRootVolumeBindingsPreimageV1{}) {
			t.Fatal("invalid topology issued root bindings", values)
		}
	}
}
