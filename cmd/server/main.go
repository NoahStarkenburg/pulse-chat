// Command server is the Pulse Chat API and WebSocket server. It is the
// composition root: it loads config, builds the logger and Hub, wires the HTTP
// routes, and runs the server until shutdown. Each package below it receives
// its collaborators through constructors; main does the wiring.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/NoahStarkenburg/pulse-chat/internal/chat"
	"github.com/NoahStarkenburg/pulse-chat/internal/config"
)

func main() {
	// run returns an error so it is testable in isolation; main only maps it to
	// an exit code.
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)
	logger.Info("starting pulse-chat",
		"addr", cfg.Server.Addr,
		"log_level", cfg.Log.Level,
		"log_format", cfg.Log.Format,
	)

	// Cancelled on SIGINT/SIGTERM. The Hub uses this as its stop condition and
	// drains clients on cancel.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start the Hub. Keep hubDone so shutdown can wait for it to finish draining
	// WebSocket connections, which srv.Shutdown does not do (see below).
	hub := chat.NewHub(logger)
	hubDone := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(hubDone)
	}()

	mux := http.NewServeMux()

	// /healthz reports the process is alive; no dependency checks.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// /readyz reports readiness to serve. Phase 1 has no dependencies, so it is
	// always ready; later phases will check Postgres/Redis/RabbitMQ here.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	mux.Handle("GET /ws", chat.NewWebSocketHandler(hub, logger))

	// Serve the browser test page. http.Dir is relative to the working
	// directory, so run from the repo root. In production this belongs on a CDN.
	mux.Handle("GET /", http.FileServer(http.Dir("frontend/static")))

	// Timeouts apply to the WebSocket handshake, not the long-lived connection
	// that follows. A server without them is a slow-client DoS target.
	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  120 * time.Second,
	}

	// Run the listener in a goroutine so main can also wait for shutdown signals.
	// ListenAndServe returns ErrServerClosed on a clean Shutdown, which is not an
	// error.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http listener up", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("http server: %w", err)
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining...")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	// Drain HTTP. Shutdown does not close hijacked connections, and every
	// WebSocket is hijacked, so this does not touch chat connections.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	// Drain WebSockets via the Hub. Cancelling ctx already triggered Run to close
	// every client's outbound channel and tear down the pumps; wait for it to
	// finish, bounded by the shutdown deadline.
	select {
	case <-hubDone:
		logger.Info("hub drained all websocket connections")
	case <-shutdownCtx.Done():
		logger.Warn("shutdown deadline reached before hub finished draining")
	}

	logger.Info("shutdown complete")
	return nil
}

// newLogger builds a slog.Logger from the LogConfig.
func newLogger(cfg config.LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}

	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler).With("service", "pulse-chat")
}
