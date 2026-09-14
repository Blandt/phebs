package t421

import (
	"github.com/bmeddeb/phebs/internal/callerpublication"
	"github.com/bmeddeb/phebs/internal/readaccounting"
	"testing"
)

// This is an explicitly modeled adapter fixture. Native constructor/replay
// gates separately establish that production outputs equal the frozen values.
func relationshipAdapterFixture(t *testing.T) (Plan, []ExecutionPhaseInspection, []AuthorityPhaseResult, ExecutionEpochOneResult) {
	t.Helper()
	plan := logicalStoreWorkTestPlan(t)
	authority := AuthorityPhaseResult{Phase: "product_queries", Outcome: "passed", AuthorityState: AuthorityState{
		Current: true, ResolverCatalogGenerationSHA256: testDigest("resolver-generation"), ResolverCatalogRootSHA256: testDigest("resolver-root"),
		CallerGenerationSHA256: testDigest("caller-generation"), CallerRootSHA256: testDigest("caller-root"),
		RelationshipGenerationSHA256: testDigest("relationship-generation"), RelationshipRootSHA256: testDigest("relationship-root"),
	}, ExtractionRoots: []ExtractionRootResult{{Domain: "grpc-caller", Candidates: SetIdentity{Records: plan.Oracle.ProductRelationships.CallerCandidateRecords, FramedBytes: 1, SHA256: testDigest("candidates")}}}}
	modeled := testCallerPublication(t, plan, authority)
	observed := &ExecutionCallerPublicationObservation{RelationshipRootReads: 1, RelationshipGenerationReads: 1,
		GenerationSHA256: authority.CallerGenerationSHA256, ManifestSHA256: authority.CallerRootSHA256, RPCProjection: modeled.Projection}
	for _, leaf := range modeled.Leaves {
		observed.Leaves = append(observed.Leaves, callerpublication.LeafObservation{
			Domain: "grpc-caller", Prefix: leaf.Prefix, CandidateRecords: leaf.CandidateRecords, Results: leaf.ResolvedPostings,
			Abstentions: leaf.Abstentions, Records: leaf.Records, ContentBytes: leaf.CanonicalBytes, ContentSHA256: leaf.ResultSHA256})
	}
	final := &ExecutionInspectionFinal{Ordinal: 12, Authority: authority.AuthorityState, CallerPublication: observed,
		RPCPostings:           &ExecutionRPCPostingObservation{Resolved: modeled.ResolvedPostings},
		ResolverCatalogCounts: &readaccounting.ResolverCatalogCounts{GenerationSHA256: authority.ResolverCatalogGenerationSHA256, ManifestSHA256: authority.ResolverCatalogRootSHA256, DeclarationRecords: 10100, GeneratedDescriptors: 10100}}
	final.Projection.ProductRelationship = expectedProductRelationshipResult(plan)
	for _, family := range plan.Oracle.Relationships {
		final.Projection.RelationshipResults = append(final.Projection.RelationshipResults, expectedRelationshipResult(family))
	}
	return plan, []ExecutionPhaseInspection{{Phase: "product_queries", ServerEpoch: 5, SelectorAccepted: true, Final: final}}, []AuthorityPhaseResult{authority}, ExecutionEpochOneResult{ProductFinals: 2, ProductFirstFinalOrdinal: 10, QueryResults: &QueryEvidence{Phase: "product_queries", Outcome: "passed"}}
}

func TestComposeExecutionRelationshipUsesObservedCaller(t *testing.T) {
	cases := []struct {
		name   string
		mutate func([]ExecutionPhaseInspection, *ExecutionEpochOneResult)
		want   bool
	}{
		{"complete", func([]ExecutionPhaseInspection, *ExecutionEpochOneResult) {}, true},
		{"prior exact catalog", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {}, true},
		{"absent counts", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {
			rows[0].Final.ResolverCatalogCounts = nil
		}, false},
		{"wrong catalog", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {
			rows[0].Final.ResolverCatalogCounts.GenerationSHA256 = testDigest("other")
		}, false},
		{"wrong native declaration count", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {
			rows[0].Final.ResolverCatalogCounts.DeclarationRecords++
		}, false},
		{"native unresolved leaf", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {
			rows[0].Final.CallerPublication.Leaves[0].Unresolved = 1
		}, false},
		{"native unresolved RPC", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {
			rows[0].Final.RPCPostings.Unresolved = 1
		}, false},
		{"wrong native RPC projection", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) {
			rows[0].Final.CallerPublication.RPCProjection.SHA256 = testDigest("other")
		}, false},
		{"not two F reads", func(_ []ExecutionPhaseInspection, product *ExecutionEpochOneResult) { product.ProductFinals = 1 }, false},
		{"unaccepted", func(rows []ExecutionPhaseInspection, _ *ExecutionEpochOneResult) { rows[0].SelectorAccepted = false }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, rows, authorities, product := relationshipAdapterFixture(t)
			if tc.name == "prior exact catalog" {
				prior := rows[0]
				prior.Phase = "cold"
				final := *prior.Final
				prior.Final = &final
				rows[0].Final.ResolverCatalogCounts = nil
				rows = append(rows, prior)
			}
			tc.mutate(rows, &product)
			got, err := composeExecutionRelationshipEvidence(plan, rows, authorities, product)
			if (err == nil) != tc.want {
				t.Fatalf("evidence=%+v error=%v want success=%v", got, err, tc.want)
			}
			if tc.want {
				if got.Caller.ResolvedPostings != 10999 {
					t.Fatal("lost observed counts")
				}
				got.Caller.Leaves[0].ResultSHA256 = "changed"
				if rows[0].Final.CallerPublication.Leaves[0].ContentSHA256 == "changed" {
					t.Fatal("aliased native evidence")
				}
			}
		})
	}
}
