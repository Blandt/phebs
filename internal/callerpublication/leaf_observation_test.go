package callerpublication

import (
	"path/filepath"
	"testing"
)

func TestLeafObservationsPreserveUnresolvedAndDetach(t *testing.T) {
	fixture := newPublicationFixture(t, filepath.Join(t.TempDir(), "leaves"), "example.invalid/observed", '1')
	publication := publishFixture(t, fixture)
	reopened, err := Open(t.Context(), fixture.root, publication.State())
	if err != nil {
		t.Fatal(err)
	}
	for _, opened := range []*Publication{publication, reopened} {
		got := opened.LeafObservations()
		if len(got) != 1 || got[0].Unresolved != 1 || got[0].Abstentions != 1 || got[0].Results != 0 ||
			got[0].ContentSHA256 != fixture.receipt.ContentDigest || got[0].ContentBytes != uint64(fixture.receipt.ContentBytes) {
			t.Fatalf("actual leaf observation=%+v", got)
		}
		got[0].Unresolved = 0
		if opened.LeafObservations()[0].Unresolved != 1 {
			t.Fatal("projection aliases retained native count")
		}
	}
}
