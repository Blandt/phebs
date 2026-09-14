package callerpublication

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestCallerArchiveIndependentArtifactObservations(t *testing.T) {
	root := filepath.Join(t.TempDir(), "caller-leaves")
	installArchivePublication(t, root, "github.com/acme/archive", func(context.Context, State) error { return nil })
	var observations []archiveevidence.Observation
	ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { observations = append(observations, value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "caller-publication.tar")
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
		if value.Stage != archiveevidence.Stage(index+1) || value.Path != "caller-publication.tar" || value.Identity.Records < 2 || value.Identity != observations[0].Identity {
			t.Fatalf("observation %d = %+v", index, value)
		}
	}
}
