//go:build !darwin

package t421

import "context"

func createExecutionSignerSeal(
	context.Context,
	*executionSignerSealCustody,
	Plan,
	executionFreezeCandidatePreparation,
) (ExecutionFreeze, error) {
	return ExecutionFreeze{}, ErrExecutionEpochOne
}

func verifyExecutionSignerSealAndIssue(context.Context, *executionSignerSealCustody, Plan) (ExecutionFreezeAdmissionBinding, error) {
	return ExecutionFreezeAdmissionBinding{}, ErrExecutionEpochOne
}

func checkExecutionSignerSeal(context.Context, *executionSignerSealCustody) error {
	return ErrExecutionEpochOne
}

func closeExecutionSignerSeal(*executionSignerSealCustody) error {
	return ErrExecutionEpochOne
}
