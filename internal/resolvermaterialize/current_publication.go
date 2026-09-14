package resolvermaterialize

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/bmeddeb/phebs/internal/analysisunit"
	"github.com/bmeddeb/phebs/internal/extract"
	"github.com/bmeddeb/phebs/internal/resolvercatalog"
	"github.com/bmeddeb/phebs/internal/store"
)

// ReconcileCurrent handles a non-forced publication notification only when this
// worker has already cold-validated the exact current artifact. Missing or
// changed authority returns false so the caller retains its ordinary queued
// recovery. The existing downstream callback still runs, including on retry
// after a callback failure. This method opens no catalog or source member.
func (worker *Worker) ReconcileCurrent(ctx context.Context, repository string) (bool, error) {
	if worker == nil || worker.store == nil || worker.registry == nil || worker.manifests == nil || ctx == nil || repository == "" {
		return false, errors.New("resolver current publication is not configured")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	cached := worker.cached(repository)
	if cached == nil || !cached.Current() {
		return false, nil
	}
	pointer, err := worker.store.GetResolverCatalogPublication(ctx, repository)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidResolverCatalogPublication) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load cached resolver authority: %w", err)
	}
	if pointer == nil || !reflect.DeepEqual(cached.State(), StateFromStore(*pointer)) {
		return false, nil
	}
	// The pointer's own declaration list cannot detect a newly published domain
	// that was absent when this catalog was built. Derive the complete enabled
	// declaration identity through the same bounded controls used by Handle.
	repo, err := worker.store.GetRepo(ctx, repository)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load resolver repository: %w", err)
	}
	if repo == nil || repo.Name != repository || repo.Deleting || repo.IndexedCommitHash == "" {
		return false, nil
	}
	if repo.IndexedAnalysisUnit != nil {
		if err := repo.IndexedAnalysisUnit.Validate(repository); err != nil {
			return false, fmt.Errorf("resolver analysis unit: %w", err)
		}
	}
	candidate, err := worker.manifests.CandidateManifestGeneration(ctx, extract.CandidateManifestRequest{
		Repository: repository, Commit: repo.IndexedCommitHash,
		AnalysisUnit: analysisunit.CloneState(repo.IndexedAnalysisUnit),
		Domains:      worker.registry.CandidateDomains(),
	})
	if err != nil {
		return false, fmt.Errorf("load resolver candidate authority: %w", err)
	}
	if candidate.ManifestDigest == "" || candidate.ControlRevision == 0 {
		return false, nil
	}
	declarations, settled, err := worker.currentDeclarations(ctx, repo, candidate)
	if err != nil || !settled {
		return false, err
	}
	identityDeclarations := make([]resolvercatalog.DeclarationPublication, len(declarations))
	for index, declaration := range declarations {
		identityDeclarations[index] = resolvercatalog.DeclarationPublication{
			Domain: declaration.Domain, RunID: declaration.RunID,
			GenerationDigest: declaration.GenerationDigest,
			AuthoritySchema:  declaration.AuthoritySchema,
			PlanDigest:       declaration.PlanDigest, RootDigest: declaration.RootDigest,
		}
	}
	identity, err := resolvercatalog.NewIdentity(repository, repo.IndexedCommitHash,
		unitDigest(repo.IndexedAnalysisUnit), candidate.ManifestDigest,
		identityDeclarations, worker.registry.Packs())
	if err != nil {
		return false, fmt.Errorf("resolver current identity: %w", err)
	}
	if !pointerMatchesIdentity(*pointer, identity) {
		return false, nil
	}
	current, err := worker.store.ResolverCatalogPublicationCurrent(ctx, *pointer)
	if err != nil {
		return false, fmt.Errorf("confirm cached resolver authority: %w", err)
	}
	if !current || !cached.Current() {
		return false, nil
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if err := worker.afterPublish(ctx, repository); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return true, nil
}
