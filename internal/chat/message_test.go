// Wire-format contract tests. They pin the JSON shape the browser frontend
// depends on, so a rename like "text" to "content" fails the build instead of
// silently breaking the UI.
package chat

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEnvelope_OmitsEmptyServerFields(t *testing.T) {
	// A client-to-server chat carries only type/room/text; the server-set fields
	// must be omitted when empty.
	got, err := json.Marshal(Envelope{Type: TypeChat, Room: "general", Text: "hi"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"chat","room":"general","text":"hi"}`
	if string(got) != want {
		t.Errorf("JSON shape drifted:\n got: %s\nwant: %s", got, want)
	}
}

func TestEnvelope_IncludesServerFieldsWhenSet(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	got, err := json.Marshal(Envelope{
		Type:      TypeMessage,
		Room:      "general",
		Text:      "hi",
		ID:        "abc",
		Sender:    "alice",
		Timestamp: &ts,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"type":"message","room":"general","text":"hi","id":"abc","sender":"alice","timestamp":"2026-01-02T03:04:05Z"}`
	if string(got) != want {
		t.Errorf("JSON shape drifted:\n got: %s\nwant: %s", got, want)
	}
}

func TestEnvelope_Roundtrip(t *testing.T) {
	ts := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)
	in := Envelope{
		Type:      TypeMessage,
		Room:      "r",
		Text:      "t",
		ID:        "1",
		Sender:    "s",
		Timestamp: &ts,
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out Envelope
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	// Use Time.Equal for the timestamp; == on time.Time is unreliable because of
	// the monotonic clock reading and location.
	if out.Type != in.Type || out.Room != in.Room || out.Text != in.Text ||
		out.ID != in.ID || out.Sender != in.Sender {
		t.Errorf("roundtrip mismatch:\n in: %+v\nout: %+v", in, out)
	}
	if out.Timestamp == nil || !out.Timestamp.Equal(*in.Timestamp) {
		t.Errorf("timestamp roundtrip mismatch: in=%v out=%v", in.Timestamp, out.Timestamp)
	}
}

func TestEnvelope_IgnoresUnknownFields(t *testing.T) {
	// Unknown fields are ignored, keeping the protocol forward-compatible.
	raw := `{"type":"chat","room":"r","text":"t","emoji":"fire","future":123}`
	var env Envelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal with unknown fields: %v", err)
	}
	if env.Type != TypeChat || env.Text != "t" {
		t.Errorf("known fields not parsed correctly: %+v", env)
	}
}
