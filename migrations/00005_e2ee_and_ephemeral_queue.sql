-- +goose Up
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS public_key TEXT;

ALTER TABLE conversation_members
    ADD COLUMN IF NOT EXISTS hidden_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS cleared_at TIMESTAMPTZ;

ALTER TABLE messages
    DROP CONSTRAINT IF EXISTS messages_reply_to_message_id_fkey,
    ADD CONSTRAINT messages_reply_to_message_id_fkey
        FOREIGN KEY (reply_to_message_id) REFERENCES messages(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS message_deliveries (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    delivered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (message_id, user_id)
);

CREATE TABLE IF NOT EXISTS message_hidden_for (
    message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hidden_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (message_id, user_id)
);

-- +goose Down
DROP TABLE IF EXISTS message_hidden_for;
DROP TABLE IF EXISTS message_deliveries;
ALTER TABLE conversation_members
    DROP COLUMN IF EXISTS cleared_at,
    DROP COLUMN IF EXISTS hidden_at;
ALTER TABLE users
    DROP COLUMN IF EXISTS public_key;
