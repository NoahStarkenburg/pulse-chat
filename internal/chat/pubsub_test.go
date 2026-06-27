// Tests for the Phase 3 Pub/Sub behavior: messages crossing server instances
// through a shared bus, the per-room subscription reference-count lifecycle, and
// the loopback (a publisher receives its own message exactly once). They use the
// in-memory bus so two in-process "instances" sharing one bus stand in for two
// servers sharing one Redis, with no broker running.
package chat

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/NoahStarkenburg/pulse-chat/internal/bus"
)

// startInstance starts one server "instance": its own Hub and HTTP server, but
// bound to the shared bus so instances exchange messages through it.
func startInstance(t *testing.T, b bus.PubSub, store MessageStore) string {
	t.Helper()
	hub := NewHub(testLogger(), b)
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	srv := httptest.NewServer(NewWebSocketHandler(hub, store, testLogger(), testSender))
	t.Cleanup(func() {
		srv.Close()
		cancel()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func publishEnvelope(t *testing.T, b bus.PubSub, room string, env Envelope) {
	t.Helper()
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := b.Publish(context.Background(), channelForRoom(room), data); err != nil {
		t.Fatalf("publish: %v", err)
	}
}

func TestWebSocket_CrossInstanceDelivery(t *testing.T) {
	// Two instances share one bus and one store (as two servers would share one
	// Redis and one Postgres). Alice connects to instance A, Bob to instance B,
	// in the same room. A message Alice sends must reach Bob on the other
	// instance, which only works if it travelled through the bus.
	b := bus.NewMemory()
	store := newFakeStore()
	instanceA := startInstance(t, b, store)
	instanceB := startInstance(t, b, store)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	bob, resp, err := websocket.Dial(ctx, instanceB+"/ws?room=general&name=bob", nil)
	if err != nil {
		t.Fatalf("bob dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer bob.Close(websocket.StatusNormalClosure, "")

	alice, resp2, err := websocket.Dial(ctx, instanceA+"/ws?room=general&name=alice", nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	if resp2 != nil && resp2.Body != nil {
		_ = resp2.Body.Close()
	}
	defer alice.Close(websocket.StatusNormalClosure, "")

	// Drain alice's own echoes so her outbound buffer never fills.
	go func() {
		for {
			var discard Envelope
			if err := wsjson.Read(ctx, alice, &discard); err != nil {
				return
			}
		}
	}()

	// Subscription and registration are async per connection, so alice sends
	// repeatedly until bob, on the other instance, receives one.
	sendDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sendDone:
				return
			case <-ticker.C:
				_ = wsjson.Write(ctx, alice, Envelope{Type: TypeChat, Room: "general", Text: "cross"})
			}
		}
	}()

	var got Envelope
	if err := wsjson.Read(ctx, bob, &got); err != nil {
		t.Fatalf("bob read: %v", err)
	}
	close(sendDone)

	if got.Type != TypeMessage || got.Text != "cross" || got.Sender != "alice" {
		t.Errorf("bob on instance B got %+v, want a 'message' 'cross' from 'alice' on instance A", got)
	}
}

func TestSubscriptions_RefCountedLifecycle(t *testing.T) {
	// Subscribe on the first acquire, stay subscribed while references remain,
	// unsubscribe only when the last releases.
	b := bus.NewMemory()
	received := make(chan Envelope, 8)
	subs := newSubscriptions(b, func(e Envelope) { received <- e }, testLogger())

	if err := subs.acquire("r"); err != nil { // first client: subscribes
		t.Fatalf("acquire 1: %v", err)
	}
	if err := subs.acquire("r"); err != nil { // second client: ref-count only
		t.Fatalf("acquire 2: %v", err)
	}

	publishEnvelope(t, b, "r", Envelope{Type: TypeMessage, Room: "r", Text: "one"})
	if got := recvEnvelope(t, received); got.Text != "one" {
		t.Errorf("after acquire: got %q, want one", got.Text)
	}

	subs.release("r") // one client leaves: still subscribed (ref 1 remains)
	publishEnvelope(t, b, "r", Envelope{Type: TypeMessage, Room: "r", Text: "two"})
	if got := recvEnvelope(t, received); got.Text != "two" {
		t.Errorf("after one release: got %q, want two", got.Text)
	}

	subs.release("r") // last client leaves: unsubscribed
	publishEnvelope(t, b, "r", Envelope{Type: TypeMessage, Room: "r", Text: "three"})
	select {
	case got := <-received:
		t.Errorf("received %+v after the last release; should be unsubscribed", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func recvEnvelope(t *testing.T, ch <-chan Envelope) Envelope {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for a delivered message")
		return Envelope{}
	}
}

func TestWebSocket_SenderReceivesOwnMessageOnce(t *testing.T) {
	// The publishing instance also receives its own message via its subscription
	// (the loopback). Because we publish instead of also fanning out locally, the
	// sender must see its message exactly once, never twice.
	base := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, base+"/ws?room=loop&name=alice", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := wsjson.Write(ctx, conn, Envelope{Type: TypeChat, Room: "loop", Text: "echo"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got Envelope
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if got.Type != TypeMessage || got.Text != "echo" {
		t.Fatalf("first read = %+v, want the 'echo' message", got)
	}

	// A second read with a short deadline must NOT yield a duplicate.
	dupCtx, dupCancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer dupCancel()
	var dup Envelope
	if err := wsjson.Read(dupCtx, conn, &dup); err == nil {
		t.Fatalf("sender received a duplicate of its own message: %+v", dup)
	}
}
