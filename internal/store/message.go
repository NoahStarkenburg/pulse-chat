package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Message is a stored chat message with the sender's username resolved for
// display. IDs are UUIDs rendered as text so they slot straight into the chat
// wire protocol, which uses string ids.
type Message struct {
	ID        string
	Room      string
	Sender    string
	Body      string
	CreatedAt time.Time
}

// queryTimeout bounds every database call so a slow or stuck query fails fast
// instead of hanging the connection (and goroutine) that issued it.
const queryTimeout = 5 * time.Second

// MessageRepo reads and writes chat messages in Postgres.
type MessageRepo struct {
	pool *pgxpool.Pool
}

// NewMessageRepo returns a repository backed by the given pool.
func NewMessageRepo(pool *pgxpool.Pool) *MessageRepo {
	return &MessageRepo{pool: pool}
}

// Insert stores a message sent by userID in room, creating the room on first
// use, and returns the stored message with its generated id, timestamp, and the
// sender's username. The room upsert and the message insert run in one
// transaction so a failure leaves neither behind.
//
// Every value is passed as a parameter ($1, $2, ...), never concatenated into
// the SQL string, which is what makes SQL injection impossible.
func (r *MessageRepo) Insert(ctx context.Context, room, userID, body string) (Message, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Message{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op once the commit succeeds

	// Get or create the room by name, returning its id either way. ON CONFLICT
	// turns the INSERT into an upsert; the no-op SET is what lets RETURNING fire
	// even when the row already existed.
	var roomID string
	if err := tx.QueryRow(ctx,
		`INSERT INTO rooms (name) VALUES ($1)
		 ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
		 RETURNING id::text`,
		room,
	).Scan(&roomID); err != nil {
		return Message{}, fmt.Errorf("get or create room: %w", err)
	}

	// Insert the message and resolve the sender's username in one round-trip with
	// a data-modifying CTE: the INSERT runs inside WITH, then its returned row is
	// joined to users to pick up the username.
	msg := Message{Room: room, Body: body}
	if err := tx.QueryRow(ctx,
		`WITH inserted AS (
			INSERT INTO messages (room_id, user_id, body)
			VALUES ($1, $2, $3)
			RETURNING id, user_id, created_at
		 )
		 SELECT inserted.id::text, u.username, inserted.created_at
		 FROM inserted JOIN users u ON u.id = inserted.user_id`,
		roomID, userID, body,
	).Scan(&msg.ID, &msg.Sender, &msg.CreatedAt); err != nil {
		return Message{}, fmt.Errorf("insert message: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Message{}, fmt.Errorf("commit: %w", err)
	}
	return msg, nil
}

// RecentByRoom returns up to limit of the most recent messages in room, ordered
// oldest-first so the caller can render them top to bottom.
func (r *MessageRepo) RecentByRoom(ctx context.Context, room string, limit int) ([]Message, error) {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	// ORDER BY created_at DESC + LIMIT lets the (room_id, created_at DESC) index
	// hand back only the most recent rows, already in order, without scanning the
	// whole room or sorting.
	rows, err := r.pool.Query(ctx,
		`SELECT m.id::text, r.name, u.username, m.body, m.created_at
		 FROM messages m
		 JOIN rooms r ON r.id = m.room_id
		 JOIN users u ON u.id = m.user_id
		 WHERE r.name = $1
		 ORDER BY m.created_at DESC
		 LIMIT $2`,
		room, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query recent messages: %w", err)
	}
	defer rows.Close()

	var msgs []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.Room, &m.Sender, &m.Body, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan message: %w", err)
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Fetched newest-first for the index and LIMIT; reverse to oldest-first for
	// natural top-to-bottom display.
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return msgs, nil
}
