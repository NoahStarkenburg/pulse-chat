package chat

import (
	"log/slog"
	"net/http"
)

// =============================================================================
// THE WEBSOCKET UPGRADE HANDLER
// =============================================================================
//
// A WebSocket connection starts life as an HTTP GET request with a special
// "Upgrade: websocket" header. The server responds with 101 Switching
// Protocols, and from that moment the same TCP connection is no longer
// speaking HTTP — it's speaking the WebSocket framing protocol.
//
// That moment of transition is what `Accept` (from coder/websocket)
// performs. After it returns successfully, you have a *websocket.Conn
// that lets you Read/Write WebSocket messages (each one becomes one or
// more frames on the wire).
//
// =============================================================================
// YOUR TASK (Phase 1)
// =============================================================================
//
// Implement NewWebSocketHandler(hub *Hub, logger *slog.Logger) http.Handler.
// The returned handler should:
//
//   1. Pull the desired room and (placeholder) sender name from the URL
//      query string: `/ws?room=general&name=alice`. In Phase 1 this is
//      fine; real auth (session cookie / JWT) comes much later. Treat
//      missing or empty values as 400 Bad Request.
//
//   2. Call websocket.Accept(w, r, opts). Set opts.InsecureSkipVerify =
//      true ONLY for local development — in production you must specify
//      OriginPatterns to defend against cross-site WebSocket hijacking.
//      Add a TODO comment about this.
//
//   3. Construct a Client, register it with the Hub, then start the
//      read/write pumps:
//
//          ctx := r.Context()
//          client := NewClient(conn, hub, room, name, logger)
//          hub.Register(client)
//          go client.writePump(ctx)
//          client.readPump(ctx)   // BLOCK here — when readPump returns,
//                                 // the connection is over.
//
//      Important: readPump must NOT be in a goroutine here. By blocking
//      in the handler, the http.Server keeps the connection alive. If
//      you launched readPump in a goroutine and returned, the http
//      package would treat the request as finished and would not work
//      correctly with the long-lived conn.
//
//   4. After readPump returns: Unregister + close the conn (defer the
//      close at the top of the handler so all exit paths clean up).
//
// =============================================================================
// COMMON FAILURES YOU WILL HIT
// =============================================================================
//
//   * "websocket: response does not implement http.Hijacker" — you set
//     up the handler with middleware that wraps the ResponseWriter in a
//     way that breaks hijacking. Mount this handler at the router level
//     without buffering middleware.
//
//   * "no carriage return after the request line" or similar from the
//     browser — your handshake response is malformed. Don't write to w
//     before Accept() returns.
//
//   * The connection drops after 60 seconds of silence — your reverse
//     proxy is timing out idle connections. Send periodic pings from
//     writePump (see client.go for guidance).
// =============================================================================

// NewWebSocketHandler returns an http.Handler that upgrades the request
// to a WebSocket and ties the resulting Client into the Hub for the
// duration of the connection.
func NewWebSocketHandler(hub *Hub, logger *slog.Logger) http.Handler {
	// TODO(phase-1): implement per the file header.
	_ = hub
	_ = logger
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not implemented yet — Phase 1 work in progress", http.StatusNotImplemented)
	})
}
