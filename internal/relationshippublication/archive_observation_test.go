package relationshippublication

import (
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestRelationshipArchiveIndependentArtifactObservations(t *testing.T) {
	fixture := newArchiveV3Fixture(t)
	fixture.publishV3(t)
	var observations []archiveevidence.Observation
	ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { observations = append(observations, value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "relationship-publication.tar")
	if _, err := CreateArchive(ctx, fixture.dataDir, archive); err != nil {
		t.Fatal(err)
	}
	if err := RestoreArchive(ctx, archive, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %+v", observations)
	}
	for index, value := range observations {
		if value.Stage != archiveevidence.Stage(index+1) || value.Path != "relationship-publication.tar" || value.Identity.Records < 5 || value.Identity != observations[0].Identity {
			t.Fatalf("observation %d = %+v", index, value)
		}
	}
}
