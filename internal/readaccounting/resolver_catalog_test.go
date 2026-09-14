package readaccounting

import (
	"context"
	"errors"
	"testing"
)

func TestResolverCatalogObserver(t *testing.T) {
	for _, name := range []string{"actual", "sink", "panic", "cancel"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			want := ResolverCatalogCounts{GenerationSHA256: "generation", ManifestSHA256: "manifest", DeclarationRecords: 3, GeneratedDescriptors: 7}
			calls := 0
			ctx, err := WithResolverCatalogObserver(ctx, func(got ResolverCatalogCounts) error {
				calls++
				if got != want {
					t.Fatal("native counts changed")
				}
				if name == "sink" {
					return ErrEvent
				}
				if name == "panic" {
					panic("sink")
				}
				if name == "cancel" {
					cancel()
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			err = ObserveResolverCatalog(ctx, true, want)
			if calls != 1 || (err == nil) != (name == "actual") {
				t.Fatal(calls, err)
			}
			if name == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
	if err := ObserveResolverCatalog(t.Context(), false, ResolverCatalogCounts{}); err != nil {
		t.Fatal(err)
	}
	if err := ObserveResolverCatalog(t.Context(), true, ResolverCatalogCounts{}); !errors.Is(err, ErrScope) {
		t.Fatal(err)
	}
}
