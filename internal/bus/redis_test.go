package bus

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestRedisPubSub_Integration runs against a real Redis named by PULSE_REDIS_URL.
// It skips when none is configured, so the suite stays green in CI (which has no
// Redis) and runs locally when the container is up.
func TestRedisPubSub_Integration(t *testing.T) {
	url := os.Getenv("PULSE_REDIS_URL")
	if url == "" {
		t.Skip("PULSE_REDIS_URL not set; skipping Redis integration test")
	}
	ctx := context.Background()

	b, err := NewRedisPubSub(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer b.Close()

	sub, err := b.Subscribe(ctx, "test:phase3")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer sub.Close()

	if err := b.Publish(ctx, "test:phase3", []byte("hello redis")); err != nil {
		t.Fatalf("publish: %v", err)
	}

	select {
	case msg, ok := <-sub.Messages():
		if !ok || string(msg) != "hello redis" {
			t.Fatalf("got (%q, ok=%v), want hello redis", msg, ok)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for published message")
	}
}
