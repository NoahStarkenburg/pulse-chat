package chat

import (
	"log/slog"
	"net/http"

	"github.com/coder/websocket"
)

// NewWebSocketHandler returns an http.Handler that upgrades the request to a
// WebSocket and ties the resulting Client into the Hub for the connection's
// lifetime.
//
// A WebSocket starts as an HTTP GET with an Upgrade header; websocket.Accept
// performs the 101 handshake, after which the connection is hijacked and the
// normal HTTP machinery no longer manages it.
func NewWebSocketHandler(hub *Hub, logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		room := r.URL.Query().Get("room")
		name := r.URL.Query().Get("name")
		if room == "" || name == "" {
			http.Error(w, "missing required query params: room, name", http.StatusBadRequest)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			// InsecureSkipVerify disables the Origin check. Safe only because
			// Phase 1 has no cookies or session to hijack (CSWSH). Replace with
			// OriginPatterns in Phase 1.5 once auth exists.
			InsecureSkipVerify: true,
		})
		if err != nil {
			logger.Warn("websocket upgrade failed", "err", err, "room", room, "remote", r.RemoteAddr)
			return
		}
		// Release the connection on every exit path. Harmless if writePump
		// already closed it gracefully.
		defer conn.CloseNow()

		client := NewClient(conn, hub, room, name, logger)
		hub.Register(client)

		ctx := r.Context()
		go client.writePump(ctx)
		// readPump runs in the handler goroutine and blocks for the connection's
		// lifetime. While the handler is executing the server keeps the
		// connection open; returning early would tear it down.
		client.readPump(ctx)
	})
}
