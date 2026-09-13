//go:build !darwin

package t421

import (
	"context"
	"time"
)

func sendExecutionAuthorization(context.Context, string, []byte, time.Time) error {
	return errExecutionAuthorization
}
