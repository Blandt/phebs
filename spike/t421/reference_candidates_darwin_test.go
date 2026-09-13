//go:build darwin

package t421

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionReferenceCandidatesPathAndCleanup(t *testing.T) {
	candidates := newExecutionReferenceCandidateTestRoots(t)
	directory := candidates.root.path
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

func newExecutionReferenceCandidateTestRoots(t *testing.T) *executionReferenceCandidates {
	t.Helper()
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
	t.Cleanup(func() {
		_ = root.file.Close()
		_ = parentRoot.file.Close()
	})
	return &executionReferenceCandidates{parent: parentRoot, root: root}
}

// These fixtures model interruption before any supplied image exists. They
// exercise the real descriptor/path cleanup guard without starting a build.
func TestExecutionReferenceCandidatesPartialConstructionCleanup(t *testing.T) {
	for _, state := range []string{"held", "replaced", "missing_identity", "missing_descriptor"} {
		t.Run(state, func(t *testing.T) {
			candidates := newExecutionReferenceCandidateTestRoots(t)
			rootFile, parentFile := candidates.root.file, candidates.parent.file
			directory := candidates.root.path
			if err := os.WriteFile(filepath.Join(directory, "owned"), []byte("owned"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch state {
			case "replaced":
				if err := os.Rename(directory, directory+"-held"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(directory, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(directory, "replacement"), []byte("not ours"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing_identity":
				candidates.root.info = nil
			case "missing_descriptor":
				if err := rootFile.Close(); err != nil {
					t.Fatal(err)
				}
				candidates.root.file = nil
			}
			wantError := state != "held"
			if err := candidates.checkRoots(); (err != nil) != wantError {
				t.Fatal("partial root identity guard result", err)
			}
			for range 2 {
				if err := candidates.Close(); (err != nil) != wantError {
					t.Fatal("partial cleanup lost its result", err)
				}
			}
			for _, file := range []*os.File{rootFile, parentFile} {
				if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatal("partial cleanup left an owned descriptor open", err)
				}
			}
			if !wantError {
				if _, err := os.Lstat(directory); !os.IsNotExist(err) {
					t.Fatal("owned partial scratch survived successful cleanup", err)
				}
				return
			}
			owned := filepath.Join(directory, "owned")
			if state == "replaced" {
				owned = filepath.Join(directory+"-held", "owned")
				if raw, err := os.ReadFile(filepath.Join(directory, "replacement")); err != nil || string(raw) != "not ours" {
					t.Fatal("cleanup touched replacement custody", err)
				}
			}
			if raw, err := os.ReadFile(owned); err != nil || string(raw) != "owned" {
				t.Fatal("uncertain partial custody was removed", err)
			}
		})
	}
}

func TestExecutionReferenceCandidatesFailedSessionCleanupRetainsScratch(t *testing.T) {
	candidates := newExecutionReferenceCandidateTestRoots(t)
	path := filepath.Join(candidates.root.path, "t422-author")
	if err := os.WriteFile(path, []byte("unfinished"), 0o700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	candidates.paths[0], candidates.infos[0] = path, info
	// runReferenceGo's generic error cannot prove the session is empty. Model
	// the retained pre-Start latch, without inventing a successful native join.
	candidates.cleanupUncertain = true
	if got, err := candidates.Path(t.Context(), "t422-author"); err == nil || got != "" {
		t.Fatal("unproven child session released a candidate path")
	}
	for range 2 {
		if err := candidates.Close(); err != ErrExecutionGoBuildCustody {
			t.Fatal("unproven child session cleanup did not retain its error", err)
		}
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "unfinished" {
		t.Fatal("unproven child session scratch was removed", err)
	}
	for _, file := range []*os.File{candidates.root.file, candidates.parent.file} {
		if _, err := file.Stat(); !errors.Is(err, os.ErrClosed) {
			t.Fatal("failed session cleanup left an owned descriptor open", err)
		}
	}
}
