// This file exists to demonstrate the Go testing patterns you'll use
// everywhere in this project. Read it before writing your first test in
// Phase 1 — most patterns generalize.
//
// Topics demonstrated here:
//   - Table-driven tests (the Go idiom — read about it in Effective Go)
//   - t.Run subtests for grouping related cases
//   - Setup helpers
//   - Testing env-var-driven code with t.Setenv (auto-cleanup)
//   - Asserting both happy path and validation errors

package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	// t.Setenv is a Go 1.17+ helper that sets an env var and automatically
	// restores it after the test. NEVER use os.Setenv in tests — it leaks
	// across tests and creates non-deterministic failures.

	// Clear any inherited vars so this test exercises true defaults.
	t.Setenv("PULSE_SERVER_ADDR", "")
	t.Setenv("PULSE_SERVER_READ_TIMEOUT", "")
	t.Setenv("PULSE_LOG_LEVEL", "")
	t.Setenv("PULSE_LOG_FORMAT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error on defaults: %v", err)
	}

	// Assert defaults match what the docs / .env.example claim.
	// If a default changes, this test forces you to update both at once.
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
	// Table-driven test — the Go idiom. Each row is a test case. The
	// benefit: adding a case is one row of data, not a copy-pasted block.

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
		// t.Run nests each case as a subtest. Benefits: failures point to
		// the exact case ("TestLoad_FromEnv/invalid_log_format_rejected"),
		// and you can run a single case with `go test -run` filtering.
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

// TestLoad_BadDuration_FallsBackToDefault documents existing behavior:
// if a duration env var fails to parse, Load() silently uses the default
// rather than erroring out. This is intentional (see comments in config.go)
// but is the kind of behavior you want PINNED with a test — so if a future
// "improvement" changes it, you find out immediately.
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
