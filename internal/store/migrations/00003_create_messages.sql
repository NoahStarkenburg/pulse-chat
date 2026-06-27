-- migrate:up
-- Messages are the durable chat history. Each one references the room it was
-- sent in and the user who sent it. ON DELETE CASCADE keeps the table consistent
-- if a room or user is ever removed. body is bounded so a single message cannot
-- be empty or unbounded in size.
CREATE TABLE messages (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id    UUID        NOT NULL REFERENCES rooms(id) ON DELETE CASCADE,
    user_id    UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    body       TEXT        NOT NULL CHECK (char_length(body) BETWEEN 1 AND 4000),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The hot read is "the most recent N messages in a room". Indexing room_id plus
-- created_at descending lets Postgres serve that straight from the index, in
-- order, without scanning the table or sorting.
CREATE INDEX idx_messages_room_recent ON messages (room_id, created_at DESC);

-- migrate:down
DROP TABLE messages;
