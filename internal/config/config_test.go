// Tests for config loading: defaults, environment overrides, validation
// errors, and the documented fallback when a duration fails to parse.

package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// t.Setenv sets an env var and restores it after the test. Using os.Setenv
	// in tests leaks across tests and creates non-deterministic failures.

	// Clear any inherited vars so this test exercises true defaults.
	t.Setenv("PULSE_SERVER_ADDR", "")
	t.Setenv("PULSE_SERVER_READ_TIMEOUT", "")
	t.Setenv("PULSE_LOG_LEVEL", "")
	t.Setenv("PULSE_LOG_FORMAT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error on defaults: %v", err)
	}

	// Defaults must match what the docs and .env.example claim; if one changes,
	// this test forces updating both at once.
	if cfg.Server.Addr != ":8080" {
		t.Errorf("Addr: got %q, want %q", cfg.Server.Addr, ":8080")
	}
	if cfg.Server.ReadTimeout != 10*time.Second {
		t.Errorf("ReadTimeout: got %v, want 10s", cfg.Server.ReadTimeout)
	}
	if cfg.Log.Level != slog.LevelInfo {
		t.Errorf("Log.Level: got %v, want Info", cfg.Log.Level)
	}
	if cfg.Log.Format != "text" {
		t.Errorf("Log.Format: got %q, want %q", cfg.Log.Format, "text")
	}
}

func TestLoad_FromEnv(t *testing.T) {
	tests := []struct {
		name    string
		envKey  string
		envVal  string
		check   func(t *testing.T, cfg Config)
		wantErr bool
	}{
		{
			name:   "addr override",
			envKey: "PULSE_SERVER_ADDR",
			envVal: "127.0.0.1:9999",
			check: func(t *testing.T, cfg Config) {
				if cfg.Server.Addr != "127.0.0.1:9999" {
					t.Errorf("Addr = %q", cfg.Server.Addr)
				}
			},
		},
		{
			name:   "json log format",
			envKey: "PULSE_LOG_FORMAT",
			envVal: "json",
			check: func(t *testing.T, cfg Config) {
				if cfg.Log.Format != "json" {
					t.Errorf("Log.Format = %q", cfg.Log.Format)
				}
			},
		},
		{
			name:    "invalid log format rejected",
			envKey:  "PULSE_LOG_FORMAT",
			envVal:  "yaml-but-someone-typo'd",
			wantErr: true,
		},
		{
			name:   "debug log level",
			envKey: "PULSE_LOG_LEVEL",
			envVal: "debug",
			check: func(t *testing.T, cfg Config) {
				if cfg.Log.Level != slog.LevelDebug {
					t.Errorf("Log.Level = %v", cfg.Log.Level)
				}
			},
		},
	}

	for _, tc := range tests {
		// Each case is a subtest so failures name the exact case and a single
		// case can be run with go test -run.
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.envKey, tc.envVal)

			cfg, err := Load()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error: %v", err)
			}
			tc.check(t, cfg)
		})
	}
}

// TestLoad_BadDuration_FallsBackToDefault pins existing behavior: a duration
// env var that fails to parse falls back to the default rather than erroring.
// This is intentional (see config.go), so the test guards against a future
// change silently breaking it.
func TestLoad_BadDuration_FallsBackToDefault(t *testing.T) {
	t.Setenv("PULSE_SERVER_READ_TIMEOUT", "definitely-not-a-duration")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Server.ReadTimeout != 10*time.Second {
		t.Errorf("expected default 10s, got %v", cfg.Server.ReadTimeout)
	}
}
