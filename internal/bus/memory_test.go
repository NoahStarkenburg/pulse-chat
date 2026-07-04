package bus

import (
	"context"
	"testing"
	"time"
)

func recvWithin(t *testing.T, s Subscription, d time.Duration) ([]byte, bool) {
	t.Helper()
	select {
	case msg, ok := <-s.Messages():
		return msg, ok
	case <-time.After(d):
		t.Fatal("timed out waiting for a message")
		return nil, false
	}
}

func TestMemory_PublishReachesAllSubscribers(t *testing.T) {
	m := NewMemory()
	defer m.Close()
	ctx := context.Background()

	a, _ := m.Subscribe(ctx, "room:general")
	b, _ := m.Subscribe(ctx, "room:general")

	if err := m.Publish(ctx, "room:general", []byte("hi")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	for name, s := range map[string]Subscription{"a": a, "b": b} {
		msg, ok := recvWithin(t, s, time.Second)
		if !ok || string(msg) != "hi" {
			t.Errorf("%s: got (%q, ok=%v), want hi", name, msg, ok)
		}
	}
}

func TestMemory_OnlyChannelSubscribersReceive(t *testing.T) {
	m := NewMemory()
	defer m.Close()
	ctx := context.Background()

	a, _ := m.Subscribe(ctx, "room:a")
	if _, err := m.Subscribe(ctx, "room:b"); err != nil {
		t.Fatalf("subscribe b: %v", err)
	}

	if err := m.Publish(ctx, "room:b", []byte("for b")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case msg := <-a.Messages():
		t.Fatalf("room:a subscriber wrongly received %q", msg)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestMemory_CloseEndsSubscription(t *testing.T) {
	m := NewMemory()
	defer m.Close()
	s, _ := m.Subscribe(context.Background(), "room:general")
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, ok := <-s.Messages(); ok {
		t.Fatal("expected Messages() to be closed after Close")
	}
}
