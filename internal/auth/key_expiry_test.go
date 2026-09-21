package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/bmeddeb/phebs/internal/config"
	"github.com/bmeddeb/phebs/internal/store"
)

// establishSession installs a CSRF-protected browser session for userID into
// jar and returns the CSRF token to send on unsafe requests.
func establishSession(t *testing.T, service *Service, jar *cookiejar.Jar, baseURL, userID string) string {
	t.Helper()
	req := serviceRequestContext(t, service, httptest.NewRequest(http.MethodGet, "/", nil))
	if err := service.sessions.RenewToken(req.Context()); err != nil {
		t.Fatal(err)
	}
	const csrf = "test-csrf-token"
	service.sessions.Put(req.Context(), sessionUserID, userID)
	service.sessions.Put(req.Context(), sessionCSRF, csrf)
	token, _, err := service.sessions.Commit(req.Context())
	if err != nil {
		t.Fatal(err)
	}
	jar.SetCookies(mustURL(t, baseURL), []*http.Cookie{{
		Name: "phebs_session", Value: token, Path: "/",
	}})
	return csrf
}

func seedUser(t *testing.T, st *memoryAuthStore, id string, isAdmin bool) {
	t.Helper()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.users[id] = store.User{
		ID: id, Email: id + "@example.com", NormalizedEmail: id + "@example.com",
		IsAdmin: isAdmin,
	}
}

func TestCreateKeyAcceptsExpiresAtAndEnforcesIt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	current := now
	st := newMemoryAuthStore()
	service, err := New(ctx, Options{
		Store:            st,
		Config:           config.Auth{CookieSecure: insecureCookieConfig()},
		Now:              func() time.Time { return current },
		ArgonConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedUser(t, st, "admin", true)
	server := httptest.NewServer(service.LoadAndSave(service.Handler()))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := establishSession(t, service, jar, server.URL, "admin")

	expiry := now.Add(24 * time.Hour)
	response := request(t, client, http.MethodPost, server.URL+"/api/auth/keys",
		`{"name":"expiring","expires_at":`+strconv.Quote(expiry.Format(time.RFC3339))+`}`,
		csrf, "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create expiring key = %d: %s", response.StatusCode, readBody(response))
	}
	var created struct {
		Key   keyResponse `json:"key"`
		Token string      `json:"token"`
	}
	decodeResponse(t, response, &created)
	if created.Key.ExpiresAt == nil || !created.Key.ExpiresAt.Equal(expiry) {
		t.Fatalf("created key expires_at = %v, want %v", created.Key.ExpiresAt, expiry)
	}
	st.mu.Lock()
	storedExpiry := st.keys[created.Key.ID].ExpiresAt
	st.mu.Unlock()
	if storedExpiry == nil || !storedExpiry.Equal(expiry) {
		t.Fatalf("stored key expires_at = %v, want %v", storedExpiry, expiry)
	}
	if _, err := service.authenticateBearer(ctx, created.Token); err != nil {
		t.Fatalf("unexpired key rejected: %v", err)
	}

	// An expired key must fail authentication even though its hash still
	// matches and it was never revoked.
	current = now.Add(25 * time.Hour)
	if _, err := service.authenticateBearer(ctx, created.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("expired key authentication error = %v, want unauthenticated", err)
	}
	current = now

	// A key created without expires_at keeps working: expiry is optional.
	response = request(t, client, http.MethodPost, server.URL+"/api/auth/keys",
		`{"name":"perpetual"}`, csrf, "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create perpetual key = %d: %s", response.StatusCode, readBody(response))
	}
	var perpetual struct {
		Key   keyResponse `json:"key"`
		Token string      `json:"token"`
	}
	decodeResponse(t, response, &perpetual)
	if perpetual.Key.ExpiresAt != nil {
		t.Fatalf("perpetual key expires_at = %v, want nil", perpetual.Key.ExpiresAt)
	}
	if _, err := service.authenticateBearer(ctx, perpetual.Token); err != nil {
		t.Fatalf("perpetual key rejected: %v", err)
	}
}

func TestCreateKeyRejectsBadExpiresAt(t *testing.T) {
	ctx := context.Background()
	st := newMemoryAuthStore()
	service, err := New(ctx, Options{
		Store:            st,
		Config:           config.Auth{CookieSecure: insecureCookieConfig()},
		ArgonConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	seedUser(t, st, "admin", true)
	server := httptest.NewServer(service.LoadAndSave(service.Handler()))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	csrf := establishSession(t, service, jar, server.URL, "admin")

	for _, test := range []struct {
		name string
		body string
		want int
	}{
		{"past", `{"name":"past","expires_at":"2020-01-01T00:00:00Z"}`, http.StatusBadRequest},
		{"malformed", `{"name":"malformed","expires_at":"next friday"}`, http.StatusBadRequest},
		{"empty_string", `{"name":"empty","expires_at":""}`, http.StatusBadRequest},
		{"explicit_null", `{"name":"null","expires_at":null}`, http.StatusCreated},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := request(t, client, http.MethodPost, server.URL+"/api/auth/keys",
				test.body, csrf, "")
			if response.StatusCode != test.want {
				t.Fatalf("create key %s = %d: %s, want %d",
					test.name, response.StatusCode, readBody(response), test.want)
			} else {
				_ = response.Body.Close()
			}
		})
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.keys) != 1 {
		t.Fatalf("refused requests created %d keys, want 1", len(st.keys))
	}
	for _, key := range st.keys {
		if key.ExpiresAt != nil {
			t.Fatalf("explicit null expires_at stored %v, want nil", key.ExpiresAt)
		}
	}
}

