package t421

import (
	"slices"

	"github.com/bmeddeb/phebs/internal/readaccounting"
)

// Build only from the two accepted product F calls and a matching genuine
// sealed-catalog observation. Prior epochs can supply an unchanged catalog's
// counts, but every supplied observation of those exact bytes must agree.
func composeExecutionRelationshipEvidence(plan Plan, rows []ExecutionPhaseInspection, authorities []AuthorityPhaseResult, product ExecutionEpochOneResult) (RelationshipEvidence, error) {
	refuse := func() (RelationshipEvidence, error) { return RelationshipEvidence{}, errExecutionReceiptMetrics }
	if plan.Schema != PlanV3Schema || product.ProductFinals != 2 || product.ProductFirstFinalOrdinal == 0 ||
		product.QueryResults == nil || product.QueryResults.Phase != "product_queries" || product.QueryResults.Outcome != "passed" {
		return refuse()
	}
	index := slices.IndexFunc(rows, func(row ExecutionPhaseInspection) bool { return row.Phase == "product_queries" })
	authorityIndex := slices.IndexFunc(authorities, func(row AuthorityPhaseResult) bool { return row.Phase == "product_queries" })
	if index < 0 || authorityIndex < 0 {
		return refuse()
	}
	row, authority := rows[index], authorities[authorityIndex]
	if !row.SelectorAccepted || row.ServerEpoch != 5 || row.Final == nil || row.Final.Ordinal <= product.ProductFirstFinalOrdinal ||
		row.Final.Authority != authority.AuthorityState || authority.Outcome != "passed" || row.Final.CallerPublication == nil || row.Final.RPCPostings == nil {
		return refuse()
	}
	observed := row.Final.CallerPublication
	if !observed.valid() || !row.Final.RPCPostings.valid() || observed.GenerationSHA256 != authority.CallerGenerationSHA256 || observed.ManifestSHA256 != authority.CallerRootSHA256 {
		return refuse()
	}
	var counts *readaccounting.ResolverCatalogCounts
	for _, candidate := range rows {
		if !candidate.SelectorAccepted || candidate.Final == nil || candidate.Final.ResolverCatalogCounts == nil {
			continue
		}
		value := candidate.Final.ResolverCatalogCounts
		if value.GenerationSHA256 != authority.ResolverCatalogGenerationSHA256 || value.ManifestSHA256 != authority.ResolverCatalogRootSHA256 {
			continue
		}
		if candidate.Final.Authority.ResolverCatalogGenerationSHA256 != value.GenerationSHA256 || candidate.Final.Authority.ResolverCatalogRootSHA256 != value.ManifestSHA256 || counts != nil && *counts != *value {
			return refuse()
		}
		counts = value
	}
	rootIndex := slices.IndexFunc(authority.ExtractionRoots, func(root ExtractionRootResult) bool { return root.Domain == "grpc-caller" })
	if counts == nil || rootIndex < 0 {
		return refuse()
	}
	caller := CallerPublicationResult{
		Schema: "t422-global-caller-publication-v1", ExecutionPolicy: "callerexecute-catalog-wide-direct-resolver-v1",
		CandidateInventory:              authority.ExtractionRoots[rootIndex].Candidates,
		ResolverCatalogGenerationSHA256: counts.GenerationSHA256, ResolverCatalogRootSHA256: counts.ManifestSHA256,
		ResolverDeclarationRecords: counts.DeclarationRecords, GeneratedDescriptors: counts.GeneratedDescriptors,
		GenerationSHA256: observed.GenerationSHA256, RootSHA256: authority.CallerRootSHA256, ManifestSHA256: observed.ManifestSHA256,
		Current: authority.Current, UnresolvedPostings: row.Final.RPCPostings.Unresolved, Projection: observed.RPCProjection,
		RelationshipGenerationSHA256: authority.RelationshipGenerationSHA256, RelationshipRootSHA256: authority.RelationshipRootSHA256,
	}
	for _, leaf := range observed.Leaves {
		if leaf.Domain != "grpc-caller" {
			continue
		}
		caller.Leaves = append(caller.Leaves, CallerPublicationLeafResult{
			Prefix: leaf.Prefix, CandidateRecords: leaf.CandidateRecords, Outcome: "success", ResolvedPostings: leaf.Results,
			Abstentions: leaf.Abstentions, Records: leaf.Records, Unresolved: leaf.Unresolved,
			CanonicalBytes: leaf.ContentBytes, EncodedBytes: leaf.ContentBytes, ResultSHA256: leaf.ContentSHA256,
		})
		caller.ResolvedPostings += leaf.Results
		caller.Abstentions += leaf.Abstentions
		caller.Records += leaf.Records
		caller.CanonicalBytes += leaf.ContentBytes
		caller.EncodedBytes += leaf.ContentBytes
	}
	var err error
	caller.LeavesSHA256, err = receiptSHA256(caller.Leaves)
	if err != nil {
		return refuse()
	}
	caller.ComponentBindingSHA256, err = receiptSHA256(caller)
	if err != nil {
		return refuse()
	}
	identity, ok := authorityIdentitySHA256(authority)
	if !ok {
		return refuse()
	}
	productProjection := row.Final.Projection.ProductRelationship
	out := RelationshipEvidence{Phase: "product_queries", Outcome: "passed", AuthorityBeforeSHA256: identity, AuthorityAfterSHA256: identity,
		RelationshipRootReads: observed.RelationshipRootReads, RelationshipGenerationReads: observed.RelationshipGenerationReads,
		Results: slices.Clone(row.Final.Projection.RelationshipResults), Caller: &caller, Product: &productProjection}
	if validateRelationshipEvidence(out, "passed", identity, authority, plan) != nil {
		return refuse()
	}
	return out, nil
}
