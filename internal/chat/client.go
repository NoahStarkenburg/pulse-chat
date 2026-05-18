package chat

import (
	"context"
	"log/slog"
)

// Client represents one connected user — one WebSocket connection, one
// browser tab. Each Client runs TWO goroutines for its lifetime:
//
//   - readPump:  reads messages from the WebSocket and forwards them to
//                the Hub (or whatever handler is appropriate).
//   - writePump: reads from the Client's outbound channel and writes to
//                the WebSocket.
//
// =============================================================================
// WHY TWO GOROUTINES (the most important Go concurrency lesson in this file)
// =============================================================================
//
// The WebSocket library (`coder/websocket`) is safe to use from MULTIPLE
// goroutines as long as you have one reader and one writer at a time.
// Concurrent writers will corrupt the byte stream — you'd be interleaving
// frames mid-message. Concurrent readers don't make sense either.
//
// So the rule is: one goroutine owns reads, one goroutine owns writes.
// They communicate through an in-memory channel (the Client's `outbound`
// channel) that the Hub pushes to and the writePump consumes.
//
// Picture the flow for sending a message:
//
//      Browser ── WS frame ──▶  readPump
//                                   │
//                                   ▼
//                             Hub.Broadcast(msg)
//                                   │
//                  (Hub Run loop fans out to each Client's outbound chan)
//                                   │
//                                   ▼
//                             other Client's writePump
//                                   │
//                                   ▼
//                             ◀── WS frame ─── other Browser
//
// =============================================================================
// YOUR TASK (Phase 1)
// =============================================================================
//
// 1. Add fields to Client:
//      - conn:      the *websocket.Conn (from coder/websocket)
//      - room:      string (which room this client is in)
//      - sender:    string (display name — Phase 1 can hardcode or accept
//                   on join; real auth comes later)
//      - outbound:  chan Envelope (buffered, e.g. cap 64). Closed by the
//                   Hub when the client is unregistered.
//      - hub:       *Hub (so the client can Register/Unregister itself)
//      - logger:    *slog.Logger
//
// 2. Implement NewClient(conn, hub, room, sender, logger) *Client. It
//    should create the outbound channel and return the struct. It should
//    NOT register with the Hub or start the pumps — that's the caller's
//    job, so that the wiring is explicit at the upgrade-handler site.
//
// 3. Implement readPump(ctx context.Context):
//      - Loop: read a JSON message from the conn.
//      - Unmarshal into an Envelope.
//      - SERVER-STAMP the fields the client should not control: ID,
//        Sender, Timestamp. Do this in the readPump, not later.
//      - For TypeChat: hub.Broadcast(msg).
//      - For other types: ignore for Phase 1 (we'll add /leave handling
//        when room state is more sophisticated).
//      - On ANY read error (including normal close): hub.Unregister(c)
//        and return. The error is your signal to stop.
//      - When this function returns, defer-close the conn.
//
// 4. Implement writePump(ctx context.Context):
//      - Loop:
//          - Select on c.outbound and ctx.Done().
//          - If c.outbound is closed (the !ok case in a channel receive),
//            return — the Hub has decided we're done.
//          - Otherwise, marshal the Envelope to JSON and write it on the
//            WebSocket.
//      - On any write error: return. The readPump's next attempt will
//        also fail and clean up.
//
// =============================================================================
// PING / PONG (keep-alive)
// =============================================================================
//
// Browsers and proxies will close idle WebSocket connections. To keep them
// open, you send WebSocket *ping* frames periodically and expect a *pong*
// back. `coder/websocket` exposes a Ping() method.
//
// For Phase 1 a 30-second ping interval is fine. Add a context-aware
// ticker inside writePump that calls c.conn.Ping(ctx) every 30s. If the
// ping fails or times out, the connection is dead — return from the pump.
//
// =============================================================================
// BACKPRESSURE: what to do when outbound is full
// =============================================================================
//
// The outbound channel is buffered. If the consumer (this client's
// browser) is slow, the buffer fills. The Hub's broadcast case must NOT
// block waiting for a slow client — that would freeze the whole chat.
//
// In Phase 1, the Hub's broadcast logic should:
//
//     select {
//     case client.outbound <- msg:
//         // sent
//     default:
//         // buffer full — client is too slow. Unregister and let them
//         // reconnect. Log this event.
//         h.Unregister(client)
//     }
//
// This is one of those "the right thing is the obvious thing once you've
// seen it" moments. Slow clients must not slow the system down.
type Client struct {
	// TODO(phase-1): add fields per the comment above.
	logger *slog.Logger
}

// NewClient constructs a Client. Caller is responsible for registering
// it with the Hub and starting the read/write pumps.
func NewClient(/* TODO(phase-1): conn, hub, room, sender, */ logger *slog.Logger) *Client {
	return &Client{logger: logger}
}

// readPump owns the WebSocket read side. Returns when the connection
// errors or the context is cancelled.
func (c *Client) readPump(ctx context.Context) {
	// TODO(phase-1): implement per the file header.
	_ = ctx
}

// writePump owns the WebSocket write side. Returns when the outbound
// channel is closed or the context is cancelled.
func (c *Client) writePump(ctx context.Context) {
	// TODO(phase-1): implement per the file header.
	_ = ctx
}
