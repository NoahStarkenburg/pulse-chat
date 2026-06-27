package auth

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

// MemoryUserStore keeps users in memory, indexed by both ID and username. It is
// used in tests and for any deployment without a database.
type MemoryUserStore struct {
	mu     sync.Mutex
	byID   map[string]*User
	byName map[string]*User
}

// NewMemoryUserStore returns an empty in-memory store.
func NewMemoryUserStore() *MemoryUserStore {
	return &MemoryUserStore{
		byID:   make(map[string]*User),
		byName: make(map[string]*User),
	}
}

// Create adds a user. The context is unused (no I/O) but is part of the
// interface so the database store can honor cancellation.
func (s *MemoryUserStore) Create(_ context.Context, username, passwordHash string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byName[username]; ok {
		return nil, ErrUserExists
	}
	u := &User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    time.Now().UTC(),
	}
	s.byID[u.ID] = u
	s.byName[username] = u
	return u, nil
}

// ByUsername looks up a user by username, returning ErrUserNotFound if absent.
func (s *MemoryUserStore) ByUsername(_ context.Context, username string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byName[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// ByID looks up a user by ID, returning ErrUserNotFound if absent.
func (s *MemoryUserStore) ByID(_ context.Context, id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}
