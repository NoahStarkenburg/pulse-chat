package chat

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/NoahStarkenburg/pulse-chat/internal/bus"
)

// Hub is the in-process broker between connected Clients. It owns the set of
// clients per room and is the only goroutine allowed to mutate that set.
//
// Rather than guarding the rooms map with a mutex, the Hub gives ownership to a
// single goroutine (Run) and has everything else communicate with it over
// channels. Concurrent map access becomes impossible by construction, so there
// is no lock to forget and no risk of holding one across a slow network write.
//
// From Phase 3 the Hub no longer decides cross-instance delivery. A message is
// PUBLISHED to the bus (Redis); every instance subscribed to the room receives
// it and fans it out to its own local clients. The Hub's job shrinks to local
// delivery: Publish sends to the bus, the subscription pump calls deliverLocal,
// and Run fans that out to the room's clients here.
type Hub struct {
	// rooms maps a room name to the set of clients in it. The inner map uses
	// struct{} values because only key presence matters. Only Run touches it.
	rooms map[string]map[*Client]struct{}

	// Inbound channels. Other goroutines send on them; Run is the sole receiver.
	register   chan *Client
	unregister chan *Client
	deliver    chan Envelope

	// done is closed when Run returns so Register/Unregister/deliverLocal can give
	// up instead of blocking forever on channels no one is receiving from.
	done chan struct{}

	logger *slog.Logger

	// bus carries messages between instances; subs manages one subscription per
	// room with local clients and feeds received messages into deliver.
	bus  bus.PubSub
	subs *subscriptions
}

// deliverBuffer sizes the single server-wide local-delivery channel. It absorbs
// bursts when messages arrive from the bus faster than Run drains them. Tune
// under load.
const deliverBuffer = 256

// NewHub constructs an unstarted Hub bound to a message bus. Call Run (in a
// goroutine) to start it.
func NewHub(logger *slog.Logger, b bus.PubSub) *Hub {
	h := &Hub{
		rooms: make(map[string]map[*Client]struct{}),
		// register/unregister are unbuffered: join and leave are rare and should
		// rendezvous with Run directly. deliver is buffered to absorb bursts.
		register:   make(chan *Client),
		unregister: make(chan *Client),
		deliver:    make(chan Envelope, deliverBuffer),
		done:       make(chan struct{}),
		logger:     logger,
		bus:        b,
	}
	h.subs = newSubscriptions(b, h.deliverLocal, logger)
	return h
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
		case msg := <-h.deliver:
			h.fanout(msg)
		case <-ctx.Done():
			// Close every client's outbound channel so each writePump exits and
			// closes its socket, then close every bus subscription. Run only
			// releases ownership of the map.
			h.closeAll()
			h.subs.closeAll()
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

// Subscribe ensures this instance receives bus messages for room. Reference
// counted: call once per joining connection, paired with Unsubscribe. Safe to
// call from any goroutine.
func (h *Hub) Subscribe(room string) error {
	return h.subs.acquire(room)
}

// Unsubscribe drops one reference to room's subscription, closing it when the
// last local client leaves.
func (h *Hub) Unsubscribe(room string) {
	h.subs.release(room)
}

// Publish sends env to every instance (including this one) via the bus. The
// loopback to this instance's own subscription is what delivers the message to
// local clients, so callers must NOT also fan out locally or clients see it
// twice.
func (h *Hub) Publish(ctx context.Context, env Envelope) error {
	data, err := json.Marshal(env)
	if err != nil {
		return err
	}
	return h.bus.Publish(ctx, channelForRoom(env.Room), data)
}

// deliverLocal hands a message to Run for fan-out to local clients. It is called
// by the subscription pump for every message received from the bus, including
// this instance's own published messages. Safe to call from any goroutine.
func (h *Hub) deliverLocal(env Envelope) {
	select {
	case h.deliver <- env:
	case <-h.done:
	}
}
