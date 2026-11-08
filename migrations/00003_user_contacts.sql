-- +goose Up
CREATE TABLE IF NOT EXISTS user_contacts (
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    contact_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (owner_id, contact_id),
    CONSTRAINT user_contacts_no_self CHECK (owner_id <> contact_id)
);

CREATE INDEX IF NOT EXISTS idx_user_contacts_owner_id ON user_contacts(owner_id);

-- Vincula automaticamente pares de usuários que já compartilham conversas existentes
INSERT INTO user_contacts (owner_id, contact_id)
SELECT DISTINCT cm1.user_id, cm2.user_id
FROM conversation_members cm1
JOIN conversation_members cm2 ON cm1.conversation_id = cm2.conversation_id
WHERE cm1.user_id <> cm2.user_id
ON CONFLICT (owner_id, contact_id) DO NOTHING;

-- +goose Down
DROP TABLE IF EXISTS user_contacts;

