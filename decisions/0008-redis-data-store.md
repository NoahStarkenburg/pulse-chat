# 0008. Use Redis as a data store for presence, rate limiting, and a recent-message cache

- **Status:** Accepted
- **Date:** 2026-07-04
- **Deciders:** Noah

## Context

Phase 3 used Redis as a dumb pipe: publish a message, forget it. Phase 4 uses
Redis to hold state. Three small features each need fast, shared, ephemeral
data that Postgres is the wrong tool for: who is online in a room right now, how
fast each user is sending, and a hot copy of the last 50 messages per room.

The organizing principle: none of this Redis state is a source of truth.
Postgres stays authoritative. Everything here is derived, bounded, and safe to
lose. If Redis restarts these features degrade but no durable data is lost.

## Decision

Add an `internal/cache` package holding one `Cache` type with methods for the
three features. It owns its own Redis client (symmetric to `bus.RedisPubSub`) so
the two packages stay independent. The chat layer depends on a small `Cache`
interface, not the implementation.

- **Presence** is a sorted set per room, `presence:<room>`: members are users,
  scores are the unix time they were last seen. A connection marks itself present
  on join and on its keepalive tick; "online" is a `ZRANGEBYSCORE` over the last
  60 seconds. Presence is keyed by user, not connection, so a multi-tab user is
  one entry. Nobody is removed on disconnect; they age out of the window. A
  periodic `ZREMRANGEBYSCORE` sweep reclaims memory.
- **Rate limiting** is a fixed-window counter, `ratelimit:<user>`, 10 messages
  per 5 seconds. The increment and the first-hit expiry run as one atomic Lua
  script, so `INCR` then `EXPIRE` cannot be split by a crash (which would leave a
  counter with no TTL and block the user forever). On a limiter error the chat
  path fails open (allows the message).
- **Recent-message cache** is a bounded list, `room:<room>:recent`, of the last
  50 message payloads. Reads are cache-aside (try Redis, fall back to Postgres on
  a miss); the write path pushes each new message onto the list (`LPUSH` +
  `LTRIM`) in the same code path as the Postgres insert. A one-hour TTL is a
  backstop.

Tunables (windows, limits, sizes) are constants, not configuration, because they
are product decisions rather than per-deployment knobs.

## Alternatives considered

### Presence as a key-per-user with TTL, scanned with SCAN
- **Pros:** Each user's entry expires on its own; no sweep needed.
- **Cons:** One key per online user explodes the keyspace, and answering "who is
  online" means a `SCAN`, which is O(N) over the whole database.
- **Why not:** A sorted set is one key per room with per-member timestamps, an
  O(log N) online query, and O(log N + M) bulk eviction. It is the right shape.

### Sliding-window or token-bucket rate limiting
- **Pros:** No boundary burst; a fixed window lets a user send the limit at the
  end of one window and again at the start of the next (2x for an instant).
- **Cons:** More state and more moving parts (a token count plus a refill
  timestamp, or a sorted set of request times).
- **Why not:** The fixed-window counter teaches the atomic-counter pattern
  cleanly and is enough for chat. The boundary burst is a documented, acceptable
  cost here; upgrade later if abuse warrants it.

### Fail closed when the limiter is unavailable
- **Pros:** Never lets an unlimited flood through.
- **Cons:** A Redis outage would block all chat, turning a protective feature
  into a total outage.
- **Why not:** Rate limiting is protection, not a correctness gate. Failing open
  degrades gracefully. (A login-attempt limiter might choose the opposite.)

### Repopulate the recent cache on a read miss
- **Pros:** A cold room warms on the first read instead of the first write.
- **Cons:** Rebuilding a newest-first list from oldest-first Postgres rows while
  concurrent writes prepend to the same key invites ordering races.
- **Why not:** Populating only from the write path keeps the cache correct with
  no races; a cold room simply falls back to Postgres until the next message.

### Share one Redis client between the bus and the cache
- **Pros:** One connection pool.
- **Cons:** Requires changing the Phase 3 bus constructor.
- **Why not:** Kept separate to leave Phase 3 untouched. Two small pools to the
  same Redis is fine; consolidating later is easy.

### A distributed lock so only one instance runs the presence sweep
- **Pros:** The sweep runs once per interval instead of once per instance.
- **Cons:** Real complexity (lock acquire, TTL, renewal, fencing) for no benefit.
- **Why not:** The sweep is idempotent, so every instance running it is harmless.
  Distributed locks are deferred to Phase 5, where worker jobs create a genuine
  single-owner need. See `learning-notes/concepts/redis-caching-and-locks.md`.

## Consequences

**Easier:**
- A room can show who is online, flooding clients are throttled, and history
  reads are served from memory instead of hitting Postgres every join.

**Harder:**
- Redis now stores data, so its memory is an operational concern:
  `maxmemory-policy` matters where it did not before.
- The recent cache must be updated in the same code path as every DB write, or
  the two drift.
- Two invariants to hold: the presence window must exceed the refresh interval,
  and the rate limiter must fail open.

**Risk accepted:**
- Presence is eventually consistent: a user who closes every tab can linger as
  online for up to the window.
- The fixed window allows a brief 2x burst at a window boundary.
- During a Redis outage rate limiting does not apply (fail open) and presence is
  blank; both recover when Redis returns.

## Notes

The cache methods and the presence, rate-limit, and cache-fallback behaviors are
covered by tests in `internal/cache` (against real Redis, skipped when none is
configured) and `internal/chat` (with a fake cache). Study background for the
caching strategies and the deferred distributed-lock work lives in
`learning-notes/concepts/redis-caching-and-locks.md`.
