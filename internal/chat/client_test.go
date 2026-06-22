// Client and handler integration tests. These drive a real WebSocket: an
// httptest server hosting the upgrade handler, with the coder/websocket dialer
// connecting to it, exercising upgrade -> Client -> readPump -> Hub -> writePump.
package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func startTestServer(t *testing.T) string {
	t.Helper()
	hub := NewHub(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	srv := httptest.NewServer(NewWebSocketHandler(hub, testLogger(), testSender))
	t.Cleanup(func() {
		srv.Close()
		cancel()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// testSender stands in for the production session lookup: it resolves the
// sender from the ?name= query so these handler tests run without the auth
// package.
func testSender(r *http.Request) (string, bool) {
	name := r.URL.Query().Get("name")
	return name, name != ""
}

func TestWebSocket_EchoToSender(t *testing.T) {
	// One client. The handler registers it before running readPump, so by the
	// time the server reads the chat the sender is a room member and the echo is
	// guaranteed without cross-client timing.
	base := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name=alice", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Send a chat with no id/sender/timestamp; the server must stamp them and
	// reclassify chat to message.
	if err := wsjson.Write(ctx, conn, Envelope{Type: TypeChat, Room: "general", Text: "hello"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got Envelope
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("read: %v", err)
	}

	if got.Type != TypeMessage {
		t.Errorf("Type = %q, want %q", got.Type, TypeMessage)
	}
	if got.Text != "hello" {
		t.Errorf("Text = %q, want %q", got.Text, "hello")
	}
	if got.Sender != "alice" {
		t.Errorf("Sender = %q, want server-stamped %q", got.Sender, "alice")
	}
	if got.Room != "general" {
		t.Errorf("Room = %q, want %q", got.Room, "general")
	}
	if got.ID == "" {
		t.Error("ID is empty; the server should stamp a unique ID")
	}
	if got.Timestamp == nil {
		t.Error("Timestamp is nil; the server should stamp it")
	}
}

func TestWebSocket_TwoClientsSameRoom(t *testing.T) {
	base := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	dial := func(name string) *websocket.Conn {
		t.Helper()
		c, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name="+name, nil)
		if err != nil {
			t.Fatalf("dial %s: %v", name, err)
		}
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		t.Cleanup(func() { c.Close(websocket.StatusNormalClosure, "") })
		return c
	}

	alice := dial("alice")
	bob := dial("bob")

	// Drain alice's own echoes so her outbound buffer never fills and gets her
	// dropped.
	go func() {
		for {
			var discard Envelope
			if err := wsjson.Read(ctx, alice, &discard); err != nil {
				return
			}
		}
	}()

	// Registration is async per connection, so alice sends repeatedly until bob
	// receives one.
	sendDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sendDone:
				return
			case <-ticker.C:
				_ = wsjson.Write(ctx, alice, Envelope{Type: TypeChat, Room: "general", Text: "ping"})
			}
		}
	}()

	var got Envelope
	if err := wsjson.Read(ctx, bob, &got); err != nil {
		t.Fatalf("bob read: %v", err)
	}
	close(sendDone)

	if got.Type != TypeMessage || got.Text != "ping" || got.Sender != "alice" {
		t.Errorf("bob got %+v, want a 'message' 'ping' from 'alice'", got)
	}
}

func TestWebSocket_CrossOriginRejected(t *testing.T) {
	// CSWSH defense: the handler upgrades with the default same-origin check, so
	// an upgrade carrying an Origin that does not match the host must be refused.
	// A non-browser client that sends no Origin (the other tests) is still
	// allowed; only a forged cross-origin Origin is rejected.
	base := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	header := http.Header{}
	header.Set("Origin", "http://evil.example.com")
	conn, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name=alice", &websocket.DialOptions{
		HTTPHeader: header,
	})
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("expected cross-origin upgrade to be rejected, but it succeeded")
	}
}

func TestWebSocket_MissingParamsRejected(t *testing.T) {
	// The handler validates room before upgrading, so a dial without it fails
	// the handshake.
	base := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, base+"/ws", nil)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err == nil {
		conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("expected dial to fail without room/name, but it succeeded")
	}
}
