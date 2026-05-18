# Pulse Chat

A horizontally-scalable, real-time multi-room chat system with AI-powered
moderation. Built in Go from first principles to learn how WebSockets, Redis
Pub/Sub, RabbitMQ, and durable background workers actually work — protocol-by-
protocol, not framework-by-framework.

This is a learning project built in public. The architecture, phasing, and
discipline are deliberate. Code is written with comments that explain *why*,
not *what*.

## Status

| Phase | Topic                                       | Status      |
|-------|---------------------------------------------|-------------|
| 1     | WebSockets, single-server chat              | Not started |
| 2     | Postgres for durable message history        | Not started |
| 3     | Redis Pub/Sub for horizontal scaling        | Not started |
| 4     | Redis as a data store (presence, cache, RL) | Not started |
| 5     | RabbitMQ + a worker (AI moderation)         | Not started |
| 6     | Worker pool, metrics, dashboard             | Not started |

## Architecture (target)

```
   Browser ──WS──┐
   Browser ──WS──┤── API server #1 ──┐
   Browser ──WS──┘                   │
                                     ├── Redis (Pub/Sub + cache + presence)
   Browser ──WS──┐                   │
   Browser ──WS──┤── API server #2 ──┘
   Browser ──WS──┘
                          │
                          ├── Postgres (source of truth)
                          │
                          └── RabbitMQ ── Worker pool (AI moderation,
                                                       notifications,
                                                       indexing)
```

**Hot path** (sending a message):
1. Browser → WebSocket → API server.
2. API server inserts the message into Postgres (source of truth).
3. API server `PUBLISH room:{id}` on Redis. *Every* API instance subscribed to
   that room receives the message and pushes it to its connected clients.
4. API server enqueues a `moderate` job on RabbitMQ.

**Cold path** (background work):
5. A worker pops the `moderate` job, calls the AI API, decides verdict.
6. Worker writes the verdict to Postgres + publishes `room:{id}` on Redis →
   browsers see the message updated in real time.

The worker never speaks to clients directly. Redis Pub/Sub is the bridge
between async background work and real-time UI.

## Tech stack

| Layer        | Choice                                   | Why                                  |
|--------------|------------------------------------------|--------------------------------------|
| Language     | Go 1.23+                                 | Concurrency primitives map cleanly; minimum distance from the wire |
| Web/HTTP     | `net/http` + [`go-chi`][chi]             | Standard library + a lean router. No framework. |
| WebSockets   | [`coder/websocket`][coderws]             | Modern, context-aware, exposes the protocol |
| Database     | Postgres 16 + [`pgx`][pgx]               | No ORM. Learn SQL properly. |
| Cache + PubSub | Redis 7 + [`go-redis`][goredis]         | Single tool, multiple roles |
| Queue        | RabbitMQ 3.13 + [`amqp091-go`][amqp]     | Canonical AMQP broker; teaches the model |
| Logging      | `log/slog`                               | Standard library structured logging |
| Migrations   | [`goose`][goose]                         | Lightweight, SQL-first |
| Tests        | `go test` + [`testcontainers-go`][tc]    | Integration tests against real infra |
| Lint         | `golangci-lint`                          | Standard meta-linter |
| Local dev    | Docker Compose                           | One command up the stack |
| CI           | GitHub Actions                           | Lint, test, build on PR |

[chi]: https://github.com/go-chi/chi
[coderws]: https://github.com/coder/websocket
[pgx]: https://github.com/jackc/pgx
[goredis]: https://github.com/redis/go-redis
[amqp]: https://github.com/rabbitmq/amqp091-go
[goose]: https://github.com/pressly/goose
[tc]: https://github.com/testcontainers/testcontainers-go

## Prerequisites

| Tool             | Version | Install                                                |
|------------------|---------|--------------------------------------------------------|
| Go               | 1.23+   | <https://go.dev/dl/>                                   |
| Docker Desktop   | latest  | <https://www.docker.com/products/docker-desktop/>      |
| `just` (optional)| latest  | `winget install Casey.Just` or `scoop install just`    |
| `golangci-lint`  | latest  | `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest` |

Once Go is on PATH, run `go version` to confirm. The Phase 1 work needs only
Go and a browser — Docker is for Phase 2 onward.

## Quick start

```bash
# 1. Install Go (see Prerequisites)
# 2. Clone and enter the repo
git clone https://github.com/NoahStarkenburg/pulse-chat.git
cd pulse-chat

# 3. Download dependencies (no-op if go.mod is already populated)
go mod download

# 4. Run the server (Phase 1)
go run ./cmd/server

# Server listens on :8080. Visit http://localhost:8080/healthz to confirm.
```

If you have `just` installed:

```bash
just run         # run the server
just lint        # run linter
just test        # run tests
just up          # start Docker infra (Phase 2+)
just down        # stop Docker infra
```

