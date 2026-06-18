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
// Mount it behind authentication. resolveSender returns the authenticated
// username for the request (in production, from the session that RequireAuth
// validated). The sender is taken from there, never from a query param, so a
// client cannot claim another identity.
//
// A WebSocket starts as an HTTP GET with an Upgrade header; websocket.Accept
// performs the 101 handshake, after which the connection is hijacked and the
// normal HTTP machinery no longer manages it.
func NewWebSocketHandler(hub *Hub, logger *slog.Logger, resolveSender func(*http.Request) (string, bool)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		room := r.URL.Query().Get("room")
		if room == "" {
			http.Error(w, "missing required query param: room", http.StatusBadRequest)
			return
		}
		sender, ok := resolveSender(r)
		if !ok {
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}

		// Nil AcceptOptions keeps coder/websocket's default Origin check (Origin
		// must match Host). That is the CSWSH defense now that a session cookie
		// exists: a malicious cross-origin page cannot open an authenticated
		// socket. Non-browser clients send no Origin and are still allowed.
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			logger.Warn("websocket upgrade failed", "err", err, "room", room, "remote", r.RemoteAddr)
			return
		}
		// Release the connection on every exit path. Harmless if writePump
		// already closed it gracefully.
		defer func() { _ = conn.CloseNow() }()

		client := NewClient(conn, hub, room, sender, logger)
		hub.Register(client)

		ctx := r.Context()
		go client.writePump(ctx)
		// readPump runs in the handler goroutine and blocks for the connection's
		// lifetime. While the handler is executing the server keeps the
		// connection open; returning early would tear it down.
		client.readPump(ctx)
	})
}
