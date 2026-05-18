// Client tests. Trickier than Hub tests because Client owns a
// *websocket.Conn — you need a real (in-memory) WebSocket to exercise
// it. The standard pattern uses httptest.NewServer + the coder/websocket
// dialer pointed at it.
//
// =============================================================================
// TESTS TO WRITE
// =============================================================================
//
// 1. End-to-end one-message round-trip via httptest:
//
//      srv := httptest.NewServer(chat.NewWebSocketHandler(hub, logger))
//      defer srv.Close()
//      conn, _, _ := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/?room=test&name=a", nil)
//      defer conn.Close(websocket.StatusNormalClosure, "")
//      conn.Write(ctx, websocket.MessageText, []byte(`{"type":"chat","room":"test","text":"hi"}`))
//      ... another connection ... assert the second one receives the message.
//
// 2. readPump returns when the conn is closed by the peer.
//
// 3. writePump returns when its outbound channel is closed.
//
// 4. Server-stamped fields (id/sender/timestamp) are populated on
//    broadcasts even when the client didn't send them.
//
// 5. Ping/pong keeps the connection alive across a 60s window. (Probably
//    too slow for unit tests — make this an integration-style test you
//    run on demand, not in CI.)
//
// =============================================================================
// INTEGRATION TESTS LATER
// =============================================================================
//
// In Phase 2+, when Postgres / Redis / Rabbit enter the picture, use
// testcontainers-go to spin up real containers in your tests. Mocks for
// these services have a long, sad history of passing tests that hide
// real bugs. testcontainers is slower (~5s per test setup) but worth
// every second.

package chat
