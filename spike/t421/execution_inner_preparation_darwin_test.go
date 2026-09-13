//go:build darwin

package t421

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestExecutionOperationalRootIsPrivateShortAndOwned(t *testing.T) {
	selection, _ := testExecutionSelection(t)
	root, err := createExecutionOperationalRoot(selection)
	if err != nil {
		t.Fatal(err)
	}
	path := root.path
	if filepath.Dir(path) != "/private/tmp" || len(filepath.Join(path, executionAuthorizationSocketName)) > maxExecutionAuthSocketPathBytes ||
		pressureRootsUnchanged(root) != nil {
		t.Fatal("operational root is not the exact short held directory")
	}
	if err := closeExecutionOperationalRoot(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("closed operational root remains", err)
	}
}
