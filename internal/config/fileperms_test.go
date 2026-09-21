package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFilePermissions(t *testing.T) {
	write := func(t *testing.T, mode os.FileMode) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "phebs.yaml")
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// Chmod explicitly: WriteFile is masked by umask.
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}

	tests := []struct {
		name    string
		mode    os.FileMode
		wantErr bool
	}{
		{"owner-only 0600 passes", 0o600, false},
		{"owner-only 0400 passes", 0o400, false},
		{"owner-only 0700 passes", 0o700, false},
		{"group read 0640 refused", 0o640, true},
		{"other read 0604 refused", 0o604, true},
		{"world readable 0644 refused", 0o644, true},
		{"group/other read 0444 refused", 0o444, true},
		{"other execute 0601 refused", 0o601, true},
		{"group write 0620 refused", 0o620, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := CheckFilePermissions(write(t, tt.mode))
			if tt.wantErr && err == nil {
				t.Fatalf("CheckFilePermissions(%04o) = nil, want error", tt.mode)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CheckFilePermissions(%04o) = %v, want nil", tt.mode, err)
			}
		})
	}
}

func TestCheckFilePermissionsErrorNamesFix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "phebs.yaml")
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	err := CheckFilePermissions(path)
	var insecure *InsecurePermissionsError
	if !errors.As(err, &insecure) {
		t.Fatalf("err = %T (%v), want *InsecurePermissionsError", err, err)
	}
	if insecure.Path != path {
		t.Fatalf("Path = %q, want %q", insecure.Path, path)
	}
	for _, want := range []string{"0644", "chmod 600", "--allow-insecure-config-perms"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err.Error(), want)
		}
	}
}

func TestCheckFilePermissionsMissingFile(t *testing.T) {
	if err := CheckFilePermissions(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("CheckFilePermissions(missing) = nil, want error")
	}
}
