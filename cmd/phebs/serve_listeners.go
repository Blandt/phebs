package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"time"
)

// runServeListener starts the HTTP server: graceful-shutdown wiring, TCP
// listen, then blocking serve. On success the startup owner turn is completed.
func runServeListener(d *serveDeps, handler http.Handler) error {
	ctx := d.ctx
	cfg := d.cfg

	// Keep the server-wide WriteTimeout at zero: it is an absolute response
	// deadline and would terminate legitimate SSE and MCP streams. Those
	// handlers retain their own bounded work deadlines.
	srv := &http.Server{
		Addr: cfg.Server.Addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		IdleTimeout:       120 * time.Second,
		BaseContext:       t421ExactReadServerBaseContext(ctx, d.exactReads),
	}
	shutdownErr := make(chan error, 1)
	d.runBackground(func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr <- srv.Shutdown(shutdownCtx)
	})

	listener, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		d.stopBackground()
		return serverTerminalError(
			err, <-shutdownErr, d.exact.reportFailed, d.exact.readFailed,
		)
	}
	log.Printf("phebs %s listening on %s (data: %s)", version, cfg.Server.Addr, cfg.Server.DataDir)
	reportT4013Startup("http_ready")
	d.startup.complete()
	serveErr := srv.Serve(listener)
	// Shutdown waits at most five seconds. Exact modes retain a timeout as a
	// terminal error; ordinary mode preserves its existing shutdown behavior.
	d.stopBackground()
	return serverTerminalError(
		serveErr, <-shutdownErr, d.exact.reportFailed, d.exact.readFailed,
	)
}
