package archiveevidence

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"path/filepath"
	"testing"
)

func TestInventoryRequiresIndependentReadback(t *testing.T) {
	for _, test := range []struct {
		name    string
		read    bool
		payload string
		want    bool
	}{
		{"complete", true, "payload", true},
		{"missing read", false, "payload", false},
		{"different read", true, "changed", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var reports []Observation
			ctx, err := WithObserver(t.Context(), func(value Observation) error { reports = append(reports, value); return nil })
			if err != nil {
				t.Fatal(err)
			}
			inventory := New(ctx, "component.tar", 1)
			if err := inventory.Add("member", 7, sha256.Sum256([]byte("payload"))); err != nil {
				t.Fatal(err)
			}
			if err := inventory.Emit(ctx, Archived); err != nil {
				t.Fatal(err)
			}
			root := t.TempDir()
			verify, err := inventory.VerificationContext(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			var readErr error
			if test.read {
				readErr = ObserveRead(verify, filepath.Join(root, "member"), []byte(test.payload))
			}
			err = errors.Join(readErr, inventory.Emit(ctx, After))
			if (err == nil) != test.want {
				t.Fatalf("readback error = %v", err)
			}
			if test.want && (len(reports) != 2 || reports[0].Identity != reports[1].Identity) {
				t.Fatalf("reports = %+v", reports)
			}
			if !test.want && len(reports) != 1 {
				t.Fatalf("invalid after emitted: %+v", reports)
			}
		})
	}
}

func TestInventoryOrderingDuplicatesBoundsAndCancellation(t *testing.T) {
	ctx, err := WithObserver(t.Context(), func(Observation) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	for _, names := range [][]string{{"b", "a"}, {"a", "b"}} {
		inventory := New(ctx, "component.tar", 2)
		for _, name := range names {
			if err := inventory.Add(name, 1, sha256.Sum256([]byte(name))); err != nil {
				t.Fatal(err)
			}
		}
		if err := inventory.Emit(ctx, Before); err != nil {
			t.Fatal(err)
		}
		if err := inventory.Add("extra", 0, sha256.Sum256(nil)); err == nil {
			t.Fatal("entry bound ignored")
		}
	}
	duplicate := New(ctx, "component.tar", 2)
	if err := duplicate.Add("a", 0, sha256.Sum256(nil)); err != nil {
		t.Fatal(err)
	}
	if err := duplicate.Add("a", 0, sha256.Sum256(nil)); err == nil {
		t.Fatal("duplicate accepted")
	}
	if New(t.Context(), "unused", 1) != nil {
		t.Fatal("ordinary path allocated inventory")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := Emit(canceled, Observation{Stage: Before, Path: "database.surql", Identity: (&Stream{}).Identity()}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled emit = %v", err)
	}
}

func TestInventoryFramingOverflowAndEmptyIdentity(t *testing.T) {
	value := (&Stream{}).Identity()
	if !ValidIdentity(value) {
		t.Fatal("empty native inventory rejected")
	}
	for _, mutate := range []func(*Identity){
		func(v *Identity) { v.FramedBytes = 1 },
		func(v *Identity) {
			v.SHA256 = "sha256:" + "0000000000000000000000000000000000000000000000000000000000000000"
		},
		func(v *Identity) { v.Records = math.MaxUint64 },
	} {
		copy := value
		mutate(&copy)
		if ValidIdentity(copy) {
			t.Fatal("malformed inventory accepted")
		}
	}
	for _, framed := range []uint64{math.MaxUint64, math.MaxUint64 - 4, math.MaxUint64 - 8} {
		stream := Stream{identity: Identity{FramedBytes: framed}}
		if err := stream.Add(Record{Path: "member", SHA256: value.SHA256}); err == nil {
			t.Fatal("framing overflow accepted")
		}
	}
}
