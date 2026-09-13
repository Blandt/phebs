package t421

import "errors"

var errExecutionRevisionResults = errors.New("execution revision results are incomplete")

// composeExecutionRevisionResults projects the three joined author children
// into receipt evidence. It retains only values the children actually returned;
// the frozen plan supplies recipes and logical bindings, never Git identities.
func composeExecutionRevisionResults(
	plan Plan,
	outcomes map[string]string,
	authored []ExecutionAuthorResult,
) ([]RevisionResult, error) {
	if plan.Schema != PlanV3Schema || validatePlan(plan, &plan.Revisions) != nil || len(authored) != 3 {
		return nil, errExecutionRevisionResults
	}
	physicalPhases := [...]string{"cold", "physical_delta_b", "return_a"}
	logicalPhases := [...]string{"cold", "logical_delta_b", "return_a"}
	names := [...]string{"a", "b", "a-return"}
	result := make([]RevisionResult, len(names))
	for index, name := range names {
		actual := authored[index]
		physical, physicalOK := namedPhysicalRevision(plan.Revisions.Physical, name)
		logical, logicalOK := namedLogicalRevision(plan.Revisions.Logical, name)
		if !physicalOK || !logicalOK || actual.Revision != name || actual.ProducerID != uint32(7+index) ||
			actual.Response == nil || actual.Response.Result.Name != name ||
			!validExecutionSHA256(actual.Response.ConfigSHA256) || !actual.RootStarted ||
			!actual.RootJoined || !actual.SessionEmpty || !actual.Completed ||
			!authorCustodyProducerComplete(actual.Accounting, actual.ProducerID, authorCustodyAttempts(index)) {
			return nil, errExecutionRevisionResults
		}
		authoredRevision := actual.Response.Result
		result[index] = RevisionResult{
			Name: name, PhysicalPhase: physicalPhases[index], PhysicalOutcome: outcomes[physicalPhases[index]],
			LogicalPhase: logicalPhases[index], LogicalOutcome: outcomes[logicalPhases[index]],
			PhysicalTreeRecipeSHA256:   physical.SourceTreeRecipeSHA256,
			PhysicalCommitRecipeSHA256: physical.SourceCommitRecipeSHA256,
			PhysicalCommit:             authoredRevision.Commit, PhysicalTree: authoredRevision.Tree,
			PhysicalParentCommit: authoredRevision.ParentCommit, AuthoredManifest: authoredRevision.Manifest,
			CatalogLogicalSHA256: logical.CatalogLogicalSHA256, SemanticSHA256: logical.SemanticSHA256,
			CatalogSource: logical.CatalogSource,
		}
	}
	if validateRevisionResults(result, outcomes, plan) != nil {
		return nil, errExecutionRevisionResults
	}
	return result, nil
}
