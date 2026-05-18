package chat

import (
	"context"
	"log/slog"
)

// Hub is the broker between connected Clients within a single server
// process. It owns the set of clients per room and is the only goroutine
// permitted to mutate that set.
//
// =============================================================================
// THE HUB PATTERN — what you are about to build (and why)
// =============================================================================
//
// Problem: many goroutines (one per WebSocket connection) need to share a
// data structure: "which clients are in which rooms?" Naively, you'd protect
// that map with a sync.Mutex. That works but invites subtle bugs — every
// caller has to remember to lock, you risk holding the lock across a slow
// network write, and you have to reason about lock ordering.
//
// The Hub pattern sidesteps the lock entirely. *One* goroutine owns the map.
// Other goroutines send it requests over channels:
//
//   - register:   "here is a new Client, add it to room X"
//   - unregister: "this Client is gone, remove it"
//   - broadcast:  "send this message to everyone in room X"
//
// The Hub's Run loop reads from these channels in a select{} and applies
// changes. Because only Run() touches the map, there is no data race, no
// locking, and no ambiguity about who can modify what.
//
// This is THE canonical Go concurrency pattern: "share memory by
// communicating." Memorize it. You will see it everywhere.
//
// =============================================================================
// YOUR TASK (Phase 1)
// =============================================================================
//
// 1. Add fields to Hub:
//      - rooms: a map[string]map[*Client]struct{} (room -> set of clients)
//      - register chan *Client       (clients ask to join here)
//      - unregister chan *Client     (clients leave here)
//      - broadcast chan Envelope     (fan-out messages here)
//      - logger *slog.Logger
//
//    Why struct{} for the inner map value? It's a "set" — we only care
//    whether a key is present. struct{} consumes zero bytes (Go optimizes
//    it). map[*Client]bool would work but signals "the bool means
//    something" which it doesn't.
//
// 2. Implement NewHub() to return a Hub with channels initialized.
//
//    Note: use *buffered* channels for broadcast (e.g. cap 256) so a slow
//    sender does not block the senders. The Hub's Run loop is the consumer;
//    if it falls behind, the buffer absorbs short bursts.
//
// 3. Implement Run(ctx context.Context). It must:
//      - Loop forever inside a `for { select { ... } }`.
//      - Handle the four cases: register / unregister / broadcast / ctx.Done.
//      - On ctx.Done, exit cleanly. Closing client connections is the
//        caller's job (Run just stops managing the rooms map).
//      - Be the *only* place rooms map is mutated.
//
// 4. Implement Register / Unregister / Broadcast methods that just send on
//    the corresponding channels. These are the public API; the channels
//    themselves should be unexported.
//
// =============================================================================
// GOTCHAS YOU WILL HIT
// =============================================================================
//
//   * If you call Hub.Broadcast() from inside the Hub's own goroutine, you
//     will deadlock — Run can't read from the channel while it's busy
//     sending to it. Solution: Run never sends to its own channels. It only
//     reads from them.
//
//   * When broadcasting, do *not* write to client connections directly.
//     The Hub does not own those connections. Instead, push the Envelope
//     onto each Client's outbound channel and let each Client's writePump
//     deliver it. (See client.go.) This keeps the Hub fast — it never
//     blocks on a slow network write.
//
//   * If a Client's outbound channel is full, that client is slow. You have
//     two choices: (a) drop the message for that client, (b) disconnect the
//     client. For Phase 1, choose (b) and put the unregister logic right in
//     the broadcast case.
//
//   * Goroutines must have a known stop condition. Run exits when ctx is
//     done. Document this — future-you will thank you.
//
// =============================================================================
type Hub struct {
	// TODO(phase-1): add fields per the comment above.
	logger *slog.Logger
}

// NewHub constructs an unstarted Hub. Call Run() (in a goroutine) to start it.
func NewHub(logger *slog.Logger) *Hub {
	// TODO(phase-1): initialize the channels and the rooms map.
	return &Hub{
		logger: logger,
	}
}

// Run is the Hub's main loop. It owns the rooms map and is the only
// goroutine permitted to mutate it. Returns when ctx is cancelled.
//
// Convention: this is a blocking call — invoke it in a goroutine:
//
//	go hub.Run(ctx)
func (h *Hub) Run(ctx context.Context) {
	// TODO(phase-1): the select-loop described in the file header.
	_ = ctx
}

// Register asks the Hub to add a client to its room. Safe to call from
// any goroutine.
func (h *Hub) Register(c *Client) {
	// TODO(phase-1): send on the register channel.
	_ = c
}

// Unregister asks the Hub to remove a client. Safe to call from any
// goroutine. Idempotent — calling Unregister twice for the same client
// MUST NOT panic.
func (h *Hub) Unregister(c *Client) {
	// TODO(phase-1): send on the unregister channel.
	_ = c
}

// Broadcast fans out a message to every client in the message's Room.
// Safe to call from any goroutine. Non-blocking from the caller's
// perspective (it enqueues onto a buffered channel).
func (h *Hub) Broadcast(msg Envelope) {
	// TODO(phase-1): send on the broadcast channel.
	_ = msg
}
