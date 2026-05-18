// Message protocol tests. These verify the JSON wire format hasn't
// drifted. Tests like this are cheap insurance — when you accidentally
// rename a field from "text" to "Text" and the frontend silently breaks,
// this test catches it before the user does.
//
// =============================================================================
// TESTS TO WRITE
// =============================================================================
//
//  1. Roundtrip: marshal an Envelope, unmarshal it, fields match.
//  2. JSON shape: marshal a known Envelope, assert the JSON bytes match
//     the documented shape exactly. Use a string literal:
//
//         want := `{"type":"chat","room":"general","text":"hi"}`
//
//     and json.Marshal then string-compare.
//  3. Server-set fields (id/sender/timestamp) are OMITTED when empty
//     (we used `omitempty` for this).
//  4. Unknown JSON fields are ignored (json package default behavior).
//
// =============================================================================
// WHY CONTRACT TESTS MATTER
// =============================================================================
//
// The browser frontend will rely on these field names. If you rename
// "text" → "content" in Go, the frontend silently shows empty bubbles.
// This test makes the contract reviewable in code: changing the JSON
// shape requires changing this test, which forces conscious thought.

package chat
