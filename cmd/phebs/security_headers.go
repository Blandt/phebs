package main

import "net/http"

// securityHeadersMiddleware wraps an HTTP handler with hardening response
// headers. enabled=false passes the handler through unchanged, for operators
// who manage these headers at a reverse proxy instead (see
// server.security_headers in docs/config.example.yaml).
func securityHeadersMiddleware(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "no-referrer")
			// Conservative policy that keeps the embedded UI working: the Vite
			// bundle loads scripts/styles from 'self', React inline style
			// attributes need 'unsafe-inline' on style-src, and
			// fonts/images may use data: URIs. API and MCP JSON responses are
			// unaffected by these headers. HSTS is deliberately absent: docs
			// tell operators to terminate TLS at a reverse proxy, and a
			// plaintext loopback default must never emit HSTS.
			h.Set(
				"Content-Security-Policy",
				"default-src 'self'; "+
					"script-src 'self'; "+
					"style-src 'self' 'unsafe-inline'; "+
					"img-src 'self' data:; "+
					"font-src 'self' data:; "+
					"connect-src 'self'; "+
					"frame-ancestors 'none'; "+
					"base-uri 'self'; "+
					"form-action 'self'",
			)
			next.ServeHTTP(w, r)
		})
	}
}
