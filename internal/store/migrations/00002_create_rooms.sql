-- migrate:up
-- Rooms are a first-class entity so messages can reference them by a stable id
-- and so later phases can attach room metadata (membership, visibility, etc.).
-- Rooms are created on first use (the app upserts by name).
CREATE TABLE rooms (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT        NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- migrate:down
DROP TABLE rooms;
