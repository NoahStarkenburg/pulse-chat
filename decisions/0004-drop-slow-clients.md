# 0004. Drop slow clients instead of blocking fan-out

- **Status:** Accepted
- **Date:** 2026-06-17
- **Deciders:** Noah

## Context

Fan-out runs inside the Hub's single Run goroutine (ADR 0003), delivering each
message to every client in a room by sending on each client's outbound channel.
Clients consume at different speeds: one on a slow network, or with a frozen
tab, drains its channel slowly. The Hub needs a policy for what happens when a
client cannot keep up.

## Decision

Each client has a bounded outbound channel (64 messages). Fan-out sends to it
without blocking: if the buffer is full, the client is dropped (its channel is
closed and it is removed from the room) rather than the Hub waiting on it. The
dropped client's browser can reconnect.

## Alternatives considered

### Block until the slow client catches up
- **Pros:** No client ever misses a message.
- **Cons:** Because fan-out is in the single Run goroutine, blocking on one
  client stalls delivery to every other client in the room and every other
  room. One bad connection freezes the whole server (head-of-line blocking).
- **Why not:** One slow client must not be able to degrade everyone.

### Unbounded per-client buffer
- **Pros:** Never blocks and never drops.
- **Cons:** A client that never drains grows its buffer without limit, so a
  slow or malicious client becomes a memory leak.
- **Why not:** Trades a liveness problem for an unbounded-memory problem.

## Consequences

**Easier:**
- A single slow client cannot stall the room or the server.
- Per-client memory is bounded.

**Harder:**
- A dropped client loses messages sent during the drop and must reconnect.
  Phase 2 (durable history) lets a reconnecting client backfill what it missed,
  so the drop becomes recoverable rather than permanent loss.

**Risk accepted:**
- The buffer size (64) is a guess until measured under load in Phase 6.

## Notes

Buffer sizes: 64 per client, 256 for the shared broadcast channel. Both are
starting points to tune against real traffic.
