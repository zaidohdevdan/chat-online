package realtime

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Envelope struct {
	ID             string          `json:"id"`
	Type           string          `json:"type"`
	OccurredAt     time.Time       `json:"occurredAt"`
	ConversationID *string         `json:"conversationId,omitempty"`
	Sequence       *int64          `json:"sequence"`
	Data           json.RawMessage `json:"data"`
}

type Publisher interface {
	PublishConversationEvent(ctx context.Context, env Envelope)
	PublishToUsers(userIDs []string, env Envelope)
}

// AppendDurableEvent locks the conversation row in the current transaction,
// increments last_sequence, inserts the event into the transactional outbox table (`events`),
// and returns the Envelope ready to be dispatched after commit.
func AppendDurableEvent(ctx context.Context, tx pgx.Tx, conversationID, eventType string, data any) (*Envelope, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}

	var nextSeq int64
	err = tx.QueryRow(ctx, `
		UPDATE conversations
		SET last_sequence = last_sequence + 1
		WHERE id = $1
		RETURNING last_sequence
	`, conversationID).Scan(&nextSeq)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO events (id, conversation_id, sequence, type, payload, occurred_at, published_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
	`, eventID, conversationID, nextSeq, eventType, payloadBytes, now)
	if err != nil {
		return nil, err
	}

	// Notify other instances via PostgreSQL LISTEN/NOTIFY (docs/events.md section 8).
	_, err = tx.Exec(ctx, `SELECT pg_notify('chat_events', $1)`, eventID.String())
	if err != nil {
		return nil, err
	}

	convCopy := conversationID
	seqCopy := nextSeq
	return &Envelope{
		ID:             eventID.String(),
		Type:           eventType,
		OccurredAt:     now,
		ConversationID: &convCopy,
		Sequence:       &seqCopy,
		Data:           payloadBytes,
	}, nil
}

func NewEphemeralEnvelope(conversationID *string, eventType string, data any) (*Envelope, error) {
	eventID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	payloadBytes, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return &Envelope{
		ID:             eventID.String(),
		Type:           eventType,
		OccurredAt:     time.Now().UTC(),
		ConversationID: conversationID,
		Sequence:       nil,
		Data:           payloadBytes,
	}, nil
}

