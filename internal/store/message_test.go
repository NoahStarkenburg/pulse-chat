package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/NoahStarkenburg/pulse-chat/internal/store/migrations"
)

// testPool connects to the database named by PULSE_POSTGRES_URL and applies the
// schema. It skips the test when no database is configured, so the suite stays
// green in CI (which has no Postgres) and runs locally when the DB is up.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PULSE_POSTGRES_URL")
	if dsn == "" {
		t.Skip("PULSE_POSTGRES_URL not set; skipping database test")
	}
	ctx := context.Background()
	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := migrations.Up(ctx, pool); err != nil {
		pool.Close()
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func randomSuffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestMessageRepo_InsertAndRecent(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	// A message needs a real user (foreign key) and a room. Use unique names and
	// clean up afterward; deleting the user cascades its messages.
	suffix := randomSuffix(t)
	username := "tester_" + suffix
	room := "room_" + suffix

	var userID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, 'x') RETURNING id::text`,
		username,
	).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
		_, _ = pool.Exec(ctx, `DELETE FROM rooms WHERE name = $1`, room)
	})

	repo := NewMessageRepo(pool)

	first, err := repo.Insert(ctx, room, userID, "hello")
	if err != nil {
		t.Fatalf("insert first: %v", err)
	}
	if first.ID == "" {
		t.Error("inserted message has no id")
	}
	if first.Sender != username {
		t.Errorf("sender = %q, want %q", first.Sender, username)
	}

	if _, err := repo.Insert(ctx, room, userID, "world"); err != nil {
		t.Fatalf("insert second: %v", err)
	}

	msgs, err := repo.RecentByRoom(ctx, room, 50)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("got %d messages, want 2", len(msgs))
	}
	// Oldest-first ordering.
	if msgs[0].Body != "hello" || msgs[1].Body != "world" {
		t.Errorf("bodies = %q, %q; want hello, world", msgs[0].Body, msgs[1].Body)
	}
	if msgs[0].Room != room {
		t.Errorf("room = %q, want %q", msgs[0].Room, room)
	}
}
