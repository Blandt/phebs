package config

import (
	"fmt"
	"os"
)

// insecureConfigModeBits are the group/other permission bits that must never
// be set on a config file. The config can hold API keys, OIDC client secrets,
// webhook secrets, and connection tokens, so anything beyond owner-only
// access is a silent secret leak. This matches the 0o600 convention already
// used for SurrealDB rendezvous files, backup artifacts, and index archives.
const insecureConfigModeBits = os.FileMode(0o077)

// InsecurePermissionsError reports that a config file granting group or other
// access was refused. The message names the remediation.
type InsecurePermissionsError struct {
	Path string
	Mode os.FileMode
}

func (e *InsecurePermissionsError) Error() string {
	return fmt.Sprintf(
		"insecure config file permissions %04o on %s: the config may hold API keys, OIDC client secrets, webhook secrets, and connection tokens; run chmod 600 %s or pass --allow-insecure-config-perms",
		e.Mode.Perm(), e.Path, e.Path,
	)
}

// CheckFilePermissions stats the config file at path and refuses it when any
// group or other read/write/execute bit is set (mode & 0077 != 0). Callers
// must invoke this only when the config comes from an actual file path —
// never for stdin, embedded bytes, or defaults.
func CheckFilePermissions(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat config: %w", err)
	}
	if info.Mode()&insecureConfigModeBits != 0 {
		return &InsecurePermissionsError{Path: path, Mode: info.Mode()}
	}
	return nil
}
