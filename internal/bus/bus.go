// Package bus is the publish/subscribe message bus that lets multiple server
// instances exchange chat messages. Publishers send bytes to a named channel;
// every subscription on that channel receives a copy.
//
// The bus is fire-and-forget: a subscriber that is not connected when a message
// is published misses it, permanently. That is acceptable for chat because
// Postgres is the source of truth (a client can reload history), and it is why
// durable background work (Phase 5) uses a queue instead of this.
package bus

import "context"

// PubSub is a publish/subscribe bus. The chat package depends on this interface,
// not on Redis directly, so the Redis client can be swapped for the in-memory
// implementation in tests.
type PubSub interface {
	// Publish sends payload to every subscription on channel.
	Publish(ctx context.Context, channel string, payload []byte) error
	// Subscribe returns a Subscription that delivers every message published to
	// channel until the Subscription is closed.
	Subscribe(ctx context.Context, channel string) (Subscription, error)
	// Close releases the underlying connection.
	Close() error
}

// Subscription is a live subscription to a single channel.
type Subscription interface {
	// Messages delivers each published payload. The channel is closed when the
	// Subscription is closed or the underlying connection drops.
	Messages() <-chan []byte
	// Close unsubscribes and stops delivery.
	Close() error
}
