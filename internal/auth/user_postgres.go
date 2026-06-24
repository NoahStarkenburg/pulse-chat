package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresUserStore persists users in the users table.
type PostgresUserStore struct {
	pool *pgxpool.Pool
}

// NewPostgresUserStore returns a store backed by the given pool.
func NewPostgresUserStore(pool *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{pool: pool}
}

// Create inserts a user. A unique-violation on username is mapped to
// ErrUserExists so callers can tell "username taken" from a real failure.
func (s *PostgresUserStore) Create(ctx context.Context, username, passwordHash string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2)
		 RETURNING id::text, username, password_hash, created_at`,
		username, passwordHash,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return nil, ErrUserExists
		}
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return &u, nil
}

// ByUsername looks up a user by username.
func (s *PostgresUserStore) ByUsername(ctx context.Context, username string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, username, password_hash, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	return userOrErr(&u, err)
}

// ByID looks up a user by ID.
func (s *PostgresUserStore) ByID(ctx context.Context, id string) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		`SELECT id::text, username, password_hash, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	return userOrErr(&u, err)
}

// userOrErr maps a no-rows result to ErrUserNotFound and wraps anything else.
func userOrErr(u *User, err error) (*User, error) {
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("querying user: %w", err)
	}
	return u, nil
}
