# 0003. Own the Hub's room state in a single goroutine, not a mutex

- **Status:** Accepted
- **Date:** 2026-06-17
- **Deciders:** Noah

## Context

The server keeps a map of which clients are in which room. Every connection
runs in its own goroutine, so many goroutines need to read and change that map
at once: connects add a client, disconnects remove one, and every inbound
message fans out to a room's members. A Go map is not safe for concurrent
writes; two goroutines writing it at the same time crashes the process.

## Decision

One goroutine (the Hub's Run loop) owns the rooms map and is the only code that
touches it. Everything else communicates with it over channels: register,
unregister, and broadcast. Run reads those channels in a select and applies one
change at a time. The public Register, Unregister, and Broadcast methods only
send on the channels; they never touch the map.

## Alternatives considered

### Guard the map with a sync.Mutex
- **Pros:** Familiar, and fewer moving parts than channels.
- **Cons:** Every accessor has to remember to lock. It is easy to hold the lock
  across a slow network write and stall everyone, and lock ordering has to be
  reasoned about as the code grows.
- **Why not:** Correct but fragile. The race is guarded against rather than
  made impossible.

### Use sync.Map
- **Pros:** No explicit locking.
- **Cons:** Built for flat key-value access, not the read-modify-write over a
  nested map of sets that fan-out needs. The set-level logic would still race.
- **Why not:** Wrong shape for the access pattern.

## Consequences

**Easier:**
- The data race is impossible by construction; only one goroutine touches the
  map, so there is no lock to forget.
- The Hub is simple to test in pure-channel terms, with no network or mocks.

**Harder:**
- All state changes funnel through one goroutine, so any blocking work inside
  Run would back everyone up. Run therefore does no blocking work; slow clients
  are handled by dropping them (ADR 0004).

**Risk accepted:**
- A single owner is a throughput ceiling per process. Phase 3 adds Redis
  pub/sub to scale across processes. This decision is about correctness within
  one process, not cross-process scale.

## Notes

This is the standard Go "share memory by communicating" pattern.
