package t421

import "testing"

func TestExecutionArchiveSemanticDigestsUseObservedValues(t *testing.T) {
	plan := accountingTestPlan(t)
	projection, err := expectedStateProjectionForPhase(plan, "pressure_75")
	if err != nil {
		t.Fatal(err)
	}
	observed := executionArchiveSemanticObservation{Projection: projection, CallerProjection: plan.Oracle.ProductRelationships.ExpectedRPCProjections}
	for _, leaf := range plan.Oracle.ProductRelationships.CallerLeaves {
		observed.CallerLeaves = append(observed.CallerLeaves, CallerPublicationLeafResult{Prefix: leaf.Prefix, CandidateRecords: leaf.CandidateRecords,
			ResolvedPostings: leaf.ResolvedPostings, Abstentions: leaf.Abstentions, Records: leaf.Records, CanonicalBytes: leaf.CanonicalBytes, EncodedBytes: leaf.EncodedBytes})
	}
	gotState, gotRelationship, err := executionArchiveSemanticDigests(plan, observed)
	if err != nil {
		t.Fatal(err)
	}
	wantState, err := expectedArchiveSemanticStateSHA256(plan)
	if err != nil {
		t.Fatal(err)
	}
	wantRelationship, err := expectedRelationshipSemanticSHA256(plan)
	if err != nil {
		t.Fatal(err)
	}
	if gotState != wantState || gotRelationship != wantRelationship {
		t.Fatal("modeled native semantic projection differs from receipt contract")
	}
	for _, test := range []struct {
		name   string
		change func(*executionArchiveSemanticObservation)
	}{
		{"catalog", func(v *executionArchiveSemanticObservation) {
			v.Projection.Catalog.SHA256 = SHA256([]byte("changed catalog"))
		}},
		{"caller projection", func(v *executionArchiveSemanticObservation) {
			v.CallerProjection.SHA256 = SHA256([]byte("changed projection"))
		}},
		{"product", func(v *executionArchiveSemanticObservation) { v.Projection.ProductRelationship.RPCProjections++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := observed
			test.change(&changed)
			state, relationship, e := executionArchiveSemanticDigests(plan, changed)
			if e == nil && state == gotState && relationship == gotRelationship {
				t.Fatal("actual semantic change discarded")
			}
		})
	}
}
