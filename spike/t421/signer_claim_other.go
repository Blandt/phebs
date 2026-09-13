//go:build !darwin

package t421

import "context"

func createExecutionSignerCeremonyClaim(
	context.Context,
	executionSignerNamespaceBinding,
	string,
	executionSignerRegistryNames,
	[]byte,
) (*executionSignerCeremonyClaimCustody, error) {
	return nil, ErrExecutionEpochOne
}

func checkExecutionSignerCeremonyClaim(context.Context, *executionSignerCeremonyClaimCustody) error {
	return ErrExecutionEpochOne
}
