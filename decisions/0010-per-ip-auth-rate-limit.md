# 0010. Rate-limit authentication attempts by client IP

- **Status:** Accepted
- **Date:** 2026-07-04
- **Deciders:** Noah

## Context

Phase 4 added per-user message rate limiting. That defends the chat feed, but it
cannot defend login and signup, because those happen before a user is known: on a
brute-force password attack, a credential-stuffing run, or signup spam, there is
no authenticated user id to key on. The natural key for pre-auth abuse is the
source IP address.

## Decision

Add a per-IP fixed-window limit on `POST /login` and `POST /signup`: 10 attempts
per minute per client IP, keyed `ratelimit:login:<ip>`, reusing the same atomic
increment-and-expire counter as the message limiter. Over the limit returns `429
Too Many Requests` before any credential work. On a Redis error it fails open
(allows the attempt) and logs, like the message limiter, so a limiter outage does
not lock everyone out of login.

The auth handlers depend on a small `LoginLimiter` interface, so the concrete
limiter (`*cache.Cache`) is injected the same way as every other collaborator.
The client IP is taken from the connection's remote address.

## Alternatives considered

### Per-user instead of per-IP
- **Why not:** There is no user yet at login and signup time. Per-user is the
  right key for authenticated actions (chat messages) and is already in place;
  per-IP covers the pre-auth gap. The two are complementary.

### Account lockout after N failed logins
- **Pros:** Directly stops guessing against one account.
- **Cons:** An attacker can weaponize it to lock a victim out on purpose by
  failing logins against their username (a denial-of-service).
- **Why not:** Per-IP throttling slows guessing without giving an attacker a lever
  to lock out real users.

### CAPTCHA on login/signup
- **Pros:** Strong against automated abuse.
- **Cons:** Heavier, hurts UX, and needs a third-party service.
- **Why not:** Out of scope for now; a per-IP limit is a cheap first line and can
  be layered with a CAPTCHA later if abuse warrants it.

## Consequences

**Easier:**
- Brute-force and signup spam from a single address are slowed to a crawl.

**Harder / risk accepted:**
- Users behind a shared address (office or campus NAT, a mobile carrier) share one
  limit, so a busy shared IP could hit it. The limit is set generously (10/min)
  to make that rare, and it is a constant that is easy to tune.
- Behind a load balancer the server sees the balancer's IP, not the client's, so
  the real client IP must come from a trusted `X-Forwarded-For` header instead
  (the `PULSE_TRUSTED_PROXY_CIDRS` machinery, a Phase 7 concern). Until then the
  direct remote address is correct for local and single-instance runs. This is
  called out in the `clientIP` helper so it is not forgotten.

## Notes

Limits are constants in `internal/cache`, tunable if abuse patterns demand it.
The behavior is covered by a handler test (the third attempt returns 429) and a
cache test (`AllowLogin` rejects past the limit, against real Redis, skipped when
`PULSE_REDIS_URL` is unset).