## Project structure

```
pulse-chat/
├── cmd/
│   └── server/             # Main API + WebSocket binary (Phase 1+)
│       └── main.go
├── internal/               # Private packages — not importable by other modules
│   ├── chat/               # Hub, Client, message types (Phase 1)
│   ├── config/             # Env parsing, single Config struct
│   ├── store/              # Postgres repository (Phase 2)
│   ├── cache/              # Redis data wrapper (Phase 4)
│   └── bus/                # Redis Pub/Sub + RabbitMQ (Phases 3, 5)
├── migrations/             # SQL migrations (Phase 2+)
├── deployments/
│   └── docker-compose.yml  # Local infra stack
├── .github/
│   └── workflows/
│       └── ci.yml          # Lint + test + build on every PR
├── .golangci.yml           # Linter config
├── Dockerfile              # Multi-stage build for the server binary
├── justfile                # Common commands
├── go.mod / go.sum         # Module + dependency pins
├── README.md               # You are here
├── LICENSE                 # MIT
└── learning-notes/         # gitignored — your personal notes & deep dives
```

Why `internal/`? Anything inside `internal/` cannot be imported by code outside
this module. That's a Go-language-level enforcement of "this is a private
implementation detail." It keeps the API surface honest.

## Development workflow

This project follows **trunk-based development** with feature branches per
phase. No `develop` branch, no GitFlow. Same pattern most modern teams use.

### Branch naming

```
phase-1/websocket-foundation
phase-2/postgres-history
phase-3/redis-pubsub-fanout
phase-4/redis-presence-cache
phase-5/rabbitmq-moderation-worker
phase-6/worker-pool-metrics
```

Within a phase, smaller feature branches are fine: `phase-1/graceful-shutdown`,
`phase-3/scale-test-script`, etc.

### Commits

- One logical change per commit.
- Use imperative mood: "Add Hub broadcast channel", not "Added" or "Adding".
- Reference the phase in the first line when relevant: `[phase-1] wire WebSocket upgrade handler`.
- Body is for *why*, not *what*. The diff already shows what changed.

### Pull requests

Even though this is a solo project, every phase ships through a PR. Reasons:
- The PR description is where you write up *what you learned*. That's the
  portfolio artifact.
- It forces you to read your own diff cold, like a reviewer would.
- CI runs against the PR before merge.

PR template lives in `.github/pull_request_template.md`.

### CI

GitHub Actions runs on every PR and push to `main`:
1. **Lint** — `golangci-lint run` with the config in `.golangci.yml`.
2. **Test** — `go test ./...` (will skip if no test files yet).
3. **Build** — `go build ./...` to confirm the module compiles.

A red CI run blocks merge. Fix the underlying issue; don't skip it.

## Programming standards (non-negotiable)

These are the standards this project commits to. If a piece of code violates
them, fix the code.

1. **`context.Context` is the first parameter of every I/O function.**
   No exceptions. This is how you cancel WebSocket reads on shutdown, abort
   slow DB queries, kill stuck worker jobs.

2. **Structured logging with `log/slog`.** Every log line has the relevant
   entity IDs. No `fmt.Println` in production code paths. Ever.

3. **Config via environment variables**, parsed into one `Config` struct at
   startup. No global state. No reading env vars deep in the code.

4. **Errors are wrapped with context.** `fmt.Errorf("doing X: %w", err)` so the
   chain is preserved. Use `errors.Is` / `errors.As` for inspection.

5. **Graceful shutdown.** On SIGTERM: stop accepting new connections, drain
   in-flight work, close DB/Redis/Rabbit cleanly, exit 0. A deploy must not
   lose messages.

6. **Health endpoints.** `/healthz` (am I running?) and `/readyz` (can I serve
   traffic — i.e. dependencies are reachable?).

7. **Integration tests use real infra** via `testcontainers-go`. Mocks lie.

8. **No `panic` outside `main`.** Panics are bugs. Return errors. The only
   place a panic is acceptable is `main()` calling `log.Fatal`.

9. **Goroutines must have a known stop condition.** Either they exit when a
   channel closes, when a context cancels, or when their parent goroutine
   says so. No "fire and forget" goroutines.

10. **Comments explain WHY, not WHAT.** The code already says what. The
    comment is for the reader who needs to know the constraint, the trade-off,
    or the historical incident.

## Learning notes

The `learning-notes/` folder is gitignored — that's your personal scratch
space. Each phase has a corresponding `phase-N-*.md` file pre-populated with
prompts, deliverables, and gotchas. The `concepts/` subfolder is for deep
dives on individual topics (the WebSocket protocol, Go concurrency, Pub/Sub
vs. message queues, etc.).

Treat the notes as a thinking tool, not a deliverable. Honest "I don't yet
understand X" entries are worth more than polished summaries.

## License

MIT — see [`LICENSE`](./LICENSE).
