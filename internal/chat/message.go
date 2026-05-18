// Package chat implements the real-time chat domain: the Hub that fans
// messages out to subscribers, the Client that owns a single WebSocket
// connection, and the message types they speak over the wire.
//
// This file defines the *wire protocol* — the JSON shape of messages that
// flow between the browser and the server.
//
// Why define a protocol explicitly?
// =================================
// The temptation in early development is to send "whatever shape feels right
// in the moment" and let the JSON dance on the wire. This always backfires.
// Within a week you can't tell what's a server-initiated event versus a
// client command, you've shipped three subtly different "join" message
// shapes, and the frontend silently ignores fields it doesn't recognize.
//
// A protocol is a contract. Defining it in one place — like this file —
// makes the contract reviewable. Every WebSocket message has exactly one
// shape per type. If you want to change the protocol you change this file,
// and every caller breaks visibly until they're updated.
package chat

import "time"

// MessageType is the discriminator for the message envelope below.
//
// We use a string type (not an int enum) because:
//   - It is self-describing in JSON / logs / browser DevTools.
//   - It is forward-compatible: new types can be added without renumbering.
//   - It costs ~10 bytes per message, which is negligible.
type MessageType string

// --- Client-to-server message types -----------------------------------------
// These are messages the browser sends to us.

const (
	// TypeJoin: the client is joining a room. Sent right after the
	// WebSocket connection is established and authenticated.
	TypeJoin MessageType = "join"

	// TypeLeave: the client is leaving a room. The browser SHOULD send
	// this when the user navigates away, but you cannot rely on it —
	// network failures, tab closes, and the user's wifi all mean you
	// must also handle the "client disappeared without notice" path
	// (the Hub's unregister channel).
	TypeLeave MessageType = "leave"

	// TypeChat: a chat message from the client. The server is responsible
	// for stamping the authoritative ID, sender, and timestamp — never
	// trust the client's claim for any of those fields.
	TypeChat MessageType = "chat"
)

// --- Server-to-client message types -----------------------------------------
// These are messages the server sends to browsers.

const (
	// TypeMessage: a chat message that should be displayed. May originate
	// from another user (or from this user — echoed back so the client UI
	// has a single source of truth for ordering and IDs).
	TypeMessage MessageType = "message"

	// TypeSystem: a notice from the server (joined, left, error, etc.).
	// Distinct from TypeMessage so the UI can render it differently
	// (system messages typically aren't avatared).
	TypeSystem MessageType = "system"

	// TypeError: something went wrong with the client's last action.
	// Includes a human-readable reason. The client should display this
	// without crashing.
	TypeError MessageType = "error"
)

// Envelope is the outer shape of every message on the wire. Both directions
// use the same envelope; only the Type discriminates intent.
//
// Why a single envelope? Browser-side parsing is simpler — one JSON.parse,
// one switch on Type. Server-side likewise. Avoid the temptation to define
// `IncomingMessage` and `OutgoingMessage` as separate types; the marginal
// type safety isn't worth the duplication.
type Envelope struct {
	// Type is required on every message. Decoders MUST switch on this
	// before touching any other field.
	Type MessageType `json:"type"`

	// Room identifies the chat room this message pertains to. Always set
	// for chat / join / leave / message / system.
	Room string `json:"room,omitempty"`

	// Text is the human-typed payload (or system text). Empty for join /
	// leave control messages.
	Text string `json:"text,omitempty"`

	// --- Server-set fields (clients should not populate these on send) --

	// ID is the server-assigned message ID. Stable across redelivery so
	// the UI can deduplicate. Populated only on server-to-client messages.
	ID string `json:"id,omitempty"`

	// Sender is the username (or display name) of who sent the message.
	// Server-assigned from the authenticated session, never client-claimed.
	Sender string `json:"sender,omitempty"`

	// Timestamp is when the server received the message. Use UTC.
	// Server-assigned. We use time.Time (RFC 3339 in JSON).
	Timestamp time.Time `json:"timestamp,omitempty"`
}
