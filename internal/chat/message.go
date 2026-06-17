// Package chat implements the real-time chat domain: the Hub that fans
// messages out to clients, the Client that owns a single WebSocket connection,
// and the message types they speak over the wire.
//
// This file defines the wire protocol: the JSON shape of every message
// exchanged between the browser and the server.
package chat

import "time"

// MessageType is the discriminator for an Envelope. It is a string rather than
// an int enum so messages are self-describing in logs and the browser console,
// and new types can be added without renumbering.
type MessageType string

// Client-to-server message types.
const (
	TypeJoin  MessageType = "join"
	TypeLeave MessageType = "leave"
	TypeChat  MessageType = "chat"
)

// Server-to-client message types.
const (
	TypeMessage MessageType = "message" // a chat message to display
	TypeSystem  MessageType = "system"  // a server notice (joined, left, ...)
	TypeError   MessageType = "error"   // a problem with the client's last action
)

// Envelope is the outer shape of every message on the wire. Both directions
// use the same envelope; only Type discriminates intent.
type Envelope struct {
	Type MessageType `json:"type"`
	Room string      `json:"room,omitempty"`
	Text string      `json:"text,omitempty"`

	// Server-set fields. Clients must not populate these on send; the server
	// stamps them so a client cannot forge identity, room, or time.
	ID     string `json:"id,omitempty"`
	Sender string `json:"sender,omitempty"`

	// Timestamp is when the server received the message (UTC). It is a pointer
	// because omitempty does not omit a zero time.Time value (a zero time is a
	// non-empty struct to encoding/json); a nil *time.Time is omitted correctly.
	Timestamp *time.Time `json:"timestamp,omitempty"`
}
