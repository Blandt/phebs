package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/internal/store"
)

// newTestStore skips when the surreal binary is absent, so these tests run
// only where a supervised engine is available.

func TestAuthAPIKeyExpiryRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	// The pinned SurrealDB Go SDK encodes time.Time query variables with
	// second precision (RFC3339, no fractional digits), so fixtures use
	// whole seconds, matching the other engine-backed store tests.
	now := time.Now().UTC().Truncate(time.Second)
	expiry := now.Add(30 * 24 * time.Hour)
	created, err := s.CreateAPIKey(ctx, store.APIKey{
		ID: "expiring", UserID: "user1", Name: "expiring",
		Prefix: "phebs_expir", Hash: "digest",
		CreatedAt: now, ExpiresAt: &expiry,
	})
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if created.ExpiresAt == nil || !created.ExpiresAt.Equal(expiry) {
		t.Fatalf("created expires_at = %v, want %v", created.ExpiresAt, expiry)
	}
	fetched, err := s.GetAPIKey(ctx, "expiring")
	if err != nil {
		t.Fatalf("GetAPIKey: %v", err)
	}
	if fetched.ExpiresAt == nil || !fetched.ExpiresAt.Equal(expiry) {
		t.Fatalf("fetched expires_at = %v, want %v", fetched.ExpiresAt, expiry)
	}

	// Keys created without an expiry keep working exactly as before: the
	// field reads back nil and the row needs no backfill.
	if _, err := s.CreateAPIKey(ctx, store.APIKey{
		ID: "perpetual", UserID: "user1", Name: "perpetual",
		Prefix: "phebs_perpet", Hash: "digest-perpetual", CreatedAt: now,
	}); err != nil {
		t.Fatalf("CreateAPIKey(perpetual): %v", err)
	}
	plain, err := s.GetAPIKey(ctx, "perpetual")
	if err != nil {
		t.Fatalf("GetAPIKey(perpetual): %v", err)
	}
	if plain.ExpiresAt != nil {
		t.Fatalf("perpetual expires_at = %v, want nil", plain.ExpiresAt)
	}
}

func TestAuthLegacyKeyReservedIdentityAndRevocation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.SetLegacyAPIKey(ctx, "legacy-digest", now); err != nil {
		t.Fatalf("SetLegacyAPIKey: %v", err)
	}
	key, err := s.GetAPIKey(ctx, "legacy-config")
	if err != nil {
		t.Fatalf("GetAPIKey(legacy): %v", err)
	}
	if key.UserID != store.LegacyAPIKeyUserID {
		t.Fatalf("legacy user_id = %q, want reserved %q", key.UserID, store.LegacyAPIKeyUserID)
	}

	// Ordinary user-scoped revocation cannot touch the legacy row.
	if err := s.RevokeAPIKey(ctx, "legacy-config", "user1", now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cross-user legacy revoke err = %v, want ErrNotFound", err)
	}
	// The reserved identity revokes it.
	if err := s.RevokeAPIKey(ctx, "legacy-config", store.LegacyAPIKeyUserID, now); err != nil {
		t.Fatalf("RevokeAPIKey(legacy): %v", err)
	}
	revoked, err := s.GetAPIKey(ctx, "legacy-config")
	if err != nil {
		t.Fatalf("GetAPIKey(revoked legacy): %v", err)
	}
	if revoked.RevokedAt == nil {
		t.Fatal("legacy key has no revoked_at after revocation")
	}

	// Re-syncing the same config hash preserves the revocation across
	// restarts instead of resurrecting the key.
	if err := s.SetLegacyAPIKey(ctx, "legacy-digest", now.Add(time.Minute)); err != nil {
		t.Fatalf("SetLegacyAPIKey(resync): %v", err)
	}
	still, err := s.GetAPIKey(ctx, "legacy-config")
	if err != nil {
		t.Fatalf("GetAPIKey(after resync): %v", err)
	}
	if still.RevokedAt == nil {
		t.Fatal("legacy revocation was lost across config resync")
	}

	// A rotated config hash is a new credential and starts unrevoked.
	if err := s.SetLegacyAPIKey(ctx, "rotated-digest", now.Add(2*time.Minute)); err != nil {
		t.Fatalf("SetLegacyAPIKey(rotated): %v", err)
	}
	fresh, err := s.GetAPIKey(ctx, "legacy-config")
	if err != nil {
		t.Fatalf("GetAPIKey(rotated): %v", err)
	}
	if fresh.RevokedAt != nil {
		t.Fatalf("rotated legacy key still revoked at %v", fresh.RevokedAt)
	}
	if fresh.Hash != "rotated-digest" {
		t.Fatalf("rotated legacy hash = %q", fresh.Hash)
	}

	// Removing the config key deletes the row.
	if err := s.SetLegacyAPIKey(ctx, "", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("SetLegacyAPIKey(clear): %v", err)
	}
	if _, err := s.GetAPIKey(ctx, "legacy-config"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("cleared legacy key err = %v, want ErrNotFound", err)
	}
}
