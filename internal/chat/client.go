package chat

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/NoahStarkenburg/pulse-chat/internal/store"
)

// MessageStore persists chat messages and loads recent history. store.MessageRepo
// satisfies it; the chat package depends on this interface, not on the store
// implementation directly, so it stays decoupled and easy to fake in tests.
type MessageStore interface {
	Insert(ctx context.Context, room, userID, body string) (store.Message, error)
	RecentByRoom(ctx context.Context, room string, limit int) ([]store.Message, error)
}

// Cache is the Redis-backed derived state a Client uses: per-user rate limiting,
// room presence, and the recent-message cache. cache.Cache satisfies it. The
// chat package depends on this interface, not the implementation, so tests can
// fake it and no import cycle forms: the cache traffics in opaque bytes, never
// chat types.
type Cache interface {
	// Allow reports whether the user may send another message now.
	Allow(ctx context.Context, userID string) (bool, error)
	// MarkPresent records that user was just seen in room.
	MarkPresent(ctx context.Context, room, user string) error
	// PushRecent appends a message payload to room's recent-message cache.
	PushRecent(ctx context.Context, room string, payload []byte) error
	// Recent returns up to limit cached payloads for room, oldest-first, or empty
	// on a miss.
	Recent(ctx context.Context, room string, limit int) ([][]byte, error)
}

// Client owns a single WebSocket connection. It runs two goroutines for the
// connection's lifetime: readPump (reads from the socket, persists, and forwards
// to the Hub) and writePump (writes Hub messages to the socket). They are split
// because a WebSocket is full-duplex, reads block indefinitely, and a connection
// must have exactly one writer or concurrent frames corrupt the stream.
type Client struct {
	conn   *websocket.Conn
	room   string // server-authoritative room
	userID string // authenticated user id (the messages foreign key)
	sender string // display name, from the authenticated session

	// outbound is this client's delivery queue: the Hub pushes envelopes on and
	// writePump drains them. The Hub closes it (once) to signal shutdown.
	outbound chan Envelope

	store  MessageStore
	cache  Cache
	hub    *Hub
	logger *slog.Logger
}

// outboundBuffer is the per-client queue depth. Large enough to ride out a
// brief network stall (and to hold the joined history burst), small enough that
// many clients fit in memory and a genuinely slow client is dropped before it
// accumulates stale messages.
const outboundBuffer = 64

// historyLimit is how many recent messages a joining client is sent.
const historyLimit = 50

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
func NewClient(conn *websocket.Conn, hub *Hub, store MessageStore, cache Cache, room, userID, sender string, logger *slog.Logger) *Client {
	return &Client{
		conn:     conn,
		room:     room,
		userID:   userID,
		sender:   sender,
		outbound: make(chan Envelope, outboundBuffer),
		store:    store,
		cache:    cache,
		hub:      hub,
		logger:   logger,
	}
}

// loadHistory sends up to historyLimit recent messages for the room to this
// client, oldest-first, before it joins the live feed. A failure is logged but
// not fatal: the client still receives live messages.
//
// Cache-aside: try the Redis recent-message cache first and fall back to
// Postgres on a miss. The cache is populated by the write path, so a cold or
// evicted room simply misses here and is served from the source of truth; the
// next message written repopulates it.
func (c *Client) loadHistory(ctx context.Context) {
	if payloads, err := c.cache.Recent(ctx, c.room, historyLimit); err != nil {
		c.logger.Warn("reading recent cache failed; falling back to store", "err", err, "room", c.room)
	} else if len(payloads) > 0 {
		for _, p := range payloads {
			var env Envelope
			if err := json.Unmarshal(p, &env); err != nil {
				c.logger.Warn("dropping malformed cached message", "err", err, "room", c.room)
				continue
			}
			c.queue(env)
		}
		return
	}

	msgs, err := c.store.RecentByRoom(ctx, c.room, historyLimit)
	if err != nil {
		c.logger.Error("loading history failed", "err", err, "room", c.room)
		return
	}
	for _, m := range msgs {
		ts := m.CreatedAt
		c.queue(Envelope{
			Type:      TypeMessage,
			Room:      c.room,
			Text:      m.Body,
			ID:        m.ID,
			Sender:    m.Sender,
			Timestamp: &ts,
		})
	}
}

