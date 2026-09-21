package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanSourceAcceptsExactHEADAndRejectsDirtyCheckout(t *testing.T) {
	root := t.TempDir()
	for _, arguments := range [][]string{
		{"init", "--quiet"},
		{"config", "user.name", "T42.2r Test"},
		{"config", "user.email", "t422r@example.invalid"},
	} {
		runGit(t, root, arguments...)
	}
	path := filepath.Join(root, "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"add", "fixture.go"}, {"commit", "--quiet", "-m", "fixture"}} {
		runGit(t, root, arguments...)
	}

	gotRoot, commit, err := cleanSource(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	actualRoot, err := os.Stat(gotRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(wantRoot, actualRoot) || len(commit) != 40 {
		t.Fatalf("clean source = root %q commit %q", gotRoot, commit)
	}
	runGit(t, root, "update-index", "--assume-unchanged", "fixture.go")
	if err := os.WriteFile(path, []byte("package fixture\n\nvar Dirty = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := cleanSource(t.Context(), root); err == nil || !strings.Contains(err.Error(), "pinned Git blob") {
		t.Fatalf("assume-unchanged checkout refusal = %v", err)
	}
}

func runGit(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v: %s", arguments[0], err, output)
	}
}
