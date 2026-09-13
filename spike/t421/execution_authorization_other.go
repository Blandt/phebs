//go:build !darwin

package t421

import (
	"context"
	"time"
)

type executionAuthorizationWait struct{}

func prepareExecutionAuthorization(context.Context, productionRoot, time.Time) (*executionAuthorizationWait, error) {
	return nil, errExecutionAuthorization
}

func (*executionAuthorizationWait) consume(context.Context, executionAuthorizationV1) (executionAuthorizationPeer, error) {
	return executionAuthorizationPeer{}, errExecutionAuthorization
}

func (*executionAuthorizationWait) close() error {
	return errExecutionAuthorization
}

func sendExecutionAuthorization(context.Context, string, []byte, time.Time) error {
	return errExecutionAuthorization
}
