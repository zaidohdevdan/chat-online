package realtime

import (
	"context"
	"log/slog"
	"time"

	"chat-online/internal/metrics"

	"github.com/jackc/pgx/v5/pgxpool"
)

// StartOutboxWorker runs a background loop that:
// 1. Publishes any pending durable events (`published_at IS NULL`) from the `events` outbox table.
// 2. Updates outbox queue depth metrics.
// 3. Enforces the 7-day retention policy on the `events` table per docs/architecture.md section 6.
func (h *Hub) StartOutboxWorker(ctx context.Context) {
	workerCtx, cancel := context.WithCancel(ctx)
	h.mu.Lock()
	h.workerCancel = cancel
	h.mu.Unlock()

	publishTicker := time.NewTicker(1 * time.Second)
	retentionTicker := time.NewTicker(1 * time.Hour)

	h.workerWg.Add(2)
	go func() {
		defer h.workerWg.Done()
		defer publishTicker.Stop()
		defer retentionTicker.Stop()

		h.cleanupOldEvents(workerCtx)

		for {
			select {
			case <-workerCtx.Done():
				return
			case <-publishTicker.C:
				h.flushUnpublishedEvents(workerCtx)
			case <-retentionTicker.C:
				h.cleanupOldEvents(workerCtx)
			}
		}
	}()

	go func() {
		defer h.workerWg.Done()
		h.listenPostgresNotifications(workerCtx)
	}()
}

func (h *Hub) listenPostgresNotifications(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		conn, err := h.pool.Acquire(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		_, err = conn.Exec(ctx, `LISTEN chat_events`)
		if err != nil {
			conn.Release()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
				continue
			}
		}

		for {
			notification, waitErr := conn.Conn().WaitForNotification(ctx)
			if waitErr != nil {
				conn.Release()
				break
			}
			if notification != nil && notification.Payload != "" {
				h.publishEventByIDIfRemote(ctx, notification.Payload)
			}
		}
	}
}

func (h *Hub) publishEventByIDIfRemote(ctx context.Context, eventID string) {
	if h.wasLocallyPublished(eventID) {
		return
	}
	var env Envelope
	var convID string
	var seq int64
	err := h.pool.QueryRow(ctx, `
		SELECT id::text, conversation_id::text, type, occurred_at, sequence, payload
		FROM events
		WHERE id = $1
	`, eventID).Scan(&env.ID, &convID, &env.Type, &env.OccurredAt, &seq, &env.Data)
	if err != nil {
		return
	}
	env.ConversationID = &convID
	env.Sequence = &seq
	h.PublishConversationEvent(ctx, env)
}

func (h *Hub) flushUnpublishedEvents(ctx context.Context) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	rows, err := tx.Query(ctx, `
		SELECT id::text, conversation_id::text, type, occurred_at, sequence, payload
		FROM events
		WHERE published_at IS NULL
		ORDER BY sequence ASC
		LIMIT 100
		FOR UPDATE SKIP LOCKED
	`)
	if err != nil {
		return
	}

	var pending []Envelope
	var ids []string
	for rows.Next() {
		var env Envelope
		var convID string
		var seq int64
		if sErr := rows.Scan(&env.ID, &convID, &env.Type, &env.OccurredAt, &seq, &env.Data); sErr != nil {
			rows.Close()
			return
		}
		env.ConversationID = &convID
		env.Sequence = &seq
		pending = append(pending, env)
		ids = append(ids, env.ID)
	}
	rows.Close()

	if len(ids) > 0 {
		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			UPDATE events
			SET published_at = $2
			WHERE id::text = ANY($1)
		`, ids, now)
		if err != nil {
			return
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return
	}

	metrics.Default.SetOutboxPending(int64(len(pending)))
	for _, env := range pending {
		h.PublishConversationEvent(ctx, env)
	}
}

func (h *Hub) cleanupOldEvents(ctx context.Context) {
	_, err := h.pool.Exec(ctx, `
		DELETE FROM events
		WHERE occurred_at < NOW() - INTERVAL '7 days'
	`)
	if err != nil {
		slog.Warn("failed to clean up expired outbox events", "err", err)
	}
}

func CountPendingOutbox(ctx context.Context, pool *pgxpool.Pool) int64 {
	var count int64
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM events WHERE published_at IS NULL`).Scan(&count)
	return count
}

