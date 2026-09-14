package focusedindex

import (
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestFocusedArchiveIndependentArtifactObservations(t *testing.T) {
	fixture := newFocusedFixture(t)
	indexDir := t.TempDir()
	if _, err := Build(t.Context(), Request{Schema: RequestSchema, RepoDir: fixture.repo, OutputDir: indexDir, Scope: fixture.scope, Revisions: fixture.revisions}, BuildOptions{ShardMax: 128}); err != nil {
		t.Fatal(err)
	}
	var observations []archiveevidence.Observation
	ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { observations = append(observations, value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "focused-index.tar")
	if _, err := CreateArchiveWithSelections(ctx, indexDir, archive, nil); err != nil {
		t.Fatal(err)
	}
	if err := RestoreArchiveContext(ctx, archive, filepath.Join(t.TempDir(), "restored")); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %+v", observations)
	}
	for index, value := range observations {
		if value.Stage != archiveevidence.Stage(index+1) || value.Path != "focused-index.tar" || value.Identity.Records < 3 || value.Identity != observations[0].Identity {
			t.Fatalf("observation %d = %+v", index, value)
		}
	}
}
