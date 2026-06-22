package auth

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
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

// UserStore holds users in memory, indexed by both ID and username. Phase 2
// replaces this with a users table.
type UserStore struct {
	mu     sync.Mutex
	byID   map[string]*User
	byName map[string]*User
}

// NewUserStore returns an empty store.
func NewUserStore() *UserStore {
	return &UserStore{
		byID:   make(map[string]*User),
		byName: make(map[string]*User),
	}
}

// Create adds a user with the given username and password hash. It returns
// ErrUserExists if the username is taken.
func (s *UserStore) Create(username, passwordHash string) (*User, error) {
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
func (s *UserStore) ByUsername(username string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byName[username]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}

// ByID looks up a user by ID, returning ErrUserNotFound if absent.
func (s *UserStore) ByID(id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, ErrUserNotFound
	}
	return u, nil
}
