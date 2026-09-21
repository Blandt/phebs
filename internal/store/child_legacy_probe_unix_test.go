//go:build darwin || linux

package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestResolveChildPassRefusesFIFOWithoutOpening(t *testing.T) {
	const fixtureEnv = "PHEBS_TEST_CREDENTIAL_FIFO_DIR"
	if dataDir := os.Getenv(fixtureEnv); dataDir != "" {
		if _, err := resolveChildPass(t.Context(), dataDir); err == nil {
			t.Fatal("FIFO accepted as a database directory")
		}
		return
	}
	dataDir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dataDir, "db"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A regression could block in open(2), ignoring context cancellation.
	// Isolate that call in a bounded, joined test process with no engine.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestResolveChildPassRefusesFIFOWithoutOpening$")
	cmd.Env = append(os.Environ(), fixtureEnv+"="+dataDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("FIFO refusal did not finish safely: %v\n%s", err, output)
	}
}
