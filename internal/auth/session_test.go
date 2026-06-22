package auth

import (
	"testing"
	"time"
)

func TestSession_IssueAndValidate(t *testing.T) {
	s := NewSessionStore()
	token, err := s.Issue("user-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("issued an empty token")
	}
	got, ok := s.Validate(token)
	if !ok || got != "user-1" {
		t.Errorf("validate = (%q, %v), want (\"user-1\", true)", got, ok)
	}
}

func TestSession_IssueIsUnique(t *testing.T) {
	s := NewSessionStore()
	a, _ := s.Issue("user-1")
	b, _ := s.Issue("user-1")
	if a == b {
		t.Error("two issued tokens are identical")
	}
}

func TestSession_UnknownTokenRejected(t *testing.T) {
	s := NewSessionStore()
	if _, ok := s.Validate("nope"); ok {
		t.Error("validated an unknown token")
	}
}

func TestSession_DeleteInvalidates(t *testing.T) {
	s := NewSessionStore()
	token, _ := s.Issue("user-1")
	s.Delete(token)
	if _, ok := s.Validate(token); ok {
		t.Error("token still valid after delete")
	}
}

func TestSession_ExpiredRejected(t *testing.T) {
	// White-box: insert an already-expired session and confirm Validate rejects
	// and reaps it.
	s := NewSessionStore()
	s.sessions["stale"] = session{userID: "user-1", expiresAt: time.Now().Add(-time.Minute)}
	if _, ok := s.Validate("stale"); ok {
		t.Error("expired session validated")
	}
	if _, present := s.sessions["stale"]; present {
		t.Error("expired session was not reaped on validate")
	}
}
