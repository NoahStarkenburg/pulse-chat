// Command migrate applies or rolls back the database schema. Migrations are
// embedded in the binary, so this runs anywhere with no external tool and no
// .sql files to ship.
//
// Usage (reads PULSE_POSTGRES_URL, the same DSN the server uses):
//
//	go run ./cmd/migrate up       # apply all pending migrations
//	go run ./cmd/migrate down     # roll back the most recent migration
//	go run ./cmd/migrate status   # show which migrations are applied
//
// Migrations are run as a separate step (here, or a CI/CD job), never
// automatically on server startup, so many server instances can never race to
// migrate the same database.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/NoahStarkenburg/pulse-chat/internal/store"
	"github.com/NoahStarkenburg/pulse-chat/internal/store/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	dsn := os.Getenv("PULSE_POSTGRES_URL")
	if dsn == "" {
		return fmt.Errorf("PULSE_POSTGRES_URL is required")
	}

	ctx := context.Background()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch command {
	case "up":
		return migrations.Up(ctx, pool)
	case "down":
		return migrations.Down(ctx, pool)
	case "status":
		return migrations.Status(ctx, pool)
	default:
		return fmt.Errorf("unknown command %q (use up, down, or status)", command)
	}
}
