// Command server is the Pulse Chat API and WebSocket server. It is the
// composition root: it loads config, builds the logger and Hub, wires the HTTP
// routes, and runs the server until shutdown. Each package below it receives
// its collaborators through constructors; main does the wiring.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/NoahStarkenburg/pulse-chat/internal/auth"
	"github.com/NoahStarkenburg/pulse-chat/internal/bus"
	"github.com/NoahStarkenburg/pulse-chat/internal/cache"
	"github.com/NoahStarkenburg/pulse-chat/internal/chat"
	"github.com/NoahStarkenburg/pulse-chat/internal/config"
	"github.com/NoahStarkenburg/pulse-chat/internal/store"
)

// presenceSweepInterval is how often each instance reclaims stale presence
// entries from Redis. The sweep is idempotent, so every instance can run it
// without a lock.
const presenceSweepInterval = 30 * time.Second

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

	// Connect to Redis before serving. From Phase 3 on it is a hard dependency:
	// the message bus (cross-instance fan-out), the cache, and sessions all run
	// through it, so fail fast at startup if it is missing or unreachable.
	if cfg.Redis.URL == "" {
		return fmt.Errorf("PULSE_REDIS_URL is required (set it in your environment or .env)")
	}
	// One Redis client, shared by the bus, the cache, and the session store.
	// go-redis clients are safe for concurrent use and pool connections
	// internally, so a single shared client is the right default; main owns its
	// lifecycle and closes it once.
	redisOpt, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("parsing redis url: %w", err)
	}
	redisClient := redis.NewClient(redisOpt)
	if err := redisClient.Ping(ctx).Err(); err != nil {
		_ = redisClient.Close()
		return fmt.Errorf("connecting to redis: %w", err)
	}
	defer redisClient.Close()
	logger.Info("connected to redis")

	msgBus := bus.NewRedisPubSub(redisClient)
	msgCache := cache.New(redisClient)

	// Start the Hub. Keep hubDone so shutdown can wait for it to finish draining
	// WebSocket connections, which srv.Shutdown does not do (see below).
	hub := chat.NewHub(logger, msgBus)
	hubDone := make(chan struct{})
	go func() {
		hub.Run(ctx)
		close(hubDone)
	}()

	// Reclaim stale presence entries periodically. The sweep is idempotent and
	// safe on every instance, so it needs no leader election.
	go func() {
		ticker := time.NewTicker(presenceSweepInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweepCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
				if err := msgCache.SweepStalePresence(sweepCtx); err != nil {
					logger.Warn("presence sweep failed", "err", err)
				}
				cancel()
			}
		}
	}()

	mux := http.NewServeMux()

	// Users live in Postgres (Phase 2).
	users := auth.NewPostgresUserStore(pool)
	// Sessions live in Redis (the shared client) so every instance validates
	// against the same store, they survive a restart, and expiry rides on the key
	// TTL.
	sessions := auth.NewRedisSessionStore(redisClient)
	authHandlers := auth.NewHandlers(users, sessions, msgCache, logger, cfg.Server.CookieSecure)
	requireAuth := auth.RequireAuth(sessions)
	messages := store.NewMessageRepo(pool)

	// resolveSender turns the authenticated user ID (placed in the request
	// context by requireAuth) into what the chat layer needs: the id for the
	// messages foreign key and the display name to stamp as sender.
	resolveSender := func(r *http.Request) (string, string, bool) {
		userID, ok := auth.UserIDFromContext(r.Context())
		if !ok {
			return "", "", false
		}
		u, err := users.ByID(r.Context(), userID)
		if err != nil {
			return "", "", false
		}
		return u.ID, u.Username, true
	}

	// /healthz reports the process is alive; no dependency checks.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	// /readyz reports readiness to serve. Postgres (Phase 2) and Redis (Phase 3)
	// are both hard dependencies, so a failed ping on either returns 503 and a
	// load balancer or Kubernetes stops routing traffic here until it recovers.
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		pingCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			http.Error(w, "not ready: postgres", http.StatusServiceUnavailable)
			return
		}
		if err := msgBus.Ping(pingCtx); err != nil {
			http.Error(w, "not ready: redis", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})

	mux.HandleFunc("POST /signup", authHandlers.Signup)
	mux.HandleFunc("POST /login", authHandlers.Login)
	mux.HandleFunc("POST /logout", authHandlers.Logout)
	mux.Handle("GET /me", requireAuth(http.HandlerFunc(authHandlers.Me)))

	// /ws requires a valid session; the sender comes from it, not the URL.
	mux.Handle("GET /ws", requireAuth(chat.NewWebSocketHandler(hub, messages, msgCache, logger, resolveSender)))

	// Room presence: who is online in a room right now, served from Redis. Behind
	// auth like /ws, since only members should see a room's roster.
	mux.Handle("GET /rooms/{room}/online", requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		room := r.PathValue("room")
		if room == "" {
			http.Error(w, "missing room", http.StatusBadRequest)
			return
		}
		readCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		online, err := msgCache.Online(readCtx, room)
		if err != nil {
			logger.Error("reading presence failed", "err", err, "room", room)
			http.Error(w, "could not read presence", http.StatusInternalServerError)
			return
		}
		if online == nil {
			online = []string{}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"room":   room,
			"online": online,
			"count":  len(online),
		})
	})))

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
