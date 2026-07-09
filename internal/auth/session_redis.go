package auth

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore keeps sessions in Redis so every server instance validates
// against the same shared store. Unlike the in-memory store, sessions survive a
// restart and work behind a load balancer, and expiry is Redis's own key TTL.
// The client is owned by the caller (main) and shared with the bus and cache;
// the store only borrows it.
type RedisSessionStore struct {
	client *redis.Client
}

// NewRedisSessionStore wraps a shared Redis client as a session store. The caller
// owns the client (creates and closes it); the store only borrows it.
func NewRedisSessionStore(client *redis.Client) *RedisSessionStore {
	return &RedisSessionStore{client: client}
}

func sessionKey(token string) string { return "session:" + token }

// Issue creates a session for userID and returns its opaque token. The record is
// stored with the session TTL as the key expiry, so Redis reaps it on its own; no
// sweep is needed.
func (s *RedisSessionStore) Issue(ctx context.Context, userID string) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	if err := s.client.Set(ctx, sessionKey(token), userID, sessionTTL).Err(); err != nil {
		return "", fmt.Errorf("storing session: %w", err)
	}
	return token, nil
}

// Validate returns the userID for a token if the session exists. A missing key
// (unknown or TTL-expired token) returns not-ok, and so does a genuine Redis
// error: both fail closed, because admitting a request we cannot verify is worse
// than denying it during an outage.
func (s *RedisSessionStore) Validate(ctx context.Context, token string) (string, bool) {
	userID, err := s.client.Get(ctx, sessionKey(token)).Result()
	if err != nil {
		return "", false
	}
	return userID, true
}

// Delete removes a session (logout). A missing key is not an error.
func (s *RedisSessionStore) Delete(ctx context.Context, token string) error {
	return s.client.Del(ctx, sessionKey(token)).Err()
}
