# Pulse Chat - frontend

This directory will hold a minimal browser client for testing and
demoing the chat. Phase 1 builds a single HTML page with a tiny bit of
JavaScript that opens a WebSocket and renders messages - that's
enough to verify end-to-end functionality.

We deliberately avoid React / Vue / heavy frameworks here. The frontend
isn't the lesson, but it IS the demo. Vanilla JS + a single HTML file
is enough to:

- Open one or two browser tabs side-by-side and chat between them.
- Record a 30-second demo gif for the README.
- Test reconnection behavior.

## Phase 1 deliverable

A `static/index.html` that:

1. Connects to `ws://localhost:8080/ws?room=<x>&name=<y>` (parameters
   from URL query or simple form inputs).
2. Renders incoming messages as a scrolling list.
3. Sends typed messages on Enter / button click.
4. Shows connection state (connected / reconnecting / closed).

Served by the Go server at `GET /`. See `cmd/server/main.go` for the
`mux.Handle("/", http.FileServer(...))` snippet that mounts static files.

## Why not Next.js / React?

If after Phase 6 you want a polished portfolio frontend, a tasteful
Next.js app deployed to Vercel is a fine addition - and it's a Conduit
opportunity too. For Pulse Chat itself, the browser demo just needs to
prove end-to-end. Don't let frontend complexity steal cycles from
backend learning.

## Later additions (optional)

- Presence indicator (Phase 4) - show who's currently in the room.
- Flagged-message UI (Phase 5) - gray out / strike through.
- Reconnect-with-history (stretch) - show last 50 messages on reconnect.
- A separate "admin" page (Phase 6) consuming `/metrics` for a tiny
  in-browser dashboard.
