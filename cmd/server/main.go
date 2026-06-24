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

	"github.com/NoahStarkenburg/pulse-chat/internal/auth"
	"github.com/NoahStarkenburg/pulse-chat/internal/chat"
	"github.com/NoahStarkenburg/pulse-chat/internal/config"
	"github.com/NoahStarkenburg/pulse-chat/internal/store"
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

	// Connect to Postgres before serving. The database is a hard dependency from
	// Phase 2 on, so fail fast at startup if it is missing or unreachable rather
	// than starting and erroring on the first query.
	if cfg.Postgres.URL == "" {
		return fmt.Errorf("PULSE_POSTGRES_URL is required (set it in your environment or .env)")
	}
	pool, err := store.NewPool(ctx, cfg.Postgres.URL)
	if err != nil {
		return fmt.Errorf("connecting to postgres: %w", err)
	}
	defer pool.Close()
	logger.Info("connected to postgres")

	// Start the Hub. Keep hubDone so shutdown can wait for it to finish draining
	// WebSocket connections, which srv.Shutdown does not do (see below).
	hub := chat.NewHub(logger)
	hubDone := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(hubDone)
	}()

	mux := http.NewServeMux()

	// Auth: in-memory user and session stores for Phase 1.5 (Phase 2 promotes
	// them to Postgres).
	users := auth.NewUserStore()
	sessions := auth.NewSessionStore()
	authHandlers := auth.NewHandlers(users, sessions, logger, cfg.Server.CookieSecure)
	requireAuth := auth.RequireAuth(sessions)

	// resolveSender turns the authenticated user ID (placed in the request
	// context by requireAuth) into the display name the chat stamps as sender.
	resolveSender := func(r *http.Request) (string, bool) {
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			return "", false
		}
		u, err := users.ByID(userID)
		if err != nil {
			return "", false
		}
		return u.Username, true
	}

	// /healthz reports the process is alive; no dependency checks.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// /readyz reports readiness to serve. From Phase 2 the database is a hard
	// dependency, so a failed ping returns 503 and a load balancer or Kubernetes
	// stops routing traffic here until Postgres recovers.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /signup", authHandlers.Signup)
	mux.HandleFunc("POST /login", authHandlers.Login)
	mux.HandleFunc("POST /logout", authHandlers.Logout)
	mux.Handle("GET /me", requireAuth(http.HandlerFunc(authHandlers.Me)))

	// /ws requires a valid session; the sender comes from it, not the URL.
	mux.Handle("GET /ws", requireAuth(chat.NewWebSocketHandler(hub, logger, resolveSender)))

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
