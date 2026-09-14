package t421

import (
	"testing"

	"github.com/bmeddeb/phebs/internal/callerpublication"
	"github.com/bmeddeb/phebs/internal/readaccounting"
)

func TestInspectionFinalCallerCountsDetached(t *testing.T) {
	value := ExecutionInspectionFinal{
		RPCPostings:           &ExecutionRPCPostingObservation{Resolved: 2, NameMatch: 1, Unresolved: 3},
		ResolverCatalogCounts: &readaccounting.ResolverCatalogCounts{DeclarationRecords: 5, GeneratedDescriptors: 7},
		CallerPublication:     &ExecutionCallerPublicationObservation{Leaves: []callerpublication.LeafObservation{{Unresolved: 3}}},
	}
	copy := cloneInspectionFinal(value)
	copy.RPCPostings.Unresolved = 0
	copy.ResolverCatalogCounts.DeclarationRecords = 0
	copy.CallerPublication.Leaves[0].Unresolved = 0
	if value.RPCPostings.Unresolved != 3 || value.ResolverCatalogCounts.DeclarationRecords != 5 || value.CallerPublication.Leaves[0].Unresolved != 3 {
		t.Fatal("inspection snapshot changed native observations")
	}
	for _, counts := range []ExecutionRPCPostingObservation{{Resolved: 1_000_001}, {Resolved: 1_000_000, NameMatch: 1}, {NameMatch: 1_000_000, Unresolved: 1}} {
		if counts.valid() {
			t.Fatal("out-of-bound RPC population accepted")
		}
	}
}
