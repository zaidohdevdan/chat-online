-- +goose Up
CREATE INDEX IF NOT EXISTS idx_events_occurred_at ON events(occurred_at);
CREATE INDEX IF NOT EXISTS idx_attachments_conversation_id ON attachments(conversation_id) WHERE message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_messages_reply_to ON messages(reply_to_message_id) WHERE reply_to_message_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_messages_reply_to;
DROP INDEX IF EXISTS idx_attachments_conversation_id;
DROP INDEX IF EXISTS idx_events_occurred_at;

