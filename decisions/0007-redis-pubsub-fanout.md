# 0007. Fan out chat messages across instances with Redis Pub/Sub

- **Status:** Accepted
- **Date:** 2026-06-26
- **Deciders:** Noah

## Context

Through Phase 2 all messaging was in-process: the Hub fanned a message out to
clients in the same Go process. The moment we run more than one server instance
behind a load balancer, that breaks. A user connected to instance A and a user
connected to instance B land on different Hubs that know nothing about each
other, so a message from A never reaches B. Phase 3 makes horizontal scaling
work: instances must exchange messages.

## Decision

Publish each chat message to a per-room Redis Pub/Sub channel (`room:<name>`).
Every instance subscribes to the channels for rooms that have local clients, and
fans a received message out to its own local clients. The publishing instance
receives its own message back through its subscription (the loopback) and
delivers it that way, so it does **not** also fan out locally. Subscriptions are
reference-counted: the first local client in a room subscribes, the last to
leave unsubscribes.

The chat package depends on a `bus.PubSub` interface, not on Redis directly, with
a Redis implementation in production and an in-memory one for tests. Redis is a
hard dependency from this phase on (fail-fast at startup, pinged in `/readyz`).

## Alternatives considered

### A durable queue (RabbitMQ) instead of Pub/Sub
- **Pros:** Durable; messages survive a broker restart.
- **Cons:** Queues are point-to-point (one message, one consumer). We need
  broadcast-to-all-subscribers, the opposite shape.
- **Why not:** Pub/Sub fits fan-out; a queue does not. Durability is unnecessary
  here because Postgres is the source of truth, so a briefly-disconnected
  subscriber just reloads history. Phase 5 uses RabbitMQ where durability matters
  (moderation jobs).

### Broadcast locally, then publish to other instances (origin-id dedupe)
- **Pros:** The sender's own echo never waits on a Redis round-trip.
- **Cons:** Every message needs an instance-id tag and every subscriber must skip
  its own; more state and a new way to get double-delivery wrong.
- **Why not:** The publish-only loopback is simpler and has one delivery path for
  every message, local or remote. The extra sub-millisecond hop for the sender's
  echo is not worth the complexity.

### Subscribe every instance to all rooms (`room:*`)
- **Pros:** No per-room subscribe/unsubscribe bookkeeping.
- **Cons:** Every instance receives every room's traffic, including rooms it has
  no clients in.
- **Why not:** Per-room subscription keeps each instance's load proportional to
  the rooms it actually serves.

## Consequences

**Easier:**
- Run N instances behind a load balancer and users chat across all of them.
- The Hub shrinks to local delivery; cross-instance routing is the bus's job.

**Harder:**
- Redis is now required to run the server.
- There is a brief window between a room's first client joining and its
  subscription being established where a just-published message could be missed.
  Accepted: Pub/Sub is fire-and-forget and Postgres is the source of truth.

**Risk accepted:**
- Fire-and-forget delivery. A subscriber disconnected from Redis misses messages
  for the gap. Fine for chat (reload recovers); would not be fine for jobs.

## Notes

go-redis pinned to v9.7.3 to keep the `go` directive at 1.23.0. The loopback and
the ref-counted lifecycle are covered by tests in `internal/chat`. Related: this
supersedes the single-instance assumption noted in ADR 0005.
