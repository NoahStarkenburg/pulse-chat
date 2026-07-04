package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

const (
	sessionTTL        = 24 * time.Hour
	sessionTokenBytes = 32 // 256 bits of entropy; infeasible to guess or brute-force
)

// SessionStore issues, validates, and revokes login sessions. Both
// MemorySessionStore and RedisSessionStore satisfy it, so the handlers and
// middleware depend on this interface and the backend is swapped by one line in
// main. The context is here for the Redis-backed store (cancellation and
// timeouts); the in-memory store ignores it.
type SessionStore interface {
	Issue(ctx context.Context, userID string) (string, error)
	Validate(ctx context.Context, token string) (string, bool)
	Delete(ctx context.Context, token string) error
}

// session is one issued login, keyed in the memory store by its opaque token.
type session struct {
	userID    string
	expiresAt time.Time
}

// MemorySessionStore holds active sessions in a mutex-guarded map. Simple and
// fast, but per-process: it does not survive a restart and each instance has its
// own set, so it only works for a single server. RedisSessionStore replaces it
// once you run more than one instance behind a load balancer.
type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

// NewMemorySessionStore returns an empty in-memory store.
func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[string]session)}
}

// Issue creates a session for userID and returns its opaque token. A fresh token
// is generated on every login, so there is no pre-login session to fixate.
func (s *MemorySessionStore) Issue(_ context.Context, userID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.sessions[token] = session{userID: userID, expiresAt: time.Now().Add(sessionTTL)}
	s.mu.Unlock()
	return token, nil
}

// Validate returns the userID for a token if the session exists and has not
// expired. Expired sessions are deleted lazily on lookup. The token is a
// high-entropy random key, so a plain map lookup does not leak useful timing.
func (s *MemorySessionStore) Validate(_ context.Context, token string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[token]
	if !ok {
		return "", false
	}
	if time.Now().After(sess.expiresAt) {
		delete(s.sessions, token)
		return "", false
	}
	return sess.userID, true
}

// Delete removes a session (logout). It is a no-op for an unknown token and never
// errors; the error return exists to satisfy the interface for the Redis store.
func (s *MemorySessionStore) Delete(_ context.Context, token string) error {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
	return nil
}

// newToken returns a URL-safe, base64-encoded random token.
func newToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
