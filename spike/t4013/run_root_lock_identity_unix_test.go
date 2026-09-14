//go:build darwin || linux

package t4013

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRootLockStatBindsHeldInode(t *testing.T) {
	for _, mode := range []string{"current", "replaced before capture", "closed"} {
		t.Run(mode, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			lock, err := lockRunRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Close() }()
			path := filepath.Join(root, runRootLockName)
			held, err := lock.file.Stat()
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "replaced before capture":
				if os.Rename(path, path+".prior") != nil || os.WriteFile(path, nil, 0600) != nil {
					t.Fatal("replace lock")
				}
			case "closed":
				if err := lock.Close(); err != nil {
					t.Fatal(err)
				}
			}
			got, err := lock.Stat()
			if mode == "current" {
				if err != nil || !os.SameFile(held, got) {
					t.Fatal("lost held inode", err)
				}
			} else if err == nil || got != nil {
				t.Fatal("uncertain lock supplied identity")
			}
		})
	}
}
