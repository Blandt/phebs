//go:build !darwin

package t421

import (
	"context"
	"time"
)

func runExecutionOuter(context.Context, time.Time, string, string, []string) error {
	return ErrExecutionLauncher
}

func runExecutionInner(context.Context, time.Time, string, string, []string) error {
	return ErrExecutionLauncher
}
