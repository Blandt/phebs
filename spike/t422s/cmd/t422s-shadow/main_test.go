package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivateOutputIsCreateOnlyAndOutsideRepository(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(t.TempDir(), "result.jsonl")
	if err := writePrivateCreateOnly(root, output, func(fileWriter io.Writer) error {
		_, err := fileWriter.Write([]byte("ok\n"))
		return err
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("output mode = %o, want 600", info.Mode().Perm())
	}
	if err := writePrivateCreateOnly(root, output, func(io.Writer) error { return nil }); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing output error = %v", err)
	}
	inside := filepath.Join(root, "result.jsonl")
	if err := writePrivateCreateOnly(root, inside, func(io.Writer) error { return nil }); err == nil {
		t.Fatal("output inside source repository was accepted")
	}
}

func TestReadRegularBoundedRejectsOversizeInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "allowlist.json")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 17)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readRegularBounded(path, 16); err == nil {
		t.Fatal("oversize allowlist was accepted")
	}
}
