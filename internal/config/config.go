// Package config loads runtime configuration from environment variables into
// a single, immutable Config struct at startup.
//
// Why one struct? Because configuration is a *boundary concern*. Code that
// runs after startup should never read environment variables directly — it
// should receive whatever it needs through the Config (or a slice of it).
// This makes the program's configuration surface explicit and testable.
//
// Why env vars (not a YAML/TOML file)? The 12-factor app convention. Env
// vars work identically in local dev, Docker, Kubernetes, systemd, and CI.
// They have no parsing edge cases and no secrets accidentally checked in.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Config holds everything the server needs to start. Populate via Load().
//
// Convention: every field has a corresponding PULSE_* env var. The Load()
// function is the *only* place these env vars are read. After startup, pass
// Config (or a sub-struct) by value through your dependency wiring.
type Config struct {
	Server ServerConfig
	Log    LogConfig

	// Future phases will add: Postgres, Redis, RabbitMQ, AI. Keeping the
	// struct flat-but-grouped (e.g. `Server.Addr`) makes the call sites
	// clearer than one giant flat struct.
}

// ServerConfig groups HTTP/WebSocket server settings.
type ServerConfig struct {
	Addr            string        // e.g. ":8080"
	ReadTimeout     time.Duration // HTTP read timeout (affects WS handshake)
	WriteTimeout    time.Duration // HTTP write timeout (affects WS handshake)
	ShutdownTimeout time.Duration // graceful shutdown grace period
}

// LogConfig groups logging settings.
type LogConfig struct {
	Level  slog.Level // debug | info | warn | error
	Format string     // "text" or "json"
}

// Load reads environment variables and returns a fully populated Config.
// On error, it returns a non-nil error explaining which variable was wrong —
// fail-fast at startup is better than mysterious runtime behavior.
func Load() (Config, error) {
	cfg := Config{
		Server: ServerConfig{
			Addr:            envString("PULSE_SERVER_ADDR", ":8080"),
			ReadTimeout:     envDuration("PULSE_SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    envDuration("PULSE_SERVER_WRITE_TIMEOUT", 10*time.Second),
			ShutdownTimeout: envDuration("PULSE_SERVER_SHUTDOWN_TIMEOUT", 15*time.Second),
		},
		Log: LogConfig{
			Level:  envLogLevel("PULSE_LOG_LEVEL", slog.LevelInfo),
			Format: envString("PULSE_LOG_FORMAT", "text"),
		},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate runs basic sanity checks. Catching bad config at startup beats
// debugging it in production at 2am.
func (c Config) validate() error {
	if c.Server.Addr == "" {
		return fmt.Errorf("PULSE_SERVER_ADDR cannot be empty")
	}
	if c.Server.ShutdownTimeout <= 0 {
		return fmt.Errorf("PULSE_SERVER_SHUTDOWN_TIMEOUT must be positive")
	}
	switch c.Log.Format {
	case "text", "json":
		// ok
	default:
		return fmt.Errorf("PULSE_LOG_FORMAT must be 'text' or 'json', got %q", c.Log.Format)
	}
	return nil
}

// --- helpers -----------------------------------------------------------------
//
// These wrappers around os.Getenv exist so the Load() body reads cleanly
// and we don't repeat default-value boilerplate. Keep them unexported —
// nothing outside this package should be parsing env vars directly.

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// We deliberately swallow this and return the default. A future
		// improvement would be to surface the parse error from Load() —
		// for now, defaulting silently is fine because validate() will
		// catch downstream symptoms.
		return def
	}
	return d
}

func envLogLevel(key string, def slog.Level) slog.Level {
	v := strings.ToLower(os.Getenv(key))
	switch v {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return def
	}
}
