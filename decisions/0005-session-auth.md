# 0005. Use opaque server-side sessions for authentication

- **Status:** Accepted
- **Date:** 2026-06-17
- **Deciders:** Noah

## Context

Phase 1 identified users by a `?name=` query parameter, which anyone can forge.
Phase 1.5 adds real authentication: signup, login, logout, and a session that
gates the HTTP API and the WebSocket upgrade. Two decisions follow from that:
how identity is represented on each request, and where the supporting state
lives.

## Decision

Use opaque, server-side sessions, not JWTs. On login the server generates a
random 256-bit token, stores it against the user ID with a TTL, and sets it in
an `HttpOnly`, `SameSite=Lax` cookie (`Secure` in production via
`PULSE_COOKIE_SECURE`). Every request, including the WebSocket upgrade, is
authenticated by looking the token up. Logout deletes the stored session.

Passwords are hashed with bcrypt at cost 12. The user and session stores are
in-memory for Phase 1.5; Phase 2 promotes both to Postgres tables.

## Alternatives considered

### JWTs (signed stateless tokens)
- **Pros:** Stateless; any service can verify without a shared store; fits
  multi-service architectures.
- **Cons:** Cannot be revoked without adding a server-side revocation list, at
  which point the statelessness is gone and you have reinvented sessions.
  Larger payload, and easy to misuse (storing them where JavaScript can read
  them exposes them to XSS).
- **Why not:** This is a single service that needs instant logout. Sessions are
  simpler and revoke immediately.

### Pull Postgres into Phase 1.5 for the user table
- **Pros:** The user model would be durable from the start.
- **Cons:** Postgres (driver, pooling, migrations, real-infra tests) is the
  entire subject of Phase 2. Introducing it here muddies both phases and breaks
  the "don't skip ahead" discipline of the project.
- **Why not:** In-memory keeps Phase 1.5 about auth concepts; Phase 2 owns the
  storage migration.

## Consequences

**Easier:**
- Logout and session expiry are trivial (delete a map entry / check a TTL).
- The WebSocket upgrade needs no separate auth scheme; the cookie rides the
  upgrade like any HTTP request.

**Harder:**
- The server is stateful: sessions live in memory and are lost on restart, so
  everyone is logged out when the process restarts. Acceptable for Phase 1.5;
  Phase 2 (Postgres) and later Redis make sessions durable and shared.

**Risk accepted:**
- A single in-memory store does not scale across instances. That is fine until
  Phase 3 introduces multiple instances, by which point sessions live in a
  shared store.

## Notes

bcrypt cost 12; session TTL 24h; 256-bit tokens. Login compares against a dummy
hash when the username is unknown so response timing does not reveal which
usernames exist.
