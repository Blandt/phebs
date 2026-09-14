package readaccounting

import "context"

// ResolverCatalogCounts is observed only after the native builder seals one
// catalog. Counts describe its actual declaration rows and generated descriptors;
// sealing does not assert publication or current authority.
type ResolverCatalogCounts struct {
	GenerationSHA256     string `json:"generation_sha256"`
	ManifestSHA256       string `json:"manifest_sha256"`
	DeclarationRecords   uint64 `json:"declaration_records"`
	GeneratedDescriptors uint64 `json:"generated_descriptors"`
}

type resolverCatalogObserverKey struct{}

func WithResolverCatalogObserver(ctx context.Context, observe func(ResolverCatalogCounts) error) (context.Context, error) {
	if ctx == nil || observe == nil || ctx.Value(resolverCatalogObserverKey{}) != nil {
		return nil, ErrScope
	}
	return context.WithValue(ctx, resolverCatalogObserverKey{}, observe), nil
}

func ResolverCatalogObserverBound(ctx context.Context) bool {
	return ctx != nil && ctx.Value(resolverCatalogObserverKey{}) != nil
}

func ObserveResolverCatalog(ctx context.Context, required bool, counts ResolverCatalogCounts) (err error) {
	var observe func(ResolverCatalogCounts) error
	if ctx != nil {
		observe, _ = ctx.Value(resolverCatalogObserverKey{}).(func(ResolverCatalogCounts) error)
	}
	if observe == nil {
		if required {
			return ErrScope
		}
		return nil
	}
	defer func() {
		if recover() != nil {
			err = ErrEvent
		}
	}()
	if err := observe(counts); err != nil {
		return err
	}
	return ctx.Err()
}
