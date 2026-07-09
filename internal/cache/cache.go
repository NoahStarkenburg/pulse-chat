// Package cache uses Redis as a data store rather than just a message bus: room
// presence, per-user rate limiting, and a hot cache of recent messages.
//
// Nothing here is a source of truth. Postgres stays authoritative; every value
// in this package is derived, bounded, and safe to lose. If Redis restarts these
// features degrade (presence empties, rate limiting stops, history reads fall
// back to Postgres) but no durable data is lost.
package cache

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Tunables. These are constants rather than configuration because they are
// product decisions, not per-deployment knobs; promote one to config only when a
// real deployment needs to vary it.
const (
	// presenceWindow is how recently a user must have been seen to count as
	// online. It MUST exceed the connection's presence-refresh interval, or a
	// still-connected user flickers offline between refreshes.
	presenceWindow = 60 * time.Second
	// presenceTTL expires a whole room's presence set once nobody refreshes it,
	// so an abandoned room does not leak its key. Comfortably larger than the
	// window.
	presenceTTL = 5 * time.Minute

	// rateLimitMax is the most chat messages one user may send per window.
	rateLimitMax = 10
	// rateLimitWindow is the fixed window the limit applies over.
	rateLimitWindow = 5 * time.Second

	// loginRateMax and loginRateWindow bound authentication attempts (login and
	// signup) per client IP, to slow brute-force guessing and signup spam before
	// any user is known.
	loginRateMax    = 10
	loginRateWindow = time.Minute

	// recentLimit is how many recent messages the cache keeps per room. It matches
	// the history limit the chat layer requests so a cache hit fully serves a join.
	recentLimit = 50
	// recentTTL bounds how long a room's cached messages live without a write. It
	// is only a backstop: the cache is authoritative for nothing.
	recentTTL = time.Hour
)

// Cache wraps a shared Redis client used for derived state (presence, rate
// limiting, recent messages). The client is owned by the caller (main) and
// shared with the bus and session store; Cache only borrows it.
type Cache struct {
	client *redis.Client
}

// New wraps a shared Redis client for derived state. The caller owns the client
// (creates and closes it); Cache only borrows it.
func New(client *redis.Client) *Cache {
	return &Cache{client: client}
}

// --- Presence ---------------------------------------------------------------
//
// A sorted set per room: members are users, scores are the unix time they were
// last seen. One key per room gives per-member timestamps, an O(log N) "who is
// online" range query, and O(log N + M) bulk eviction of the stale, none of
// which a plain set or a key-per-user could do cheaply.

func presenceKey(room string) string { return "presence:" + room }

// MarkPresent records that user was seen in room just now. Call on join and
// periodically while the connection is open. Presence is keyed by user, not by
// connection: a user with two tabs is one entry, and closing one tab does not
// mark them away while the other still refreshes.
func (c *Cache) MarkPresent(ctx context.Context, room, user string) error {
	key := presenceKey(room)
	now := float64(time.Now().Unix())
	pipe := c.client.Pipeline()
	pipe.ZAdd(ctx, key, redis.Z{Score: now, Member: user})
	pipe.Expire(ctx, key, presenceTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// Online returns the users seen in room within the presence window. Stale
// members are ignored here and reclaimed later by SweepStalePresence; the read
// never depends on the sweep having run.
func (c *Cache) Online(ctx context.Context, room string) ([]string, error) {
	since := strconv.FormatInt(time.Now().Add(-presenceWindow).Unix(), 10)
	return c.client.ZRangeByScore(ctx, presenceKey(room), &redis.ZRangeBy{
		Min: since,
		Max: "+inf",
	}).Result()
}

// SweepStalePresence removes users not seen within the window from every room's
// presence set, reclaiming memory. It is idempotent and safe to run from every
// instance at once, so it needs no cross-instance lock.
func (c *Cache) SweepStalePresence(ctx context.Context) error {
	cutoff := "(" + strconv.FormatInt(time.Now().Add(-presenceWindow).Unix(), 10)
	iter := c.client.Scan(ctx, 0, "presence:*", 100).Iterator()
	for iter.Next(ctx) {
		if err := c.client.ZRemRangeByScore(ctx, iter.Val(), "-inf", cutoff).Err(); err != nil {
			return err
		}
	}
	return iter.Err()
}

// --- Rate limiting ----------------------------------------------------------

// allowScript increments a fixed-window counter and, only on the first hit in a
// window, sets the window's expiry, both in one atomic server-side step. Doing
// INCR and EXPIRE as two round trips has a real bug: a crash in between leaves a
// counter with no TTL, blocking the user forever. Returns the new count.
var allowScript = redis.NewScript(`
local n = redis.call("INCR", KEYS[1])
if n == 1 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return n
`)

func rateKey(user string) string { return "ratelimit:" + user }
func loginKey(ip string) string  { return "ratelimit:login:" + ip }

// allow runs the atomic fixed-window increment for key and reports whether the
// post-increment count is still within limit. It is the shared core of the
// per-user message limit and the per-IP login limit.
func (c *Cache) allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	n, err := allowScript.Run(ctx, c.client, []string{key}, window.Milliseconds()).Int()
	if err != nil {
		return false, err
	}
	return n <= limit, nil
}

// Allow reports whether user may send another chat message now, under a fixed
// window of rateLimitMax messages per rateLimitWindow. Because the increment and
// expiry are one atomic script, concurrent sends from the same user cannot both
// slip past the limit: Redis runs the script to completion before the next starts.
func (c *Cache) Allow(ctx context.Context, user string) (bool, error) {
	return c.allow(ctx, rateKey(user), rateLimitMax, rateLimitWindow)
}

// AllowLogin reports whether another authentication attempt from ip is allowed,
// under a per-IP window of loginRateMax attempts per loginRateWindow. It throttles
// login and signup by source address, the classic brute-force and signup-spam
// defense, before any user is known.
func (c *Cache) AllowLogin(ctx context.Context, ip string) (bool, error) {
	return c.allow(ctx, loginKey(ip), loginRateMax, loginRateWindow)
}

// --- Recent-message cache ---------------------------------------------------

func recentKey(room string) string { return "room:" + room + ":recent" }

// PushRecent prepends one message payload to room's recent list, trims it back
// to recentLimit, and refreshes the key TTL, all in one pipeline. The payload is
// opaque bytes (the chat layer's message JSON); the cache neither parses nor
// understands it.
func (c *Cache) PushRecent(ctx context.Context, room string, payload []byte) error {
	key := recentKey(room)
	pipe := c.client.Pipeline()
	pipe.LPush(ctx, key, payload)
	pipe.LTrim(ctx, key, 0, recentLimit-1)
	pipe.Expire(ctx, key, recentTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// Recent returns up to limit cached payloads for room, oldest-first to match the
// Postgres history path so a caller cannot tell which served it. An empty result
// means a miss (cold or evicted room); the caller falls back to Postgres.
func (c *Cache) Recent(ctx context.Context, room string, limit int) ([][]byte, error) {
	if limit > recentLimit {
		limit = recentLimit
	}
	vals, err := c.client.LRange(ctx, recentKey(room), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	// LPush prepends, so LRange returns newest-first; reverse to oldest-first.
	out := make([][]byte, len(vals))
	for i, v := range vals {
		out[len(vals)-1-i] = []byte(v)
	}
	return out, nil
}
