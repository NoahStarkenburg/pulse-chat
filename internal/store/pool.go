// Package store is the data-access layer: it owns the Postgres connection pool
// and the repositories that read and write durable data. Everything that talks
// to the database lives here, behind small constructors, so the rest of the app
// depends on this package rather than on pgx directly.
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewPool opens a Postgres connection pool from a DSN and verifies the database
// is reachable before returning. The caller owns the returned pool and must
// Close it on shutdown.
//
// We use pgxpool (pgx's native pool) rather than database/sql: it is faster,
// exposes Postgres features directly, and this app has no need for the generic
// database/sql abstraction or an ORM.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres dsn: %w", err)
	}

	// Pool tuning can be set on the DSN (pool_max_conns, pool_max_conn_lifetime,
	// etc.). These only fill in defaults the DSN did not specify. Bounding the
	// connection lifetime matters in production: long-lived connections go stale
	// and recycling them lets the pool follow a database failover or DNS change.
	if cfg.MaxConnLifetime == 0 {
		cfg.MaxConnLifetime = time.Hour
	}
	if cfg.MaxConnIdleTime == 0 {
		cfg.MaxConnIdleTime = 30 * time.Minute
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("creating postgres pool: %w", err)
	}

	// pgxpool does not connect eagerly, so Ping forces one real connection. This
	// makes a bad DSN or an unreachable database fail fast at startup instead of
	// surfacing on the first query under load.
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return pool, nil
}
