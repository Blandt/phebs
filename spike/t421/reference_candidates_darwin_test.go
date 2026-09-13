//go:build darwin

package t421

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionReferenceCandidatesPathAndCleanup(t *testing.T) {
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	parentRoot, err := openProductionRoot(parent)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(parent, "t422-supplied-builds-")
	if err != nil {
		t.Fatal(err)
	}
	root, err := openProductionRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	candidates := &executionReferenceCandidates{parent: parentRoot, root: root}
	for index, role := range executionReferenceCandidateRoles() {
		path := filepath.Join(directory, role)
		if err := os.WriteFile(path, []byte(role), 0o700); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatal(err)
		}
		candidates.paths[index], candidates.infos[index] = path, info
	}
	// Refresh the held directory row after creating its entries.
	candidates.root.info, err = candidates.root.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	for _, role := range executionReferenceCandidateRoles() {
		path, err := candidates.Path(t.Context(), role)
		if err != nil || filepath.Base(path) != role {
			t.Fatalf("candidate %s unavailable: %q, %v", role, path, err)
		}
	}
	if path, err := candidates.Path(t.Context(), "t422-execute"); err != ErrExecutionGoBuildCustody || path != "" {
		t.Fatal("unknown candidate role admitted")
	}
	if err := os.WriteFile(candidates.paths[0], []byte("changed"), 0o700); err != nil {
		t.Fatal(err)
	}
	if path, err := candidates.Path(t.Context(), "t422-author"); err != ErrExecutionGoBuildCustody || path != "" {
		t.Fatal("changed candidate admitted")
	}
	if err := candidates.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("candidate scratch survived cleanup")
	}
}
