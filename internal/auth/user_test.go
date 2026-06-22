package auth

import (
	"errors"
	"testing"
)

func TestUser_CreateAndLookup(t *testing.T) {
	s := NewUserStore()
	u, err := s.Create("alice", "hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.ID == "" {
		t.Error("created user has no ID")
	}

	byName, err := s.ByUsername("alice")
	if err != nil || byName.ID != u.ID {
		t.Errorf("ByUsername = (%v, %v), want the created user", byName, err)
	}
	byID, err := s.ByID(u.ID)
	if err != nil || byID.Username != "alice" {
		t.Errorf("ByID = (%v, %v), want the created user", byID, err)
	}
}

func TestUser_DuplicateUsernameRejected(t *testing.T) {
	s := NewUserStore()
	if _, err := s.Create("alice", "hash"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := s.Create("alice", "other")
	if !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate create err = %v, want ErrUserExists", err)
	}
}

func TestUser_NotFound(t *testing.T) {
	s := NewUserStore()
	if _, err := s.ByUsername("ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("ByUsername err = %v, want ErrUserNotFound", err)
	}
	if _, err := s.ByID("missing"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("ByID err = %v, want ErrUserNotFound", err)
	}
}
