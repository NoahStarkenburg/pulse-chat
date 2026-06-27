package migrations

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Each .sql file has two sections split by these marker lines. Everything
// between the up marker and the down marker is the forward migration; everything
// after the down marker is the rollback.
const (
	upMarker   = "-- migrate:up"
	downMarker = "-- migrate:down"
)

type migration struct {
	version int
	name    string
	up      string
	down    string
}

// load parses every embedded .sql file into migrations ordered by version. The
// version is the numeric filename prefix, e.g. 00001_create_users.sql -> 1.
func load() ([]migration, error) {
	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return nil, err
	}
	var ms []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		version, err := strconv.Atoi(strings.SplitN(e.Name(), "_", 2)[0])
		if err != nil {
			return nil, fmt.Errorf("migration %q has no numeric version prefix", e.Name())
		}
		content, err := fs.ReadFile(FS, e.Name())
		if err != nil {
			return nil, err
		}
		up, down, err := split(string(content))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", e.Name(), err)
		}
		ms = append(ms, migration{version: version, name: e.Name(), up: up, down: down})
	}
	sort.Slice(ms, func(i, j int) bool { return ms[i].version < ms[j].version })
	return ms, nil
}

func split(content string) (up, down string, err error) {
	ui := strings.Index(content, upMarker)
	di := strings.Index(content, downMarker)
	if ui < 0 || di < 0 || di < ui {
		return "", "", fmt.Errorf("missing %q / %q markers", upMarker, downMarker)
	}
	up = strings.TrimSpace(content[ui+len(upMarker) : di])
	down = strings.TrimSpace(content[di+len(downMarker):])
	return up, down, nil
}

// ensureTable creates the bookkeeping table that records which versions are
// applied. It is itself idempotent (IF NOT EXISTS).
func ensureTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    BIGINT      PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

func appliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[int]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := make(map[int]bool)
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// Up applies every pending migration in version order. Each runs inside a
// transaction together with its bookkeeping insert, so a failure leaves the
// database exactly as it was (Postgres DDL is transactional).
func Up(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureTable(ctx, pool); err != nil {
		return err
	}
	ms, err := load()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}
	for _, m := range ms {
		if applied[m.version] {
			continue
		}
		if err := runInTx(ctx, pool, m.up, `INSERT INTO schema_migrations (version) VALUES ($1)`, m.version); err != nil {
			return fmt.Errorf("applying %s: %w", m.name, err)
		}
		fmt.Printf("applied  %s\n", m.name)
	}
	return nil
}

// Down rolls back only the most recently applied migration.
func Down(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureTable(ctx, pool); err != nil {
		return err
	}
	ms, err := load()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}
	var target *migration
	for i := range ms {
		if applied[ms[i].version] {
			target = &ms[i]
		}
	}
	if target == nil {
		fmt.Printf("no migrations to roll back\n")
		return nil
	}
	if err := runInTx(ctx, pool, target.down, `DELETE FROM schema_migrations WHERE version = $1`, target.version); err != nil {
		return fmt.Errorf("rolling back %s: %w", target.name, err)
	}
	fmt.Printf("rolled back %s\n", target.name)
	return nil
}

// Status prints each migration and whether it has been applied.
func Status(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureTable(ctx, pool); err != nil {
		return err
	}
	ms, err := load()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}
	for _, m := range ms {
		state := "pending"
		if applied[m.version] {
			state = "applied"
		}
		fmt.Printf("%-8s %s\n", state, m.name)
	}
	return nil
}

// runInTx executes the migration SQL and its bookkeeping statement in one
// transaction so the two always commit or roll back together.
func runInTx(ctx context.Context, pool *pgxpool.Pool, migrationSQL, bookkeepingSQL string, version int) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit
	if _, err := tx.Exec(ctx, migrationSQL); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, bookkeepingSQL, version); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
