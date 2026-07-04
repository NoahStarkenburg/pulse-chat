// Client and handler integration tests. These drive a real WebSocket: an
// httptest server hosting the upgrade handler, with the coder/websocket dialer
// connecting to it, exercising upgrade -> Client -> readPump -> Hub -> writePump.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/NoahStarkenburg/pulse-chat/internal/bus"
	"github.com/NoahStarkenburg/pulse-chat/internal/store"
)

func startTestServer(t *testing.T) string {
	return startTestServerWith(t, newFakeStore())
}

func startTestServerWith(t *testing.T, store MessageStore) string {
	return startTestServerWithCache(t, store, newFakeCache())
}

func startTestServerWithCache(t *testing.T, store MessageStore, cache Cache) string {
	t.Helper()
	hub := NewHub(testLogger(), bus.NewMemory())
	ctx, cancel := context.WithCancel(context.Background())
	go hub.Run(ctx)

	srv := httptest.NewServer(NewWebSocketHandler(hub, store, cache, testLogger(), testSender))
	t.Cleanup(func() {
		srv.Close()
		cancel()
	})
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// failingStore is a MessageStore whose Insert always fails, used to prove a
// message that cannot be persisted is not broadcast.
type failingStore struct{}

func (failingStore) Insert(context.Context, string, string, string) (store.Message, error) {
	return store.Message{}, fmt.Errorf("insert failed")
}

func (failingStore) RecentByRoom(context.Context, string, int) ([]store.Message, error) {
	return nil, nil
}

// testSender stands in for the production session lookup: it resolves the
// sender from the ?name= query so these handler tests run without the auth
// package. The user id and display name are both the name, for simplicity.
func testSender(r *http.Request) (string, string, bool) {
	name := r.URL.Query().Get("name")
	return name, name, name != ""
}

// fakeStore is an in-memory MessageStore for the handler tests. It records
// messages per room and replays them, standing in for the Postgres repo.
type fakeStore struct {
	mu      sync.Mutex
	counter int
	byRoom  map[string][]store.Message
}

func newFakeStore() *fakeStore {
	return &fakeStore{byRoom: make(map[string][]store.Message)}
}

func (f *fakeStore) Insert(_ context.Context, room, userID, body string) (store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.counter++
	m := store.Message{
		ID:        fmt.Sprintf("m%d", f.counter),
		Room:      room,
		Sender:    userID, // the fake has no users table; echo back the id given
		Body:      body,
		CreatedAt: time.Now().UTC(),
	}
	f.byRoom[room] = append(f.byRoom[room], m)
	return m, nil
}

func (f *fakeStore) RecentByRoom(_ context.Context, room string, limit int) ([]store.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.byRoom[room]
	if len(all) > limit {
		all = all[len(all)-limit:]
	}
	out := make([]store.Message, len(all))
	copy(out, all)
	return out, nil
}

// fakeCache is an in-memory chat.Cache for the handler tests. rateAllow controls
// the limiter: -1 allows every call; >=0 allows that many then denies. recent is
// what Recent returns (oldest-first); presence records MarkPresent calls.
type fakeCache struct {
	mu         sync.Mutex
	rateAllow  int
	allowCalls int
	recent     [][]byte
	pushed     [][]byte
	presence   []string
}

func newFakeCache() *fakeCache { return &fakeCache{rateAllow: -1} }

func (f *fakeCache) Allow(_ context.Context, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allowCalls++
	if f.rateAllow < 0 {
		return true, nil
	}
	return f.allowCalls <= f.rateAllow, nil
}

func (f *fakeCache) MarkPresent(_ context.Context, _, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.presence = append(f.presence, user)
	return nil
}

func (f *fakeCache) PushRecent(_ context.Context, _ string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushed = append(f.pushed, payload)
	return nil
}

func (f *fakeCache) Recent(_ context.Context, _ string, _ int) ([][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.recent, nil
}

func (f *fakeCache) presenceContains(user string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, u := range f.presence {
		if u == user {
			return true
		}
	}
	return false
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

func TestWebSocket_InsertFailureIsNotBroadcast(t *testing.T) {
	// If persistence fails, the sender must get an error and the room must NOT
	// see the message. The sender receiving an "error" (not a "message") proves
	// the broadcast never fired, since it would have received its own broadcast.
	base := startTestServerWith(t, failingStore{})

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

	if err := wsjson.Write(ctx, conn, Envelope{Type: TypeChat, Room: "general", Text: "nope"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var got Envelope
	if err := wsjson.Read(ctx, conn, &got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if got.Type != TypeError {
		t.Errorf("got type %q, want %q (a failed insert must not broadcast)", got.Type, TypeError)
	}
}

func TestWebSocket_LoadsHistoryOnJoin(t *testing.T) {
	base := startTestServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Alice connects, sends a message (which gets persisted), confirms the echo,
	// then leaves.
	alice, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name=alice", nil)
	if err != nil {
		t.Fatalf("alice dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if err := wsjson.Write(ctx, alice, Envelope{Type: TypeChat, Room: "general", Text: "first"}); err != nil {
		t.Fatalf("alice write: %v", err)
	}
	var echo Envelope
	if err := wsjson.Read(ctx, alice, &echo); err != nil {
		t.Fatalf("alice read echo: %v", err)
	}
	if echo.Text != "first" {
		t.Fatalf("alice echo = %q, want first", echo.Text)
	}
	alice.Close(websocket.StatusNormalClosure, "")

	// Bob joins afterward and must receive the stored history before any live
	// traffic.
	bob, resp2, err := websocket.Dial(ctx, base+"/ws?room=general&name=bob", nil)
	if err != nil {
		t.Fatalf("bob dial: %v", err)
	}
	if resp2 != nil && resp2.Body != nil {
		_ = resp2.Body.Close()
	}
	defer bob.Close(websocket.StatusNormalClosure, "")

	var hist Envelope
	if err := wsjson.Read(ctx, bob, &hist); err != nil {
		t.Fatalf("bob read history: %v", err)
	}
	if hist.Type != TypeMessage || hist.Text != "first" || hist.Sender != "alice" {
		t.Errorf("bob history = %+v, want a 'message' 'first' from 'alice'", hist)
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

func TestWebSocket_RateLimited(t *testing.T) {
	// The cache allows two messages then denies. The third chat must come back as
	// an error and must not be delivered as a message.
	fc := newFakeCache()
	fc.rateAllow = 2
	base := startTestServerWithCache(t, newFakeStore(), fc)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name=alice", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for i := 0; i < 3; i++ {
		if err := wsjson.Write(ctx, conn, Envelope{Type: TypeChat, Room: "general", Text: "flood"}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	// Two allowed messages arrive via the async loopback; the error is queued
	// directly, so order is not guaranteed. Tally by type over three reads.
	var gotMsgs, gotErrs int
	for i := 0; i < 3; i++ {
		var got Envelope
		if err := wsjson.Read(ctx, conn, &got); err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		switch got.Type {
		case TypeMessage:
			gotMsgs++
		case TypeError:
			gotErrs++
		default:
			t.Fatalf("unexpected envelope type %q", got.Type)
		}
	}
	if gotMsgs != 2 || gotErrs != 1 {
		t.Errorf("got %d messages and %d errors, want 2 and 1", gotMsgs, gotErrs)
	}
}

func TestWebSocket_ServesHistoryFromCache(t *testing.T) {
	// The cache returns two messages; the store is empty. On join the client must
	// receive the cached messages oldest-first, proving the cache-aside read hit
	// Redis and never needed Postgres.
	fc := newFakeCache()
	fc.recent = [][]byte{
		mustJSON(t, Envelope{Type: TypeMessage, Room: "general", Text: "cached-1", Sender: "alice", ID: "c1"}),
		mustJSON(t, Envelope{Type: TypeMessage, Room: "general", Text: "cached-2", Sender: "bob", ID: "c2"}),
	}
	base := startTestServerWithCache(t, newFakeStore(), fc)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name=carol", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	for _, want := range []string{"cached-1", "cached-2"} {
		var got Envelope
		if err := wsjson.Read(ctx, conn, &got); err != nil {
			t.Fatalf("read %q: %v", want, err)
		}
		if got.Type != TypeMessage || got.Text != want {
			t.Errorf("history = %+v, want text %q from cache", got, want)
		}
	}
}

func TestWebSocket_MarksPresenceOnJoin(t *testing.T) {
	fc := newFakeCache()
	base := startTestServerWithCache(t, newFakeStore(), fc)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, base+"/ws?room=general&name=alice", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// MarkPresent runs at the start of readPump, which begins after the handler
	// registers the client, so poll briefly for it.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fc.presenceContains("alice") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("alice was never marked present on join")
}

func mustJSON(t *testing.T, env Envelope) []byte {
	t.Helper()
	b, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
