//go:build !darwin

package t421

import "context"

func createExecutionReturnedPackage(context.Context, Plan, Receipt, ExecutionFreezeBinding, *executionSignerSealCustody) ([]byte, ReturnedPackageBinding, error) {
	return nil, ReturnedPackageBinding{}, ErrExecutionEpochOne
}
