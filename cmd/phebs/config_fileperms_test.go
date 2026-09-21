package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/config"
)

func writeConfigFixture(t *testing.T, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "phebs.yaml")
	if err := os.WriteFile(path, []byte("server:\n  data_dir: /tmp/phebs-test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestEnforceConfigFilePermissions(t *testing.T) {
	tests := []struct {
		name          string
		mode          os.FileMode
		allowInsecure bool
		wantErr       bool
	}{
		{"owner-only enforced", 0o600, false, false},
		{"insecure refused by default", 0o644, false, true},
		{"insecure allowed with escape hatch", 0o644, true, false},
		{"group-only insecure refused", 0o660, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceConfigFilePermissions(writeConfigFixture(t, tt.mode), tt.allowInsecure)
			if tt.wantErr && err == nil {
				t.Fatal("enforceConfigFilePermissions = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("enforceConfigFilePermissions = %v, want nil", err)
			}
			if tt.wantErr {
				var insecure *config.InsecurePermissionsError
				if !errors.As(err, &insecure) {
					t.Fatalf("err = %T (%v), want *config.InsecurePermissionsError", err, err)
				}
			}
		})
	}
}

func TestEnforceConfigFilePermissionsMissingFile(t *testing.T) {
	for _, allow := range []bool{false, true} {
		err := enforceConfigFilePermissions(filepath.Join(t.TempDir(), "missing.yaml"), allow)
		if err == nil {
			t.Fatalf("enforceConfigFilePermissions(missing, %v) = nil, want stat error", allow)
		}
		var insecure *config.InsecurePermissionsError
		if errors.As(err, &insecure) {
			t.Fatalf("missing file surfaced as InsecurePermissionsError: %v", err)
		}
	}
}

func TestLoadServerConfigRefusesInsecurePermissions(t *testing.T) {
	if _, _, err := loadServerConfig(writeConfigFixture(t, 0o644), false); err == nil {
		t.Fatal("loadServerConfig(insecure) = nil error, want refusal")
	}
	if _, _, err := loadServerConfig(writeConfigFixture(t, 0o644), true); err != nil {
		t.Fatalf("loadServerConfig(insecure, allow) = %v, want nil", err)
	}
	if _, _, err := loadRecoveryConfig(writeConfigFixture(t, 0o644), false); err == nil {
		t.Fatal("loadRecoveryConfig(insecure) = nil error, want refusal")
	}
}
