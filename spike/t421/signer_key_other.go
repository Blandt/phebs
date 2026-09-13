//go:build !darwin

package t421

import "context"

func generateExecutionSignerKey(context.Context, *executionSignerKeyCustody) error {
	return ErrExecutionEpochOne
}

func checkExecutionSignerKey(context.Context, *executionSignerKeyCustody) error {
	return ErrExecutionEpochOne
}

func closeExecutionSignerKey(*executionSignerKeyCustody) error {
	return ErrExecutionEpochOne
}
