package chat

import (
	"context"
	"log/slog"
)

// Hub is the in-process broker between connected Clients. It owns the set of
// clients per room and is the only goroutine allowed to mutate that set.
//
// Rather than guarding the rooms map with a mutex, the Hub gives ownership to a
// single goroutine (Run) and has everything else communicate with it over
// channels. Concurrent map access becomes impossible by construction, so there
// is no lock to forget and no risk of holding one across a slow network write.
type Hub struct {
	// rooms maps a room name to the set of clients in it. The inner map uses
	// struct{} values because only key presence matters. Only Run touches it.
	rooms map[string]map[*Client]struct{}

	// Inbound channels. Other goroutines send on them; Run is the sole receiver.
	register   chan *Client
	unregister chan *Client
	broadcast  chan Envelope

	// done is closed when Run returns so Register/Unregister/Broadcast can give
	// up instead of blocking forever on channels no one is receiving from.
	done chan struct{}

	logger *slog.Logger
}

// broadcastBuffer sizes the single server-wide broadcast channel. It absorbs
// bursts when many clients send before Run drains them. Tune under load.
const broadcastBuffer = 256

// NewHub constructs an unstarted Hub. Call Run (in a goroutine) to start it.
func NewHub(logger *slog.Logger) *Hub {
	return &Hub{
		rooms: make(map[string]map[*Client]struct{}),
		// register/unregister are unbuffered: join and leave are rare and should
		// rendezvous with Run directly. broadcast is buffered to absorb bursts.
		register:   make(chan *Client),
		unregister: make(chan *Client),
		broadcast:  make(chan Envelope, broadcastBuffer),
		done:       make(chan struct{}),
		logger:     logger,
	}
}

// Run owns the rooms map and is the only goroutine permitted to mutate it. It
// returns when ctx is cancelled. Invoke it in a goroutine: go hub.Run(ctx).
func (h *Hub) Run(ctx context.Context) {
	defer close(h.done)

	for {
		select {
		case c := <-h.register:
			h.addClient(c)
		case c := <-h.unregister:
			h.removeClient(c)
		case msg := <-h.broadcast:
			h.fanout(msg)
		case <-ctx.Done():
			// Close every client's outbound channel so each writePump exits and
			// closes its socket. Run only releases ownership of the map.
			h.closeAll()
			return
		}
	}
}

// addClient inserts c into its room, creating the room set on first use.
func (h *Hub) addClient(c *Client) {
	room := h.rooms[c.room]
	if room == nil {
		room = make(map[*Client]struct{})
		h.rooms[c.room] = room
	}
	room[c] = struct{}{}
	h.logger.Info("client registered", "room", c.room, "sender", c.sender, "room_size", len(room))
}

// removeClient deletes c from its room and closes its outbound channel.
//
// It must be idempotent: a client can be unregistered by its readPump on
// disconnect and dropped by a concurrent fan-out at the same time. Closing an
// already-closed channel panics, so we return early if c is already gone.
func (h *Hub) removeClient(c *Client) {
	room := h.rooms[c.room]
	if room == nil {
		return
	}
	if _, ok := room[c]; !ok {
		return
	}
	delete(room, c)
	close(c.outbound)

	if len(room) == 0 {
		delete(h.rooms, c.room)
	}
	h.logger.Info("client unregistered", "room", c.room, "sender", c.sender)
}

// fanout delivers msg to every client in msg.Room.
func (h *Hub) fanout(msg Envelope) {
	room := h.rooms[msg.Room]
	if room == nil {
		return
	}
	for c := range room {
		// Non-blocking send. Blocking on one slow client would stall fan-out to
		// the whole room, so a client whose buffer is full is dropped instead.
		select {
		case c.outbound <- msg:
		default:
			h.logger.Warn("dropping slow client: outbound buffer full",
				"room", msg.Room, "sender", c.sender)
			h.removeClient(c)
		}
	}
}

// closeAll closes every client's outbound channel during shutdown.
func (h *Hub) closeAll() {
	for name, room := range h.rooms {
		for c := range room {
			close(c.outbound)
		}
		delete(h.rooms, name)
	}
	h.logger.Info("hub shutting down: closed all client channels")
}

// Register adds a client to its room. Safe to call from any goroutine.
func (h *Hub) Register(c *Client) {
	select {
	case h.register <- c:
	case <-h.done:
	}
}

// Unregister removes a client. Safe to call from any goroutine and idempotent.
func (h *Hub) Unregister(c *Client) {
	select {
	case h.unregister <- c:
	case <-h.done:
	}
}

// Broadcast fans a message out to every client in its room. Safe to call from
// any goroutine.
func (h *Hub) Broadcast(msg Envelope) {
	select {
	case h.broadcast <- msg:
	case <-h.done:
	}
}
