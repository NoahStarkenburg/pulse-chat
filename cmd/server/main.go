// Command server is the Pulse Chat API + WebSocket server.
//
// This file is the "outer ring" of the application. It is responsible for:
//
//   1. Loading configuration (from env vars).
//   2. Setting up cross-cutting concerns (logger, graceful-shutdown signal
//      handling).
//   3. Wiring together the packages that do the actual work.
//   4. Starting the HTTP server and blocking until shutdown.
//
// Pattern: main() is the *composition root*. It is the only place that knows
// about every dependency. Each package below it (chat, store, bus, etc.)
// should know nothing about the others — they receive their collaborators
// through constructors, and main() does the wiring.
//
// This pattern is sometimes called "dependency injection by hand." We don't
// use a DI framework. In Go, plain constructor calls are clearer.
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

	"github.com/NoahStarkenburg/pulse-chat/internal/config"
)

func main() {
	// Run returns an error so it can be unit-tested in isolation (you can
	// call run() from a test without it calling os.Exit). main() exists only
	// to translate the error into a process exit code.
	if err := run(); err != nil {
		// Use slog if it's available; otherwise fall back to stderr.
		// At this point we're exiting, so simplicity wins over consistency.
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// --- 1. Load configuration --------------------------------------------
	// Done first so logging can use the configured level/format.
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// --- 2. Set up the logger --------------------------------------------
	// log/slog is the standard library structured logger introduced in Go
	// 1.21. It supports leveled logging and either text or JSON output.
	//
	// We set the *default* logger so that any code in this binary which
	// calls slog.Info/etc. without a handler will use our setup.
	logger := newLogger(cfg.Log)
	slog.SetDefault(logger)

	logger.Info("starting pulse-chat",
		"addr", cfg.Server.Addr,
		"log_level", cfg.Log.Level,
		"log_format", cfg.Log.Format,
	)

	// --- 3. Build the HTTP router ----------------------------------------
	// We use net/http directly here — it is enough for Phase 1. When the
	// route count grows we'll introduce chi (already noted in README). The
	// chat WebSocket handler will be wired up here in Phase 1 work.
	mux := http.NewServeMux()

	// /healthz — process is alive. No dependency checks.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// /readyz — ready to serve traffic. In later phases this will check
	// Postgres, Redis, RabbitMQ reachability and 503 if any are down.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		// Phase 1 has no dependencies — always ready.
		_, _ = w.Write([]byte("ok"))
	})

	// TODO(phase-1): mount the WebSocket handler.
	//
	// You will:
	//   1. Construct a chat.Hub and start its Run() goroutine.
	//   2. Build an http.HandlerFunc that upgrades the request to a
	//      WebSocket and registers a chat.Client with the Hub.
	//   3. Mount it at "GET /ws".
	//
	// Example (pseudo-code):
	//
	//     hub := chat.NewHub(logger)
	//     go hub.Run(ctx)
	//     mux.Handle("GET /ws", chat.NewWebSocketHandler(hub, logger))
	//
	// See internal/chat/*.go for the skeleton and the lesson plan.

	// --- 4. Configure the HTTP server ------------------------------------
	// Timeouts matter. A naively-configured server is a DoS target — slow
	// clients can hold connections open forever. ReadTimeout / WriteTimeout
	// bound that. Note: these timeouts apply to the *handshake* of a
	// WebSocket connection, not the long-lived WS conversation that follows.
	srv := &http.Server{
		Addr:         cfg.Server.Addr,
		Handler:      mux,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		// IdleTimeout: keep-alive timeout for HTTP/1.1 connections.
		IdleTimeout: 120 * time.Second,
	}

	// --- 5. Start the server in a goroutine ------------------------------
	// We can't call srv.ListenAndServe() directly in main() because we also
	// need to wait for OS signals to trigger shutdown. So we launch the
	// server in a goroutine and report its error on a channel.
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http listener up", "addr", cfg.Server.Addr)
		// ListenAndServe blocks until the server is closed. It returns
		// http.ErrServerClosed when Shutdown() is called, which is *not*
		// an error condition — we filter it out below.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- fmt.Errorf("http server: %w", err)
			return
		}
		serverErr <- nil
	}()

	// --- 6. Wait for a shutdown signal -----------------------------------
	// signal.NotifyContext returns a context that is cancelled when the
	// process receives SIGINT (Ctrl+C) or SIGTERM (kill, docker stop, k8s).
	//
	// Using a context for this (instead of a raw channel) is the modern
	// idiom — it composes cleanly with other context-aware code.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Block until either:
	//   a) the server errors out, or
	//   b) we receive a shutdown signal.
	select {
	case err := <-serverErr:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutdown signal received, draining...")
	}

	// --- 7. Graceful shutdown --------------------------------------------
	// Give the server up to ShutdownTimeout to finish in-flight requests.
	// Shutdown() stops accepting new connections, waits for active ones to
	// finish, then returns. If the timeout expires, Shutdown() returns the
	// context's error and any leftover connections are dropped.
	//
	// In later phases we will also gracefully close: WebSocket connections,
	// Postgres pools, Redis subscriptions, RabbitMQ channels. Each of those
	// should be triggered here in reverse-construction order.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("shutdown complete")
	return nil
}

// newLogger builds a slog.Logger from the LogConfig. We extract this so the
// run() function reads top-to-bottom without a helper getting in the way.
func newLogger(cfg config.LogConfig) *slog.Logger {
	opts := &slog.HandlerOptions{Level: cfg.Level}

	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opts)
	default:
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	// Attach a permanent attribute identifying the service. In a multi-
	// service environment, your log aggregator (Loki, Datadog, etc.) will
	// thank you for this.
	return slog.New(handler).With("service", "pulse-chat")
}
