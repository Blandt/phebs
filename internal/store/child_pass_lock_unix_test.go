//go:build unix

package store

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestResolveChildPassLockFileIsStable asserts the credential lock file is
// created once and never unlinked/recreated: its inode must survive
// resolutions. Unlinking and recreating the lock file would let two processes
// hold locks on different inodes at the same time.
func TestResolveChildPassLockFileIsStable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dataDir := t.TempDir()
	if _, err := resolveChildPass(ctx, dataDir); err != nil {
		t.Fatal(err)
	}
	ino := func() uint64 {
		t.Helper()
		st, err := os.Stat(filepath.Join(dataDir, childPassLockName))
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok {
			t.Skip("inode not available on this platform")
		}
		return stat.Ino
	}
	first := ino()
	if _, err := resolveChildPass(ctx, dataDir); err != nil {
		t.Fatal(err)
	}
	if second := ino(); second != first {
		t.Fatal("credential lock file was unlinked and recreated")
	}
}
