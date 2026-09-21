package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bmeddeb/phebs/internal/config"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("ok"))
	})
	yes, no := true, false
	tests := []struct {
		name   string
		server config.Server
	}{
		{name: "default enables headers", server: config.Server{}},
		{name: "explicit true enables headers", server: config.Server{SecurityHeaders: &yes}},
		{name: "explicit false passes through", server: config.Server{SecurityHeaders: &no}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			securityHeadersMiddleware(tt.server.SecurityHeadersEnabled())(inner).ServeHTTP(
				rec, httptest.NewRequest(http.MethodGet, "/", nil),
			)
			if rec.Code != http.StatusTeapot || rec.Body.String() != "ok" {
				t.Fatalf("inner handler = %d %q, want 418 ok", rec.Code, rec.Body.String())
			}
			enabled := tt.server.SecurityHeaders == nil || *tt.server.SecurityHeaders
			if !enabled {
				for _, name := range []string{
					"X-Content-Type-Options", "X-Frame-Options",
					"Referrer-Policy", "Content-Security-Policy",
				} {
					if got := rec.Header().Get(name); got != "" {
						t.Errorf("%s = %q, want unset when disabled", name, got)
					}
				}
				return
			}
			want := map[string]string{
				"X-Content-Type-Options": "nosniff",
				"X-Frame-Options":        "DENY",
				"Referrer-Policy":        "no-referrer",
			}
			for name, expected := range want {
				if got := rec.Header().Get(name); got != expected {
					t.Errorf("%s = %q, want %q", name, got, expected)
				}
			}
			csp := rec.Header().Get("Content-Security-Policy")
			for _, directive := range []string{
				"default-src 'self'", "script-src 'self'",
				"frame-ancestors 'none'", "base-uri 'self'",
			} {
				if !strings.Contains(csp, directive) {
					t.Errorf("CSP %q missing directive %q", csp, directive)
				}
			}
			if strings.Contains(csp, "strict-transport-security") ||
				rec.Header().Get("Strict-Transport-Security") != "" {
				t.Errorf("HSTS must stay off (TLS terminates at the reverse proxy): %q", csp)
			}
		})
	}
}
