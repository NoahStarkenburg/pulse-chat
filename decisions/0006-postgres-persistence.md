# 0006. Persist users, rooms, and messages in Postgres with pgx

- **Status:** Accepted
- **Date:** 2026-06-21
- **Deciders:** Noah

## Context

Phase 1.5 kept users, sessions, and messages in memory, so a restart erased
every account and every message. Phase 2 makes the durable data survive a
restart by moving it to Postgres. Three choices fall out of that: how to talk to
Postgres, how to manage the schema, and how much to move at once.

## Decision

Use the `pgx/v5` driver and its native pool (`pgxpool`) directly, with no ORM
and no `database/sql` layer. Manage the schema with versioned SQL migrations
applied by a small hand-written runner embedded in the binary. Move users,
rooms, and messages to Postgres behind a `UserStore` / `MessageStore` interface,
but leave sessions in memory until the Redis phase.

Every database call is parameterized and bounded by a query timeout, and the
room-upsert plus message-insert run in one transaction so a failure leaves
neither behind.

## Alternatives considered

### An ORM (GORM / ent) instead of hand-written SQL
- **Pros:** Less boilerplate; relationships and migrations generated for you.
- **Cons:** Hides the SQL, which is exactly what this phase is meant to teach;
  easy to emit accidental N+1 queries; another large dependency.
- **Why not:** The goal is to see and own the SQL. The repository is small
  enough that explicit queries are clearer than a mapping layer.

### A migration tool (goose / golang-migrate) instead of a custom runner
- **Pros:** Battle-tested; handles edge cases; standard in the ecosystem.
- **Cons:** goose required Go 1.25 and dragged pgx to v5.9.2, which pushed the
  `go` directive past what the CI linter (built with go1.24) accepts, turning
  the pipeline red on a version conflict unrelated to our code.
- **Why not:** A ~190-line runner using pgx directly has zero extra
  dependencies, so it can never move the Go version floor again, and the
  mechanics (a `schema_migrations` ledger, each migration in a transaction) are
  worth understanding directly.

### Move sessions to Postgres too
- **Pros:** All state durable in one place.
- **Cons:** Sessions are cheap to recreate (re-login) and are a better fit for
  Redis, which arrives in Phase 4. A sessions table would be throwaway work.
- **Why not:** Durable, must-not-be-lost data goes to Postgres now; cheap,
  recreatable data waits for the right store.

## Consequences

**Easier:**
- Messages and accounts survive restarts; a joining client replays the last 50
  messages from an indexed query.
- The store interface lets tests run against an in-memory implementation and
  production run against Postgres, swapped by one line in main.

**Harder:**
- We own the migration runner and the SQL, including their correctness.
- The server now has a hard dependency on Postgres and fails fast without it.

**Risk accepted:**
- The custom runner is intentionally minimal (no down-migration safety rails
  beyond one-step rollback). Production discipline is roll-forward.

## Notes

pgx pinned to v5.7.6 to keep the `go` directive at 1.23.0 (see the CI note in the
project memory). Index `(room_id, created_at DESC)` backs the recent-messages
read. Related: ADR 0005 (sessions stay in-memory, promoted in a later phase).
