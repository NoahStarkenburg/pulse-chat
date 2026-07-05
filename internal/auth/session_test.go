package auth

import (
	"context"
	"testing"
	"time"
)

func TestSession_IssueAndValidate(t *testing.T) {
	ctx := context.Background()
	s := NewMemorySessionStore()
	token, err := s.Issue(ctx, "user-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("issued an empty token")
	}
	got, ok := s.Validate(ctx, token)
	if !ok || got != "user-1" {
		t.Errorf("validate = (%q, %v), want (\"user-1\", true)", got, ok)
	}
}

func TestSession_IssueIsUnique(t *testing.T) {
	ctx := context.Background()
	s := NewMemorySessionStore()
	a, _ := s.Issue(ctx, "user-1")
	b, _ := s.Issue(ctx, "user-1")
	if a == b {
		t.Error("two issued tokens are identical")
	}
}

func TestSession_UnknownTokenRejected(t *testing.T) {
	s := NewMemorySessionStore()
	if _, ok := s.Validate(context.Background(), "nope"); ok {
		t.Error("validated an unknown token")
	}
}

func TestSession_DeleteInvalidates(t *testing.T) {
	ctx := context.Background()
	s := NewMemorySessionStore()
	token, _ := s.Issue(ctx, "user-1")
	if err := s.Delete(ctx, token); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.Validate(ctx, token); ok {
		t.Error("token still valid after delete")
	}
}

func TestSession_ExpiredRejected(t *testing.T) {
	// White-box: insert an already-expired session and confirm Validate rejects
	// and reaps it.
	s := NewMemorySessionStore()
	s.sessions["stale"] = session{userID: "user-1", expiresAt: time.Now().Add(-time.Minute)}
	if _, ok := s.Validate(context.Background(), "stale"); ok {
		t.Error("expired session validated")
	}
	if _, present := s.sessions["stale"]; present {
		t.Error("expired session was not reaped on validate")
	}
}
