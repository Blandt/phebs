//go:build !darwin

package t421

import (
	"context"
	"os"
)

func openExecutionSignerNamespace(string) (*os.File, error) {
	return nil, ErrExecutionEpochOne
}

func observeExecutionSignerNamespace(context.Context, *os.File, string) (executionSignerNamespaceIdentity, error) {
	return executionSignerNamespaceIdentity{}, ErrExecutionEpochOne
}
