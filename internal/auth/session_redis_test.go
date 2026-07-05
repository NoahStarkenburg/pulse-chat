package auth

import (
	"context"
	"os"
	"testing"

	"github.com/redis/go-redis/v9"
)

// TestRedisSessionStore_Integration runs against a real Redis named by
// PULSE_REDIS_URL, skipping when none is configured so CI (which has no Redis)
// stays green and it runs locally when the container is up.
func TestRedisSessionStore_Integration(t *testing.T) {
	url := os.Getenv("PULSE_REDIS_URL")
	if url == "" {
		t.Skip("PULSE_REDIS_URL not set; skipping Redis integration test")
	}
	ctx := context.Background()

	opt, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	client := redis.NewClient(opt)
	defer client.Close()
	s := NewRedisSessionStore(client)

	token, err := s.Issue(ctx, "user-1")
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if token == "" {
		t.Fatal("issued an empty token")
	}

	got, ok := s.Validate(ctx, token)
	if !ok || got != "user-1" {
		t.Fatalf("validate = (%q, %v), want (user-1, true)", got, ok)
	}

	if _, ok := s.Validate(ctx, "never-issued"); ok {
		t.Error("validated an unknown token")
	}

	if err := s.Delete(ctx, token); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok := s.Validate(ctx, token); ok {
		t.Error("token still valid after delete")
	}
}
