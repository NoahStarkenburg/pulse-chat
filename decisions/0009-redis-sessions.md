# 0009. Store login sessions in Redis

- **Status:** Accepted
- **Date:** 2026-07-04
- **Deciders:** Noah

## Context

Sessions were held in an in-process map (`MemorySessionStore`). That works for one
server and breaks the moment there are two. A user logs in on instance A, the
token lands in A's memory, the load balancer sends the next request to instance
B, and B has never heard of that token, so the user looks unauthenticated. A
restart wipes every session on that instance.

This is a real inconsistency: from Phase 3 the app fans messages across instances,
but its auth was still per-instance. Even with WebSocket stickiness (a live
socket is pinned to one instance anyway), the login request and the upgrade
handshake can land on different instances, so the upgrade fails to authenticate.

## Decision

Add a `SessionStore` interface (`Issue`, `Validate`, `Delete`) with two
implementations behind it, mirroring the existing `UserStore` pattern:
`MemorySessionStore` (unchanged behavior) and a new `RedisSessionStore`. The
backend is chosen by one line in main. Production uses Redis.

The Redis store keys each session `session:<token> -> userID` with the session
TTL as the key expiry, so Redis reaps expired sessions on its own with no sweep.
Validation is a single `GET`. A Redis error during validation fails closed
(returns not-authenticated): admitting a request we cannot verify is worse than
denying it during an outage. The store owns its own Redis client, symmetric to
the bus and cache, and fails fast at startup if Redis is unreachable.

## Alternatives considered

### Sticky sessions at the load balancer
- **Pros:** No session store change; pin a user to one instance.
- **Cons:** Fragile (a rebalance or an instance restart logs users out), and it
  couples correctness to load-balancer configuration.
- **Why not:** Shared session state is the robust fix. Stickiness is still used
  for the WebSocket connection itself, but auth should not depend on it.

### Stateless JWTs (no server-side session store)
- **Pros:** No store at all; any instance validates a signed token locally.
- **Cons:** No straightforward server-side revocation. A logout or a ban cannot
  invalidate an already-issued token until it expires, without adding a denylist,
  which reintroduces shared state anyway.
- **Why not:** Instant revocation matters for a chat app, and the denylist would
  put us back at "shared store," so a shared store is the simpler honest answer.

### Postgres-backed sessions
- **Pros:** Durable, and we already run Postgres.
- **Cons:** A row read on every authenticated request, plus expiry sweeping we
  would have to run ourselves.
- **Why not:** Sessions are ephemeral key-value with a natural TTL, which is
  exactly Redis's shape. Postgres is for the durable source of truth.

## Consequences

**Easier:**
- Login works behind a load balancer: any instance validates any session.
- Sessions survive restarts and deploys, and expire automatically via the TTL.
- Global, instant revocation: `DEL session:<token>` logs someone out everywhere.

**Harder:**
- Auth now depends on Redis. A Redis outage means nobody can log in or validate a
  session, where in-memory sessions would have survived it. It adds no new single
  point of failure (Redis is already required since Phase 3) but widens the blast
  radius to include auth.
- A Redis round trip per auth check instead of a memory lookup. Sub-millisecond,
  but more load; a small in-process cache could soften it later if needed.

**Risk accepted:**
- Fail-closed validation: during a Redis outage every request is denied (401)
  rather than admitted unverified.

## Notes

There are now three Redis clients in the process (bus, cache, sessions), each
owning its own pool for package independence. Consolidating them into one shared
client is a reasonable future cleanup; kept separate here to avoid changing the
Phase 3 and Phase 4 constructors. The Redis store is covered by an integration
test that skips when `PULSE_REDIS_URL` is unset; the in-memory store keeps its
existing unit tests.
