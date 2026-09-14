package t421

import "errors"

var errExecutionRevisionResults = errors.New("execution revision results are incomplete")

// composeExecutionRevisionResults projects the three joined author children
// into receipt evidence, including an incomplete execution prefix. It retains only values the children actually returned;
// the frozen plan supplies recipes and logical bindings, never Git identities.
func composeExecutionRevisionResults(
	plan Plan,
	outcomes map[string]string,
	authored []ExecutionAuthorResult,
) ([]RevisionResult, error) {
	if plan.Schema != PlanV3Schema || validatePlan(plan, &plan.Revisions) != nil || len(authored) > 3 {
		return nil, errExecutionRevisionResults
	}
	physicalPhases := [...]string{"cold", "physical_delta_b", "return_a"}
	logicalPhases := [...]string{"cold", "logical_delta_b", "return_a"}
	names := [...]string{"a", "b", "a-return"}
	result := make([]RevisionResult, len(names))
	for index, name := range names {
		physical, physicalOK := namedPhysicalRevision(plan.Revisions.Physical, name)
		logical, logicalOK := namedLogicalRevision(plan.Revisions.Logical, name)
		if !physicalOK || !logicalOK {
			return nil, errExecutionRevisionResults
		}
		result[index] = RevisionResult{
			Name: name, PhysicalPhase: physicalPhases[index], PhysicalOutcome: outcomes[physicalPhases[index]],
			LogicalPhase: logicalPhases[index], LogicalOutcome: outcomes[logicalPhases[index]],
			PhysicalTreeRecipeSHA256:   physical.SourceTreeRecipeSHA256,
			PhysicalCommitRecipeSHA256: physical.SourceCommitRecipeSHA256,

			CatalogLogicalSHA256: logical.CatalogLogicalSHA256, SemanticSHA256: logical.SemanticSHA256,
			CatalogSource: logical.CatalogSource,
		}
		// Receipt v3 intentionally omits partial physical identities. A completed
		// author alone does not establish that its physical phase passed.
		if outcomes[physicalPhases[index]] != "passed" {
			continue
		}
		if index >= len(authored) {
			return nil, errExecutionRevisionResults
		}
		actual := authored[index]
		if actual.Revision != name || actual.ProducerID != uint32(7+index) ||
			actual.Response == nil || actual.Response.Result.Name != name ||
			!validExecutionSHA256(actual.Response.ConfigSHA256) || !actual.RootStarted ||
			!actual.RootJoined || !actual.SessionEmpty || !actual.Completed ||
			!authorCustodyProducerComplete(actual.Accounting, actual.ProducerID, authorCustodyAttempts(index)) {
			return nil, errExecutionRevisionResults
		}
		authoredRevision := actual.Response.Result
		result[index].PhysicalCommit, result[index].PhysicalTree = authoredRevision.Commit, authoredRevision.Tree
		result[index].PhysicalParentCommit, result[index].AuthoredManifest = authoredRevision.ParentCommit, authoredRevision.Manifest
	}
	if validateRevisionResults(result, outcomes, plan) != nil {
		return nil, errExecutionRevisionResults
	}
	return result, nil
}
