# Load tests

Pulse Chat claims horizontal scalability after Phase 3. This directory
holds the scripts that *prove* that claim with real numbers.

We use [k6](https://k6.io/) — a Go-based load testing tool with a
JavaScript scripting API. It handles WebSocket connections natively,
which most load testers don't.

## Install k6

```bash
winget install k6 --source winget         # Windows
brew install k6                           # macOS
sudo apt-get install k6                   # Debian/Ubuntu
docker run --rm -i grafana/k6 ...         # any platform
```

## Phase 3 deliverable

A `websocket-fanout.js` script that:

1. Spins up N virtual users (1000 to start).
2. Each connects to `ws://localhost:8080/ws?room=loadtest&name=user${i}`.
3. Each sends 1 chat message every 5 seconds.
4. Each measures how long until it receives messages from *other* users.

Metrics to capture and put in the README:

- Concurrent connections sustained.
- p50 / p95 / p99 fanout latency (send → receive on a different client).
- CPU / memory of each server instance under load.

## Phase 5 deliverable

A `moderation-throughput.js` script that:

1. Floods the chat with messages from many users.
2. Measures: jobs/sec processed by the worker pool, queue depth peak,
   time from message-sent to moderation-verdict.

## Phase 6 deliverable

A combined load test you can run in CI nightly to detect performance
regressions. Set a threshold (e.g. "p99 fanout latency < 100ms") and
fail the build if it regresses.

## Why this matters

"I built a horizontally-scalable chat system" is a claim. "I built a
system that handles 5000 concurrent WebSocket connections at p99 < 50ms
fanout latency on two `t3.small` instances" is a result. The second
sentence is what gets you the interview.
