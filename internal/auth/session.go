package auth

import (
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

// session is one issued login, keyed in the store by its opaque token.
type session struct {
	userID    string
	expiresAt time.Time
}

// SessionStore holds active sessions in memory. A mutex-guarded map is the right
// tool here (unlike the chat Hub's channel design): access is low-frequency
// CRUD with no fan-out, so a lock is simpler and clearer than a goroutine owner.
// Phase 2 replaces this with a sessions table.
type SessionStore struct {
	mu       sync.Mutex
	sessions map[string]session
}

// NewSessionStore returns an empty store.
func NewSessionStore() *SessionStore {
	return &SessionStore{sessions: make(map[string]session)}
}

// Issue creates a session for userID and returns its opaque token. A fresh
// token is generated on every login, so there is no pre-login session to fixate.
func (s *SessionStore) Issue(userID string) (string, error) {
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
func (s *SessionStore) Validate(token string) (string, bool) {
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

// Delete removes a session (logout). It is a no-op for an unknown token.
func (s *SessionStore) Delete(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// newToken returns a URL-safe, base64-encoded random token.
func newToken() (string, error) {
	b := make([]byte, sessionTokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
