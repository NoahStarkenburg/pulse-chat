// Phase 1 test scaffolding for the Hub.
//
// This file is intentionally incomplete. Your job in Phase 1 is to fill
// in tests for the Hub's behavior BEFORE implementing it (or alongside).
// Test-first isn't dogma — it's a way of forcing you to articulate what
// the Hub *should do* before you start coding it.
//
// =============================================================================
// WHAT THE HUB NEEDS TO DO (and what to test)
// =============================================================================
//
// Tests you should write:
//
// 1. NewHub returns a usable Hub with no goroutines required to start.
//
// 2. Run can be cancelled via context. Specifically:
//      ctx, cancel := context.WithCancel(context.Background())
//      h := NewHub(...)
//      done := make(chan struct{})
//      go func() { h.Run(ctx); close(done) }()
//      cancel()
//      select {
//      case <-done:
//      case <-time.After(time.Second):
//          t.Fatal("Run did not return within 1s of cancel")
//      }
//
//    Why this matters: if Run doesn't respect ctx, you have a goroutine
//    leak in production. This test catches it.
//
// 3. Register adds a client to a room. After register, Broadcast to that
//    room should result in the client receiving the message.
//
//    Tip: you can write this test WITHOUT a real WebSocket. Create a
//    Client with a buffered outbound channel and assert messages land
//    there. This is the whole point of the Hub pattern — it's testable
//    in pure-channel terms.
//
// 4. Unregister removes a client. After Unregister, Broadcast must NOT
//    deliver to that client.
//
// 5. Unregister is idempotent. Calling it twice for the same client MUST
//    NOT panic.
//
// 6. Broadcast to room A does NOT reach a client in room B.
//
// 7. Slow client (full outbound channel) does NOT block the Hub.
//    Tip: use a client with an *unbuffered* channel that's never read.
//    Broadcast should still return quickly (use t.Deadline or a timeout).
//
// =============================================================================
// PATTERNS TO USE
// =============================================================================
//
// Use a helper to construct Hubs in tests:
//
//     func newTestHub(t *testing.T) (*Hub, context.CancelFunc) {
//         t.Helper()
//         logger := slog.New(slog.NewTextHandler(io.Discard, nil))
//         h := NewHub(logger)
//         ctx, cancel := context.WithCancel(context.Background())
//         go h.Run(ctx)
//         t.Cleanup(func() { cancel() })
//         return h, cancel
//     }
//
// `t.Cleanup` registers a function to run when the test (or its parent)
// finishes — saves you from `defer` chains in every test.

package chat
