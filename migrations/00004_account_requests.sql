-- +goose Up
CREATE TABLE IF NOT EXISTS account_requests (
    id UUID PRIMARY KEY,
    requester_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    requested_username TEXT NOT NULL,
    requested_display_name TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_account_requests_status_created_at
    ON account_requests (status, created_at DESC);

-- +goose Down
DROP TABLE IF EXISTS account_requests;
