package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInputIdentityAndOutputPreflight(t *testing.T) {
	root := t.TempDir()
	regular := filepath.Join(root, "regular")
	if err := os.WriteFile(regular, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := openRegular(regular)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(regular, symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := openRegular(symlink); err == nil {
		t.Fatal("symlink opened as a regular input")
	}
	fifo := filepath.Join(root, "fifo")
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openRegular(fifo); err == nil {
		t.Fatal("FIFO opened as a regular input")
	}

	output := filepath.Join(root, "existing-output")
	if err := os.WriteFile(output, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JEV_KEY", "test-only")
	for name, run := range map[string]func([]string) error{
		"project":  project,
		"classify": classify,
	} {
		t.Run(name, func(t *testing.T) {
			err := run([]string{"-allowlist", regular, "-output", output})
			if !errors.Is(err, os.ErrExist) {
				t.Fatalf("preflight error = %v, want output-exists refusal", err)
			}
		})
	}
}
