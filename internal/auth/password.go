// Package auth provides password hashing, opaque server-side sessions, and the
// HTTP middleware and handlers that tie them together.
//
// Phase 1.5 keeps the user and session stores in memory; Phase 2 promotes them
// to Postgres. See decisions/0005-session-auth.md.
package auth

import (
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost controls how expensive each hash is. 12 is roughly 250ms per hash
// on current hardware: slow enough to blunt offline brute force, fast enough
// for interactive login. Raise it as hardware gets faster.
const bcryptCost = 12

// HashPassword returns a bcrypt hash of plaintext. The salt is generated
// internally and embedded in the returned string, so no separate salt storage
// is needed. bcrypt rejects inputs longer than 72 bytes; callers validate
// length before reaching here.
func HashPassword(plaintext string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword reports whether plaintext matches hash. It returns nil on a
// match and a non-nil error otherwise. The comparison is constant-time within
// bcrypt, so it does not leak timing information about the password.
func VerifyPassword(plaintext, hash string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plaintext))
}

// dummyPasswordHash is a precomputed hash the login path verifies against when
// the username does not exist. Running a real bcrypt comparison either way keeps
// login timing constant, so an attacker cannot enumerate usernames by measuring
// response time. The inputs are fixed and valid, so the error cannot occur.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcryptCost)
