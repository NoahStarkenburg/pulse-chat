// Hub tests. Because the Hub talks to clients only through channels, its full
// behavior can be exercised without a network or mocks. Run is a single
// goroutine that processes one event at a time, so a blocking read on a client's
// outbound channel synchronizes the test past the Hub's handling of a prior
// event.
package chat

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	h := NewHub(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)
	t.Cleanup(cancel)
	return h
}

// newTestClient builds a Client without a real WebSocket. The Hub never touches
// Client.conn, so an outbound channel is all it needs.
func newTestClient(room, sender string, buffer int) *Client {
	return &Client{
		room:     room,
		sender:   sender,
		outbound: make(chan Envelope, buffer),
	}
}

// recv reads one envelope from a client's outbound channel, failing if nothing
// arrives within a second. The bool reports whether the channel is still open.
func recv(t *testing.T, c *Client) (Envelope, bool) {
	t.Helper()
	select {
	case env, ok := <-c.outbound:
		return env, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a message on client.outbound")
		return Envelope{}, false
	}
}

func TestHub_RunStopsOnContextCancel(t *testing.T) {
	h := NewHub(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { h.Run(ctx); close(done) }()

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of context cancel (goroutine leak)")
	}
}

func TestHub_BroadcastReachesRoomMembers(t *testing.T) {
	h := newTestHub(t)
	a := newTestClient("general", "alice", 8)
	b := newTestClient("general", "bob", 8)
	h.Register(a)
	h.Register(b)

	h.Broadcast(Envelope{Type: TypeMessage, Room: "general", Text: "hi"})

	for _, c := range []*Client{a, b} {
		env, ok := recv(t, c)
		if !ok {
			t.Fatalf("%s: outbound closed unexpectedly", c.sender)
		}
		if env.Text != "hi" {
			t.Errorf("%s: Text = %q, want %q", c.sender, env.Text, "hi")
		}
	}
}

func TestHub_BroadcastIsRoomScoped(t *testing.T) {
	h := newTestHub(t)
	a := newTestClient("roomA", "alice", 8)
	b := newTestClient("roomB", "bob", 8)
	h.Register(a)
	h.Register(b)

	h.Broadcast(Envelope{Type: TypeMessage, Room: "roomA", Text: "only A"})

	if env, ok := recv(t, a); !ok || env.Text != "only A" {
		t.Fatalf("roomA client missed its message (ok=%v, text=%q)", ok, env.Text)
	}
	// Give a short window to confirm the message does not cross rooms.
	select {
	case env := <-b.outbound:
		t.Fatalf("roomB client wrongly received a roomA message: %+v", env)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHub_UnregisterStopsDelivery(t *testing.T) {
	h := newTestHub(t)
	c := newTestClient("general", "alice", 8)
	h.Register(c)
	h.Unregister(c)

	// Unregister closes the outbound channel; the next receive observes that.
	if _, ok := recv(t, c); ok {
		t.Fatal("expected outbound to be closed after Unregister")
	}
}

func TestHub_UnregisterIsIdempotent(t *testing.T) {
	h := newTestHub(t)
	c := newTestClient("general", "alice", 8)
	h.Register(c)

	h.Unregister(c)
	if _, ok := recv(t, c); ok {
		t.Fatal("expected outbound closed after first Unregister")
	}

	// A second Unregister must not panic; without the presence guard it would
	// double-close the channel and crash the Hub goroutine.
	h.Unregister(c)

	// Confirm the Hub goroutine survived and still processes events.
	c2 := newTestClient("general", "bob", 8)
	h.Register(c2)
	h.Broadcast(Envelope{Type: TypeMessage, Room: "general", Text: "still alive"})
	if env, ok := recv(t, c2); !ok || env.Text != "still alive" {
		t.Fatalf("hub unhealthy after double Unregister (ok=%v, text=%q)", ok, env.Text)
	}
}

func TestHub_DropsSlowClient(t *testing.T) {
	h := newTestHub(t)

	// slow has a buffer of 1 that we pre-fill and never read, so it stays full
	// during fan-out. fast has room to spare.
	slow := newTestClient("general", "slow", 1)
	slow.outbound <- Envelope{Text: "prefill"}
	fast := newTestClient("general", "fast", 8)
	h.Register(slow)
	h.Register(fast)

	// Run processes the broadcast channel FIFO, so the second fan-out runs only
	// after the first completes. During the first, slow's buffer is full and it
	// is dropped instead of blocking.
	h.Broadcast(Envelope{Type: TypeMessage, Room: "general", Text: "one"})
	h.Broadcast(Envelope{Type: TypeMessage, Room: "general", Text: "two"})

	// Receiving both on fast proves both fan-outs ran, so slow was dropped.
	if env, ok := recv(t, fast); !ok || env.Text != "one" {
		t.Fatalf("fast: got (ok=%v, %q), want 'one'", ok, env.Text)
	}
	if env, ok := recv(t, fast); !ok || env.Text != "two" {
		t.Fatalf("fast: got (ok=%v, %q), want 'two'", ok, env.Text)
	}

	// slow holds only the prefill, then its channel is closed.
	if env, ok := recv(t, slow); !ok || env.Text != "prefill" {
		t.Fatalf("slow: got (ok=%v, %q), want buffered 'prefill'", ok, env.Text)
	}
	if _, ok := recv(t, slow); ok {
		t.Fatal("slow: expected outbound closed (client dropped), but it is still open")
	}
}

func TestHub_ShutdownClosesAllClients(t *testing.T) {
	h := NewHub(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	go h.Run(ctx)

	a := newTestClient("general", "alice", 8)
	b := newTestClient("general", "bob", 8)
	h.Register(a)
	h.Register(b)

	cancel() // shutdown closes every client's outbound

	for _, c := range []*Client{a, b} {
		if _, ok := recv(t, c); ok {
			t.Fatalf("%s: expected outbound closed on shutdown", c.sender)
		}
	}
}
