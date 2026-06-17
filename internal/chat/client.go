package chat

import (
	"context"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/google/uuid"
)

// Client owns a single WebSocket connection. It runs two goroutines for the
// connection's lifetime: readPump (reads from the socket, forwards to the Hub)
// and writePump (writes Hub messages to the socket). They are split because a
// WebSocket is full-duplex, reads block indefinitely, and a connection must
// have exactly one writer or concurrent frames corrupt the stream.
type Client struct {
	conn   *websocket.Conn
	room   string // server-authoritative room
	sender string // display name (Phase 1: query param; later: auth session)

	// outbound is this client's delivery queue: the Hub pushes envelopes on and
	// writePump drains them. The Hub closes it (once) to signal shutdown.
	outbound chan Envelope

	hub    *Hub
	logger *slog.Logger
}

// outboundBuffer is the per-client queue depth. Large enough to ride out a
// brief network stall, small enough that many clients fit in memory and a
// genuinely slow client is dropped before it accumulates stale messages.
const outboundBuffer = 64

const (
	// pingInterval is how often we probe an idle connection so a peer that
	// vanished is detected and intermediaries do not kill the idle connection.
	pingInterval = 30 * time.Second
	// pingTimeout is how long we wait for a pong before declaring the peer dead.
	pingTimeout = 10 * time.Second
	// writeTimeout bounds a single write so a stuck socket cannot hang writePump.
	writeTimeout = 10 * time.Second
)

// NewClient constructs a Client. Registering it with the Hub and starting the
// pumps is left to the caller so the connection lifecycle stays explicit.
func NewClient(conn *websocket.Conn, hub *Hub, room, sender string, logger *slog.Logger) *Client {
	return &Client{
		conn:     conn,
		room:     room,
		sender:   sender,
		outbound: make(chan Envelope, outboundBuffer),
		hub:      hub,
		logger:   logger,
	}
}

// readPump reads from the socket until the connection errors or ctx is
// cancelled, then unregisters. The server overwrites every consequential field
// so a client cannot forge identity, room, timestamp, or message ID.
func (c *Client) readPump(ctx context.Context) {
	for {
		var env Envelope
		if err := wsjson.Read(ctx, c.conn, &env); err != nil {
			c.hub.Unregister(c)
			return
		}

		switch env.Type {
		case TypeChat:
			now := time.Now().UTC()
			out := Envelope{
				Type:      TypeMessage,
				Room:      c.room,
				Text:      env.Text, // the only field taken from the client
				ID:        uuid.NewString(),
				Sender:    c.sender,
				Timestamp: &now,
			}
			c.hub.Broadcast(out)
		default:
			c.logger.Debug("ignoring non-chat message in phase 1", "type", env.Type)
		}
	}
}

// writePump writes to the socket until the outbound channel is closed, a write
// or ping fails, or ctx is cancelled.
func (c *Client) writePump(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-c.outbound:
			if !ok {
				// Hub closed our channel: we have been unregistered. Send a close
				// frame so the peer sees a clean close rather than a dropped TCP
				// connection.
				_ = c.conn.Close(websocket.StatusNormalClosure, "")
				return
			}

			writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := wsjson.Write(writeCtx, c.conn, msg)
			cancel()
			if err != nil {
				c.hub.Unregister(c)
				return
			}

		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := c.conn.Ping(pingCtx)
			cancel()
			if err != nil {
				c.hub.Unregister(c)
				return
			}

		case <-ctx.Done():
			_ = c.conn.Close(websocket.StatusGoingAway, "server shutting down")
			return
		}
	}
}
