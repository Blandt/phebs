package observationpublication

import (
	"path/filepath"
	"testing"

	"github.com/bmeddeb/phebs/internal/archiveevidence"
)

func TestObservationArchiveIndependentArtifactObservations(t *testing.T) {
	repositoryDirectory, commit := observationFixture(t, map[string][]byte{"a.go": []byte("package demo\nconst A = 1\n")})
	root := filepath.Join(t.TempDir(), "observations")
	repository := "example/archive-observed"
	plan := buildObservationPlan(t, repositoryDirectory, commit, repository, "v1")
	if _, err := Publish(t.Context(), root, repositoryDirectory, plan, nil); err != nil {
		t.Fatal(err)
	}
	transition, err := BeginInventoryPublicationV2(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	buildInventoryPublicationTransitionV2(t, transition, repositoryDirectory, repository, commit)
	if _, err := CompleteInventoryPublicationV2(t.Context(), root, repository, transition.TransitionID, nil); err != nil {
		t.Fatal(err)
	}
	var observations []archiveevidence.Observation
	ctx, err := archiveevidence.WithObserver(t.Context(), func(value archiveevidence.Observation) error { observations = append(observations, value); return nil })
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "observation-publication.tar")
	if _, err := CreateArchive(ctx, root, archive); err != nil {
		t.Fatal(err)
	}
	if err := RestoreArchive(ctx, archive, filepath.Join(t.TempDir(), "restored")); err != nil {
		t.Fatal(err)
	}
	if len(observations) != 3 {
		t.Fatalf("observations = %+v", observations)
	}
	for index, value := range observations {
		if value.Stage != archiveevidence.Stage(index+1) || value.Path != "observation-publication.tar" || value.Identity.Records < 5 || value.Identity != observations[0].Identity {
			t.Fatalf("observation %d = %+v", index, value)
		}
	}
}
