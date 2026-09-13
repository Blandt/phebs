//go:build !darwin

package t421

import (
	"context"
	"path/filepath"
	"strings"
	"time"
)

func runExecutionOuter(context.Context, time.Time, string, string, []string) error {
	return ErrExecutionLauncher
}

func runExecutionInner(context.Context, time.Time, string, string, []string) error {
	return ErrExecutionLauncher
}

func validExecutionLauncherPath(path string) bool {
	return len(path) > 0 && len(path) <= 1_023 && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}
