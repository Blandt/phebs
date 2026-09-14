package main

import (
	"context"
	"testing"

	"github.com/bmeddeb/phebs/internal/readaccounting"
)

func TestT422FinalResolverCatalogCounts(t *testing.T) {
	actual := readaccounting.ResolverCatalogCounts{GenerationSHA256: "generation", ManifestSHA256: "manifest", DeclarationRecords: 2, GeneratedDescriptors: 0}
	control := &t422ResolverCatalogCountsControl{latest: &actual}
	ctx := context.WithValue(t.Context(), t422ResolverCatalogCountsKey{}, control)
	for _, test := range []struct {
		name, generation, manifest string
		present                    bool
	}{
		{"actual", "generation", "manifest", true},
		{"changed generation", "replacement", "manifest", false},
		{"changed manifest", "generation", "replacement", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := t422FinalResolverCatalogCounts(ctx, test.generation, test.manifest)
			if err != nil || (got != nil) != test.present {
				t.Fatal(got, err)
			}
			if got != nil {
				if *got != actual {
					t.Fatal("native counts changed")
				}
				got.DeclarationRecords++
			}
		})
	}
	if control.latest.DeclarationRecords != 2 {
		t.Fatal("returned counts alias native capture")
	}
	control.latest = nil
	if got, err := t422FinalResolverCatalogCounts(ctx, "generation", "manifest"); err != nil || got != nil {
		t.Fatal("missing source became observed")
	}
}

func TestT422ResolverCatalogCountsRejectConflictingIdentity(t *testing.T) {
	original := readaccounting.ResolverCatalogCounts{GenerationSHA256: "generation", ManifestSHA256: "manifest", DeclarationRecords: 2}
	for _, test := range []struct {
		name   string
		mutate func(*readaccounting.ResolverCatalogCounts)
		want   bool
	}{
		{"same identity same counts", func(*readaccounting.ResolverCatalogCounts) {}, true},
		{"same identity changed declarations", func(v *readaccounting.ResolverCatalogCounts) { v.DeclarationRecords++ }, false},
		{"same identity changed descriptors", func(v *readaccounting.ResolverCatalogCounts) { v.GeneratedDescriptors++ }, false},
		{"new sealed identity", func(v *readaccounting.ResolverCatalogCounts) {
			v.ManifestSHA256 = "replacement"
			v.DeclarationRecords++
		}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			control := &t422ResolverCatalogCountsControl{}
			if err := control.retain(original); err != nil {
				t.Fatal(err)
			}
			next := original
			test.mutate(&next)
			err := control.retain(next)
			if (err == nil) != test.want {
				t.Fatal(err)
			}
			if !test.want && *control.latest != original {
				t.Fatal("conflict replaced retained observation")
			}
		})
	}
}
