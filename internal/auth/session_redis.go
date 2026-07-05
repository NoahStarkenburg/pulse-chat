package auth

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// RedisSessionStore keeps sessions in Redis so every server instance validates
// against the same shared store. Unlike the in-memory store, sessions survive a
// restart and work behind a load balancer, and expiry is Redis's own key TTL.
// It owns its own client, symmetric to the bus and cache.
type RedisSessionStore struct {
	client *redis.Client
}

// NewRedisSessionStore connects to Redis from a redis:// URL and verifies it with
// a PING, so a bad URL or unreachable Redis fails fast at startup. The caller
// owns the returned store and must Close it on shutdown.
func NewRedisSessionStore(ctx context.Context, url string) (*RedisSessionStore, error) {
	opt, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parsing redis url: %w", err)
	}
	client := redis.NewClient(opt)
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("pinging redis: %w", err)
	}
	return &RedisSessionStore{client: client}, nil
}

// Close releases the Redis connection pool.
func (s *RedisSessionStore) Close() error { return s.client.Close() }

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
