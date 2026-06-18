# 0001. Use Go as the project's implementation language

- **Status:** Accepted
- **Date:** 2026-05-18
- **Deciders:** Noah

## Context

Pulse Chat is a learning project intended to teach the fundamentals of
real-time distributed systems: WebSocket protocol, Redis Pub/Sub for
horizontal scaling, durable work queues (RabbitMQ), worker patterns,
observability, and (later) gRPC service decomposition. The success
criterion is depth of understanding - being able to explain how each
component works at the protocol level, not just how to call a library.

A secondary goal is to produce a credible portfolio artifact for
SWE / backend / AI-engineer interviews.

The realistic candidate languages were: Go, .NET (C#), Node.js (TypeScript),
Rust, and Python.

## Decision

Implement the project in Go.

## Alternatives considered

### .NET with MassTransit + SignalR
- **Pros:** Aligns with Noah's day-job (FamilyCore). MassTransit ships
  production-grade messaging patterns out of the box (outbox, sagas,
  retries). SignalR is a polished WebSocket abstraction. Faster shipping.
- **Cons:** The frameworks abstract away exactly the protocol-level
  details the project is meant to teach. AMQP concepts (exchanges,
  bindings, prefetch) are hidden by MassTransit. WebSocket frames are
  invisible behind SignalR's hub abstraction. The learning is "how to
  configure the framework," not "how the protocol works."
- **Why not:** Conflicts with the primary goal - depth of protocol
  understanding.

### Rust with tokio
- **Pros:** Most rigorous of any candidate. Forces the cleanest
  ownership model and exposes asynchronicity directly. Maximum
  technical signal in a portfolio piece.
- **Cons:** The borrow checker dominates the learning curve. Estimated
  70% of time would go to fighting the language, 30% to infrastructure
  patterns. Wrong cost-benefit for the stated goal.
- **Why not:** Time budget eaten by language friction.

### Node.js / TypeScript
- **Pros:** WebSocket libraries (`ws`) are minimal and direct.
- **Cons:** Single-threaded event loop makes worker patterns awkward
  (must spawn child processes for parallelism - a tangential lesson).
  npm ecosystem instability adds noise. No strong career alignment
  for Noah's target roles.
- **Why not:** Worker model is awkward; misalignment with target stack.

### Python with asyncio
- **Pros:** Familiar syntax. Strong AI ecosystem.
- **Cons:** Async story is bolted-on rather than native. Workers are
  hacky (Celery, RQ feel old). WebSocket and queue libraries are less
  polished than Go's.
- **Why not:** Same async-bolt-on tax as Node, with worse keyword
  alignment for the target roles.

## Consequences

**Easier:**
- Minimum distance between the code and the wire protocol. AMQP, JSON
  WebSocket frames, raw Redis commands are all visible in the code.
- Goroutines + channels map cleanly to the Hub pattern, pump pattern,
  and worker pool.
- Single static binary - trivial deployment story.
- Strong tooling: `go test`, `go vet`, gofmt, `golangci-lint`,
  testcontainers-go.
- Alignment with Conduit (Noah's flagship portfolio project) - skills
  compound across projects.

**Harder:**
- More boilerplate than the .NET equivalent. Error handling is verbose.
- No batteries-included messaging framework - outbox, retries,
  saga-style coordination must be hand-built (which is the point, but
  costs time).

**Risk accepted:**
- Slower initial velocity than .NET. Mitigated by the fact that the
  learning is the primary goal, not the velocity.

## Notes

Polyglot (Go for the gateway + .NET for workers) was explicitly
considered and rejected - interop friction (proto/JSON contracts in two
languages, two toolchains, two debugging environments) adds learning
overhead in a dimension unrelated to the project's purpose. See the
conversation that produced this ADR for the full rationale.
