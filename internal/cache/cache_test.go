package cache

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// newTestCache connects to the Redis named by PULSE_REDIS_URL, skipping when
// none is configured so the suite stays green in CI (which has no Redis) and
// runs locally when the container is up. Tests use unique keys and clean up
// after themselves so they can share one Redis without colliding.
func newTestCache(t *testing.T) *Cache {
	t.Helper()
	url := os.Getenv("PULSE_REDIS_URL")
	if url == "" {
		t.Skip("PULSE_REDIS_URL not set; skipping Redis integration test")
	}
	c, err := New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestCache_RateLimit(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	user := fmt.Sprintf("rl-%d", time.Now().UnixNano())
	t.Cleanup(func() { c.client.Del(ctx, rateKey(user)) })

	for i := 1; i <= rateLimitMax; i++ {
		allowed, err := c.Allow(ctx, user)
		if err != nil {
			t.Fatalf("Allow #%d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("message #%d was rejected; the first %d in a window must be allowed", i, rateLimitMax)
		}
	}
	allowed, err := c.Allow(ctx, user)
	if err != nil {
		t.Fatalf("Allow over-limit: %v", err)
	}
	if allowed {
		t.Fatalf("message #%d was allowed; it exceeds the limit of %d", rateLimitMax+1, rateLimitMax)
	}
}

func TestCache_LoginRateLimit(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	ip := fmt.Sprintf("ip-%d", time.Now().UnixNano())
	t.Cleanup(func() { c.client.Del(ctx, loginKey(ip)) })

	for i := 1; i <= loginRateMax; i++ {
		allowed, err := c.AllowLogin(ctx, ip)
		if err != nil {
			t.Fatalf("AllowLogin #%d: %v", i, err)
		}
		if !allowed {
			t.Fatalf("attempt #%d rejected; the first %d must be allowed", i, loginRateMax)
		}
	}
	allowed, err := c.AllowLogin(ctx, ip)
	if err != nil {
		t.Fatalf("AllowLogin over-limit: %v", err)
	}
	if allowed {
		t.Fatalf("attempt #%d allowed; it exceeds the limit of %d", loginRateMax+1, loginRateMax)
	}
}

func TestCache_Presence(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	room := fmt.Sprintf("presence-%d", time.Now().UnixNano())
	t.Cleanup(func() { c.client.Del(ctx, presenceKey(room)) })

	if err := c.MarkPresent(ctx, room, "alice"); err != nil {
		t.Fatalf("MarkPresent: %v", err)
	}
	// A user last seen before the window (a ghost) must not count as online and
	// must be reclaimable by the sweep.
	stale := float64(time.Now().Add(-2 * presenceWindow).Unix())
	if err := c.client.ZAdd(ctx, presenceKey(room), redis.Z{Score: stale, Member: "ghost"}).Err(); err != nil {
		t.Fatalf("seed ghost: %v", err)
	}

	online, err := c.Online(ctx, room)
	if err != nil {
		t.Fatalf("Online: %v", err)
	}
	if !contains(online, "alice") {
		t.Errorf("online = %v, want it to contain alice", online)
	}
	if contains(online, "ghost") {
		t.Errorf("online = %v, want ghost excluded (outside the window)", online)
	}

	if err := c.SweepStalePresence(ctx); err != nil {
		t.Fatalf("SweepStalePresence: %v", err)
	}
	members, err := c.client.ZRange(ctx, presenceKey(room), 0, -1).Result()
	if err != nil {
		t.Fatalf("ZRange after sweep: %v", err)
	}
	if contains(members, "ghost") {
		t.Errorf("ghost still present after sweep: %v", members)
	}
	if !contains(members, "alice") {
		t.Errorf("alice wrongly swept: %v", members)
	}
}

func TestCache_RecentCache(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()
	room := fmt.Sprintf("recent-%d", time.Now().UnixNano())
	t.Cleanup(func() { c.client.Del(ctx, recentKey(room)) })

	// Miss on a cold room.
	got, err := c.Recent(ctx, room, 10)
	if err != nil {
		t.Fatalf("Recent (cold): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("cold room returned %d payloads, want 0", len(got))
	}

	// Push three in send order; Recent returns them oldest-first.
	for _, s := range []string{"one", "two", "three"} {
		if err := c.PushRecent(ctx, room, []byte(s)); err != nil {
			t.Fatalf("PushRecent %q: %v", s, err)
		}
	}
	got, err = c.Recent(ctx, room, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if want := []string{"one", "two", "three"}; !equalStrings(toStrings(got), want) {
		t.Errorf("Recent = %v, want %v (oldest-first)", toStrings(got), want)
	}

	// Bounded: pushing past the limit keeps only the newest recentLimit.
	for i := 0; i < recentLimit+5; i++ {
		if err := c.PushRecent(ctx, room, []byte(fmt.Sprintf("m%d", i))); err != nil {
			t.Fatalf("PushRecent overflow: %v", err)
		}
	}
	got, err = c.Recent(ctx, room, recentLimit+100)
	if err != nil {
		t.Fatalf("Recent after overflow: %v", err)
	}
	if len(got) != recentLimit {
		t.Errorf("cached %d after overflow, want capped at %d", len(got), recentLimit)
	}

	// Delete simulates eviction; the next read misses and a write repopulates.
	if err := c.client.Del(ctx, recentKey(room)).Err(); err != nil {
		t.Fatalf("del: %v", err)
	}
	if got, _ = c.Recent(ctx, room, 10); len(got) != 0 {
		t.Fatalf("after delete, Recent returned %d, want 0", len(got))
	}
	if err := c.PushRecent(ctx, room, []byte("again")); err != nil {
		t.Fatalf("PushRecent repopulate: %v", err)
	}
	if got, _ = c.Recent(ctx, room, 10); len(got) != 1 || string(got[0]) != "again" {
		t.Fatalf("repopulate = %v, want [again]", toStrings(got))
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

func toStrings(b [][]byte) []string {
	out := make([]string, len(b))
	for i, x := range b {
		out[i] = string(x)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
