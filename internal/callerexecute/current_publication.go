package callerexecute

import (
	"context"
	"errors"
	"fmt"

	"github.com/bmeddeb/phebs/internal/store"
)

// ReconcileCurrent handles a non-forced publication notification using only a
// previously cold-validated publication and fresh native authority. It never
// cold-opens a caller artifact, repairs a marker, or directly changes a queue.
// A false result leaves those operations to the normal worker. The downstream callback
// is retried even when an earlier call published successfully but its callback
// failed.
func (worker *Worker) ReconcileCurrent(ctx context.Context, repository string) (bool, error) {
	if worker == nil || worker.store == nil || worker.registry == nil || ctx == nil || repository == "" {
		return false, errors.New("caller current publication is not configured")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	worker.cacheMu.Lock()
	cached := worker.validations.publication
	key := worker.validations.key
	worker.cacheMu.Unlock()
	if cached == nil || cached.State().Generation.Repository != repository {
		return false, nil
	}
	if current, err := cached.CurrentResultContext(ctx); err != nil || !current {
		return false, err
	}
	// This common worker/product authority path includes actual partitioned
	// observation and extraction roots, not just candidate/resolver pointers.
	current, inactive, err := worker.jobAuthority(ctx, repository)
	if errors.Is(err, ErrPartitionedCallerPending) || errors.Is(err, ErrPartitionedCallerUnavailable) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load cached caller authority: %w", err)
	}
	if inactive || current == nil || key != current.semantic.Digest {
		return false, nil
	}
	pointer, err := worker.store.GetCallerGenerationPublicationSummary(ctx, repository)
	if errors.Is(err, store.ErrNotFound) || errors.Is(err, store.ErrInvalidCallerGenerationPublication) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("load cached caller summary: %w", err)
	}
	if pointer == nil || pointer.Generation != current.stored || !publicationMatchesSummary(current, cached, *pointer) {
		return false, nil
	}
	// The cached immutable publication was already cold-authenticated by the
	// worker. This existing warm fence checks every mutable store authority
	// without hashing the pair array again.
	valid, err := worker.store.CallerGenerationPublicationSummaryAuthorityCurrent(ctx, *pointer)
	if errors.Is(err, store.ErrInvalidCallerGenerationPublication) {
		return false, nil // The queued worker owns deterministic publication repair.
	}
	if err != nil || !valid {
		return false, err
	}
	if valid, err := cached.CurrentResultContext(ctx); err != nil || !valid {
		return false, err
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
