package resolvercatalog

import (
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestResolverArchiveIndependentArtifactObservations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "catalogs")
	state := testInstall(t, root, "github.com/acme/archive", true)
	if err := ClearPublishing(root, state.Repository); err != nil {
		t.Fatal(err)
	}
	var observations []archiveevidence.Observation
	ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { observations = append(observations, value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "resolver-catalog.tar")
	if _, err := CreateArchiveWithReportContext(ctx, root, archive); err != nil {
		t.Fatal(err)
	}
	if err := RestoreArchiveContext(ctx, archive, filepath.Join(t.TempDir(), "restored")); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %+v", observations)
	}
	for index, value := range observations {
		if value.Stage != archiveevidence.Stage(index+1) || value.Path != "resolver-catalog.tar" || value.Identity.Records < 2 || value.Identity != observations[0].Identity {
			t.Fatalf("observation %d = %+v", index, value)
		}
	}
}