func bearerPrincipal(t *testing.T, service *Service, token string) Principal {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	principal, err := service.Authenticate(serviceRequestContext(t, service, req))
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func TestRevokeLegacyKeyViaAPI(t *testing.T) {
	ctx := context.Background()
	st := newMemoryAuthStore()
	const legacyToken = "legacy-admin-token"
	options := Options{
		Store:            st,
		Config:           config.Auth{APIKey: legacyToken, CookieSecure: insecureCookieConfig()},
		ArgonConcurrency: 1,
	}
	service, err := New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}

	// The legacy row carries the reserved identity, not a user.
	st.mu.Lock()
	legacy := st.keys[legacyKeyID]
	st.mu.Unlock()
	if legacy.UserID != store.LegacyAPIKeyUserID {
		t.Fatalf("legacy key user_id = %q, want reserved %q", legacy.UserID, store.LegacyAPIKeyUserID)
	}

	seedUser(t, st, "admin", true)
	seedUser(t, st, "peon", false)
	server := httptest.NewServer(service.LoadAndSave(service.Handler()))
	defer server.Close()
	adminJar, _ := cookiejar.New(nil)
	peonJar, _ := cookiejar.New(nil)
	adminClient := &http.Client{Jar: adminJar}
	peonClient := &http.Client{Jar: peonJar}
	adminCSRF := establishSession(t, service, adminJar, server.URL, "admin")
	peonCSRF := establishSession(t, service, peonJar, server.URL, "peon")

	if principal := bearerPrincipal(t, service, legacyToken); !principal.IsAdmin || principal.User != nil {
		t.Fatalf("legacy principal = %+v, want admin without user", principal)
	}

	// A non-administrator cannot revoke the legacy key and learns nothing
	// beyond the not-found response used for any out-of-scope key id.
	response := request(t, peonClient, http.MethodDelete,
		server.URL+"/api/auth/keys/"+legacyKeyID, "", peonCSRF, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("non-admin legacy revoke = %d, want 404", response.StatusCode)
	}
	_ = response.Body.Close()
	if principal := bearerPrincipal(t, service, legacyToken); !principal.IsAdmin {
		t.Fatal("non-admin revoke attempt disabled the legacy key")
	}

	// An administrator revokes it; the bearer stops authenticating.
	response = request(t, adminClient, http.MethodDelete,
		server.URL+"/api/auth/keys/"+legacyKeyID, "", adminCSRF, "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("admin legacy revoke = %d: %s", response.StatusCode, readBody(response))
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+legacyToken)
	if _, err := service.Authenticate(serviceRequestContext(t, service, req)); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("revoked legacy key authentication error = %v, want unauthenticated", err)
	}
	// Revoking twice reports not-found, matching named-key behavior.
	response = request(t, adminClient, http.MethodDelete,
		server.URL+"/api/auth/keys/"+legacyKeyID, "", adminCSRF, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("second legacy revoke = %d, want 404", response.StatusCode)
	}
	_ = response.Body.Close()

	// A restart with the same config key preserves the revocation; rotating
	// the config key clears it.
	restarted, err := New(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.authenticateBearer(ctx, legacyToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("restarted service accepted revoked legacy key: %v", err)
	}
	const rotatedToken = "rotated-admin-token"
	rotated, err := New(ctx, Options{
		Store: st,
		Config: config.Auth{
			APIKey:       rotatedToken,
			CookieSecure: insecureCookieConfig(),
		},
		ArgonConcurrency: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rotated.authenticateBearer(ctx, rotatedToken); err != nil {
		t.Fatalf("rotated legacy key rejected: %v", err)
	}
	if _, err := rotated.authenticateBearer(ctx, legacyToken); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("old legacy token survived rotation: %v", err)
	}

	// Ordinary user-scoped revocation is unaffected: a sibling user cannot
	// revoke another user's key, and owners still can.
	response = request(t, adminClient, http.MethodPost, server.URL+"/api/auth/keys",
		`{"name":"admin-owned"}`, adminCSRF, "")
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create admin key = %d: %s", response.StatusCode, readBody(response))
	}
	var created struct {
		Key   keyResponse `json:"key"`
		Token string      `json:"token"`
	}
	decodeResponse(t, response, &created)
	response = request(t, peonClient, http.MethodDelete,
		server.URL+"/api/auth/keys/"+created.Key.ID, "", peonCSRF, "")
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("cross-user revoke = %d, want 404", response.StatusCode)
	}
	_ = response.Body.Close()
	response = request(t, adminClient, http.MethodDelete,
		server.URL+"/api/auth/keys/"+created.Key.ID, "", adminCSRF, "")
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("owner revoke = %d: %s", response.StatusCode, readBody(response))
	}
}
