package auth

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors callers can inspect with errors.Is.
var (
	ErrUserExists   = errors.New("username already taken")
	ErrUserNotFound = errors.New("user not found")
)

// User is an account. PasswordHash is a bcrypt hash, never the plaintext.
type User struct {
	ID           string
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

// UserStore persists user accounts. Both MemoryUserStore and PostgresUserStore
// satisfy it, so the handlers depend on this interface rather than a concrete
// type, and the backend can be swapped by changing one line in main. The
// context is here for the database-backed store (cancellation and timeouts).
type UserStore interface {
	Create(ctx context.Context, username, passwordHash string) (*User, error)
	ByUsername(ctx context.Context, username string) (*User, error)
	ByID(ctx context.Context, id string) (*User, error)
}