// readPump reads from the socket until the connection errors or ctx is
// cancelled, then unregisters. Each chat message is persisted before it is
// published, so the room never sees a message that was not stored.
func (c *Client) readPump(ctx context.Context) {
	// Mark this user present on join so they show online immediately; writePump
	// refreshes it on a timer for as long as the connection lives.
	if err := c.cache.MarkPresent(ctx, c.room, c.sender); err != nil {
		c.logger.Warn("marking presence failed", "err", err, "room", c.room)
	}

	for {
		var env Envelope
		if err := wsjson.Read(ctx, c.conn, &env); err != nil {
			c.hub.Unregister(c)
			return
		}

		switch env.Type {
		case TypeChat:
			// Rate limit before doing any work. On a limiter error we fail OPEN
			// (allow): rate limiting is a protective measure, and refusing all chat
			// because its datastore is unreachable is worse than briefly not limiting.
			allowed, err := c.cache.Allow(ctx, c.userID)
			if err != nil {
				c.logger.Warn("rate limiter unavailable; allowing message", "err", err, "room", c.room)
			} else if !allowed {
				c.queue(Envelope{Type: TypeError, Room: c.room, Text: "you are sending messages too fast; slow down"})
				continue
			}

			// Persist first; publish only if the write succeeded. Publishing a
			// message we failed to store would show users something that vanishes on
			// reload. Text is the only field taken from the client; the store stamps
			// the id and timestamp.
			stored, err := c.store.Insert(ctx, c.room, c.userID, env.Text)
			if err != nil {
				c.logger.Error("persisting message failed; not publishing", "err", err, "room", c.room)
				c.queue(Envelope{Type: TypeError, Room: c.room, Text: "message could not be delivered"})
				continue
			}

			outgoing := Envelope{
				Type:      TypeMessage,
				Room:      c.room,
				Text:      stored.Body,
				ID:        stored.ID,
				Sender:    stored.Sender,
				Timestamp: &stored.CreatedAt,
			}

			// Warm the recent-message cache in the same code path as the DB write so
			// the two never drift. Best effort: the message is already durable in
			// Postgres, so a cache failure only costs a cache miss (and a Postgres
			// fallback) later.
			if data, err := json.Marshal(outgoing); err != nil {
				c.logger.Error("marshaling message for cache failed", "err", err, "room", c.room)
			} else if err := c.cache.PushRecent(ctx, c.room, data); err != nil {
				c.logger.Warn("caching recent message failed", "err", err, "room", c.room)
			}

			// Publish to the bus rather than fanning out here. This instance's own
			// subscription delivers the message back to its local clients (the
			// loopback), so the sender sees it exactly once and so do clients on
			// other instances. The message is already persisted, so a publish
			// failure only means it will not appear live; a refresh recovers it.
			if err := c.hub.Publish(ctx, outgoing); err != nil {
				c.logger.Error("publishing message failed", "err", err, "room", c.room)
			}
		default:
			c.logger.Debug("ignoring unsupported message type", "type", env.Type)
		}
	}
}

// queue enqueues an envelope to this client's outbound buffer, dropping it if
// the buffer is full rather than blocking the caller.
func (c *Client) queue(env Envelope) {
	select {
	case c.outbound <- env:
	default:
		c.logger.Warn("dropping outbound message: buffer full", "type", env.Type, "room", c.room)
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
			// Refresh presence on the keepalive cadence so an idle but connected
			// user (a lurker who never types) still counts as online. The ping
			// interval is shorter than the presence window, which keeps them from
			// flickering offline between refreshes.
			if err := c.cache.MarkPresent(ctx, c.room, c.sender); err != nil {
				c.logger.Warn("refreshing presence failed", "err", err, "room", c.room)
			}

		case <-ctx.Done():
			_ = c.conn.Close(websocket.StatusGoingAway, "server shutting down")
			return
		}
	}
}
