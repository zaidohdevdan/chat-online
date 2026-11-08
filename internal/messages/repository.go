package messages

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"time"

	"chat-online/internal/realtime"
	"chat-online/internal/uploads"
	"chat-online/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrMessageNotFound     = errors.New("message not found")
	ErrForbiddenAuthor     = errors.New("only the message author can perform this action")
	ErrIdempotencyConflict = errors.New("idempotency key reused with different payload")
	ErrInvalidAttachment   = errors.New("invalid or unauthorized attachment")
)

type Message struct {
	ID               string            `json:"id"`
	ConversationID   string            `json:"conversationId"`
	Sender           users.UserSummary `json:"sender"`
	Body             *string           `json:"body"`
	Attachments      []uploads.Upload  `json:"attachments"`
	ReplyToMessageID *string           `json:"replyToMessageId,omitempty"`
	CreatedAt        time.Time         `json:"createdAt"`
	EditedAt         *time.Time        `json:"editedAt,omitempty"`
	DeletedAt        *time.Time        `json:"deletedAt,omitempty"`
	Status           *string           `json:"status,omitempty"`
}

type SendParams struct {
	ConversationID   string
	Sender           users.UserSummary
	ClientMessageID  string
	Body             string
	ReplyToMessageID *string
	AttachmentIDs    []string
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

type msgCursor struct {
	CreatedAt time.Time `json:"c"`
	ID        string    `json:"id"`
}

func encodeMsgCursor(t time.Time, id string) string {
	b, _ := json.Marshal(msgCursor{CreatedAt: t.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeMsgCursor(raw string) (*msgCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var c msgCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(c.ID); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) ListByConversation(
	ctx context.Context,
	conversationID, currentUserID string,
	cursor *string,
	limit int,
) ([]Message, *string, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	var rows pgx.Rows
	var err error
	if cursor != nil && *cursor != "" {
		decoded, decErr := decodeMsgCursor(*cursor)
		if decErr != nil {
			return nil, nil, fmt.Errorf("invalid cursor: %w", decErr)
		}
		rows, err = r.pool.Query(ctx, `
			SELECT
				m.id::text,
				m.conversation_id::text,
				u.id::text,
				u.display_name,
				u.avatar_url,
				CASE WHEN m.deleted_at IS NOT NULL THEN NULL ELSE m.body END,
				m.reply_to_message_id::text,
				m.created_at,
				m.edited_at,
				m.deleted_at
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.conversation_id = $1
			  AND m.created_at > COALESCE(
			    (SELECT cleared_at FROM conversation_members WHERE conversation_id = $1 AND user_id = $5),
			    '1970-01-01T00:00:00Z'::timestamptz
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM message_hidden_for mhf WHERE mhf.message_id = m.id AND mhf.user_id = $5
			  )
			  AND (m.created_at, m.id) < ($2, $3::uuid)
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $4
		`, conversationID, decoded.CreatedAt, decoded.ID, limit+1, currentUserID)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT
				m.id::text,
				m.conversation_id::text,
				u.id::text,
				u.display_name,
				u.avatar_url,
				CASE WHEN m.deleted_at IS NOT NULL THEN NULL ELSE m.body END,
				m.reply_to_message_id::text,
				m.created_at,
				m.edited_at,
				m.deleted_at
			FROM messages m
			JOIN users u ON u.id = m.sender_id
			WHERE m.conversation_id = $1
			  AND m.created_at > COALESCE(
			    (SELECT cleared_at FROM conversation_members WHERE conversation_id = $1 AND user_id = $3),
			    '1970-01-01T00:00:00Z'::timestamptz
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM message_hidden_for mhf WHERE mhf.message_id = m.id AND mhf.user_id = $3
			  )
			ORDER BY m.created_at DESC, m.id DESC
			LIMIT $2
		`, conversationID, limit+1, currentUserID)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	list := make([]Message, 0)
	for rows.Next() {
		var m Message
		if err := rows.Scan(
			&m.ID,
			&m.ConversationID,
			&m.Sender.ID,
			&m.Sender.DisplayName,
			&m.Sender.AvatarURL,
			&m.Body,
			&m.ReplyToMessageID,
			&m.CreatedAt,
			&m.EditedAt,
			&m.DeletedAt,
		); err != nil {
			return nil, nil, err
		}
		m.Attachments = make([]uploads.Upload, 0)
		list = append(list, m)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var nextCursor *string
	if len(list) > limit {
		last := list[limit-1]
		encoded := encodeMsgCursor(last.CreatedAt, last.ID)
		nextCursor = &encoded
		list = list[:limit]
	}

	for i := range list {
		if list[i].DeletedAt == nil {
			atts, err := r.getAttachmentsForMessage(ctx, list[i].ID)
			if err != nil {
				return nil, nil, err
			}
			list[i].Attachments = atts
		}
		if list[i].Sender.ID == currentUserID {
			status, err := r.computeAggregatedStatus(ctx, conversationID, currentUserID, list[i].CreatedAt)
			if err == nil {
				list[i].Status = &status
			}
		}
	}

	return list, nextCursor, nil
}

func (r *Repository) CreateOrGetIdempotent(
	ctx context.Context,
	p SendParams,
) (*Message, *realtime.Envelope, bool, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// Check existing message by (sender_id, conversation_id, client_message_id)
	existing, err := r.findByClientIDTx(ctx, tx, p.Sender.ID, p.ConversationID, p.ClientMessageID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, false, err
	}
	if existing != nil {
		if !matchesIdempotentPayload(existing, p) {
			return nil, nil, false, ErrIdempotencyConflict
		}
		status, sErr := r.computeAggregatedStatus(ctx, p.ConversationID, p.Sender.ID, existing.CreatedAt)
		if sErr == nil {
			existing.Status = &status
		}
		return existing, nil, false, nil
	}

	var dbReplyToID *string
	if p.ReplyToMessageID != nil && *p.ReplyToMessageID != "" {
		if _, parseErr := uuid.Parse(*p.ReplyToMessageID); parseErr != nil {
			return nil, nil, false, ErrMessageNotFound
		}
		var replyExists bool
		err = tx.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM messages WHERE id = $1 AND conversation_id = $2
			)
		`, *p.ReplyToMessageID, p.ConversationID).Scan(&replyExists)
		if err == nil && replyExists {
			dbReplyToID = p.ReplyToMessageID
		}
	}

	attachments := make([]uploads.Upload, 0, len(p.AttachmentIDs))
	for _, attID := range p.AttachmentIDs {
		if _, parseErr := uuid.Parse(attID); parseErr != nil {
			return nil, nil, false, ErrInvalidAttachment
		}
		var u uploads.Upload
		var uploaderID string
		var existingMsgID *string
		err = tx.QueryRow(ctx, `
			SELECT id::text, uploader_id::text, message_id::text, file_name, mime_type, size_bytes, created_at
			FROM attachments
			WHERE id = $1
			FOR UPDATE
		`, attID).Scan(&u.ID, &uploaderID, &existingMsgID, &u.FileName, &u.MimeType, &u.Size, &u.CreatedAt)
		if err != nil {
			return nil, nil, false, ErrInvalidAttachment
		}
		if uploaderID != p.Sender.ID || existingMsgID != nil {
			return nil, nil, false, ErrInvalidAttachment
		}
		attachments = append(attachments, u)
	}

	msgUUID, err := uuid.NewV7()
	if err != nil {
		return nil, nil, false, err
	}
	now := time.Now().UTC()
	bodyCopy := p.Body

	_, err = tx.Exec(ctx, `
		INSERT INTO messages (
			id, conversation_id, sender_id, client_message_id, body, reply_to_message_id, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, msgUUID, p.ConversationID, p.Sender.ID, p.ClientMessageID, bodyCopy, dbReplyToID, now)
	if err != nil {
		return nil, nil, false, err
	}

	// Un-hide conversation for all members when a new message arrives
	_, _ = tx.Exec(ctx, `
		UPDATE conversation_members SET hidden_at = NULL WHERE conversation_id = $1
	`, p.ConversationID)

	for _, att := range attachments {
		_, err = tx.Exec(ctx, `
			UPDATE attachments
			SET message_id = $1, conversation_id = $2
			WHERE id = $3
		`, msgUUID, p.ConversationID, att.ID)
		if err != nil {
			return nil, nil, false, err
		}
	}

	_, err = tx.Exec(ctx, `
		UPDATE conversations SET updated_at = $2 WHERE id = $1
	`, p.ConversationID, now)
	if err != nil {
		return nil, nil, false, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE conversation_members
		SET last_read_message_id = $3,
		    last_delivered_message_id = $3
		WHERE conversation_id = $1 AND user_id = $2
	`, p.ConversationID, p.Sender.ID, msgUUID)
	if err != nil {
		return nil, nil, false, err
	}

	sentStatus := "sent"
	canonical := &Message{
		ID:               msgUUID.String(),
		ConversationID:   p.ConversationID,
		Sender:           p.Sender,
		Body:             &bodyCopy,
		Attachments:      attachments,
		ReplyToMessageID: p.ReplyToMessageID,
		CreatedAt:        now,
		Status:           &sentStatus,
	}

	// Event payload for other members omits sender-only status or keeps canonical representation
	eventMsg := *canonical
	env, err := realtime.AppendDurableEvent(ctx, tx, p.ConversationID, "message.created", map[string]any{
		"message": eventMsg,
	})
	if err != nil {
		return nil, nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, false, err
	}

	return canonical, env, true, nil
}

func (r *Repository) EditMessage(ctx context.Context, messageID, userID, newBody string) (*Message, *realtime.Envelope, error) {
	if _, err := uuid.Parse(messageID); err != nil {
		return nil, nil, ErrMessageNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var m Message
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT
			m.id::text,
			m.conversation_id::text,
			u.id::text,
			u.display_name,
			u.avatar_url,
			m.reply_to_message_id::text,
			m.created_at,
			m.deleted_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.id = $1
		FOR UPDATE OF m
	`, messageID).Scan(
		&m.ID,
		&m.ConversationID,
		&m.Sender.ID,
		&m.Sender.DisplayName,
		&m.Sender.AvatarURL,
		&m.ReplyToMessageID,
		&m.CreatedAt,
		&deletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	// Verify user is a member of the conversation first (return 404 if not member)
	var isMember bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2)
	`, m.ConversationID, userID).Scan(&isMember); err != nil || !isMember {
		return nil, nil, ErrMessageNotFound
	}

	if m.Sender.ID != userID {
		return nil, nil, ErrForbiddenAuthor
	}
	if deletedAt != nil {
		return nil, nil, ErrMessageNotFound
	}

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE messages
		SET body = $2, edited_at = $3
		WHERE id = $1
	`, messageID, newBody, now)
	if err != nil {
		return nil, nil, err
	}

	m.Body = &newBody
	m.EditedAt = &now
	atts, err := r.getAttachmentsForMessageTx(ctx, tx, messageID)
	if err != nil {
		return nil, nil, err
	}
	m.Attachments = atts

	env, err := realtime.AppendDurableEvent(ctx, tx, m.ConversationID, "message.updated", map[string]any{
		"message": m,
	})
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	status, sErr := r.computeAggregatedStatus(ctx, m.ConversationID, userID, m.CreatedAt)
	if sErr == nil {
		m.Status = &status
	}

	return &m, env, nil
}

func (r *Repository) DeleteMessage(ctx context.Context, messageID, userID, scope, conversationIDHint string) (*realtime.Envelope, error) {
	if _, err := uuid.Parse(messageID); err != nil {
		return nil, ErrMessageNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var convID, senderID string
	var deletedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT conversation_id::text, sender_id::text, deleted_at
		FROM messages
		WHERE id = $1
		FOR UPDATE
	`, messageID).Scan(&convID, &senderID, &deletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// S2-fix: without the message row we cannot verify authorship,
		// so refuse the operation for all scopes.  The "scope=me" soft-hide
		// also requires an existing row (message_hidden_for FK).
		return nil, ErrMessageNotFound
	}
	if err != nil {
		return nil, err
	}

	var isMember bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM conversation_members WHERE conversation_id = $1 AND user_id = $2)
	`, convID, userID).Scan(&isMember); err != nil || !isMember {
		return nil, ErrMessageNotFound
	}

	if scope == "me" {
		_, err = tx.Exec(ctx, `
			INSERT INTO message_hidden_for (message_id, user_id, hidden_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (message_id, user_id) DO NOTHING
		`, messageID, userID)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if senderID != userID {
		return nil, ErrForbiddenAuthor
	}

	// Hard delete attachments and message from the database
	_, err = tx.Exec(ctx, `
		DELETE FROM attachments WHERE message_id = $1
	`, messageID)
	if err != nil {
		return nil, err
	}

	_, err = tx.Exec(ctx, `
		DELETE FROM messages WHERE id = $1
	`, messageID)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	env, err := realtime.AppendDurableEvent(ctx, tx, convID, "message.deleted", map[string]any{
		"messageId":  messageID,
		"deletedAt":  now.Format(time.RFC3339Nano),
		"hardDelete": true,
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return env, nil
}

func (r *Repository) RecordDeliveryAck(ctx context.Context, userID, messageID string) (*realtime.Envelope, error) {
	if _, err := uuid.Parse(messageID); err != nil {
		return nil, ErrMessageNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var convID, senderID string
	err = tx.QueryRow(ctx, `
		SELECT conversation_id::text, sender_id::text
		FROM messages
		WHERE id = $1
		FOR UPDATE
	`, messageID).Scan(&convID, &senderID)
	if err != nil {
		return nil, err
	}
	if senderID == userID {
		return nil, nil
	}

	tag, err := tx.Exec(ctx, `
		UPDATE conversation_members
		SET last_delivered_message_id = $3
		WHERE conversation_id = $1 AND user_id = $2
	`, convID, userID, messageID)
	if err != nil || tag.RowsAffected() == 0 {
		return nil, err
	}

	_, _ = tx.Exec(ctx, `
		INSERT INTO message_deliveries (message_id, user_id, delivered_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (message_id, user_id) DO NOTHING
	`, messageID, userID)

	var totalRecipients, deliveredRecipients int
	_ = tx.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM conversation_members WHERE conversation_id = $1 AND user_id <> $2),
			(SELECT COUNT(*) FROM message_deliveries WHERE message_id = $3)
	`, convID, senderID, messageID).Scan(&totalRecipients, &deliveredRecipients)

	// Signal ephemeral queue: once all recipients have received the message, purge it from PostgreSQL
	if totalRecipients > 0 && deliveredRecipients >= totalRecipients {
		_, _ = tx.Exec(ctx, `DELETE FROM attachments WHERE message_id = $1`, messageID)
		_, _ = tx.Exec(ctx, `DELETE FROM messages WHERE id = $1`, messageID)
		_, _ = tx.Exec(ctx, `
			DELETE FROM events
			WHERE conversation_id = $1
			  AND type = 'message.created'
			  AND payload->'message'->>'id' = $2
		`, convID, messageID)
	}

	now := time.Now().UTC()
	env, err := realtime.AppendDurableEvent(ctx, tx, convID, "message.delivered", map[string]any{
		"messageId":   messageID,
		"userId":      userID,
		"deliveredAt": now.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return env, nil
}

func (r *Repository) findByClientIDTx(
	ctx context.Context,
	tx pgx.Tx,
	senderID, conversationID, clientMessageID string,
) (*Message, error) {
	var m Message
	err := tx.QueryRow(ctx, `
		SELECT
			m.id::text,
			m.conversation_id::text,
			u.id::text,
			u.display_name,
			u.avatar_url,
			m.body,
			m.reply_to_message_id::text,
			m.created_at,
			m.edited_at,
			m.deleted_at
		FROM messages m
		JOIN users u ON u.id = m.sender_id
		WHERE m.sender_id = $1 AND m.conversation_id = $2 AND m.client_message_id = $3
	`, senderID, conversationID, clientMessageID).Scan(
		&m.ID,
		&m.ConversationID,
		&m.Sender.ID,
		&m.Sender.DisplayName,
		&m.Sender.AvatarURL,
		&m.Body,
		&m.ReplyToMessageID,
		&m.CreatedAt,
		&m.EditedAt,
		&m.DeletedAt,
	)
	if err != nil {
		return nil, err
	}

	atts, err := r.getAttachmentsForMessageTx(ctx, tx, m.ID)
	if err != nil {
		return nil, err
	}
	m.Attachments = atts
	return &m, nil
}

func matchesIdempotentPayload(existing *Message, p SendParams) bool {
	existingBody := ""
	if existing.Body != nil {
		existingBody = *existing.Body
	}
	if existingBody != p.Body {
		return false
	}
	existingReply := ""
	if existing.ReplyToMessageID != nil {
		existingReply = *existing.ReplyToMessageID
	}
	paramReply := ""
	if p.ReplyToMessageID != nil {
		paramReply = *p.ReplyToMessageID
	}
	if existingReply != paramReply {
		return false
	}

	existingAttIDs := make([]string, 0, len(existing.Attachments))
	for _, a := range existing.Attachments {
		existingAttIDs = append(existingAttIDs, a.ID)
	}
	paramAttIDs := make([]string, 0, len(p.AttachmentIDs))
	paramAttIDs = append(paramAttIDs, p.AttachmentIDs...)
	sort.Strings(existingAttIDs)
	sort.Strings(paramAttIDs)
	return reflect.DeepEqual(existingAttIDs, paramAttIDs)
}

func (r *Repository) getAttachmentsForMessage(ctx context.Context, messageID string) ([]uploads.Upload, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, file_name, mime_type, size_bytes, created_at
		FROM attachments
		WHERE message_id = $1
		ORDER BY created_at ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]uploads.Upload, 0)
	for rows.Next() {
		var u uploads.Upload
		if err := rows.Scan(&u.ID, &u.FileName, &u.MimeType, &u.Size, &u.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

func (r *Repository) getAttachmentsForMessageTx(ctx context.Context, tx pgx.Tx, messageID string) ([]uploads.Upload, error) {
	rows, err := tx.Query(ctx, `
		SELECT id::text, file_name, mime_type, size_bytes, created_at
		FROM attachments
		WHERE message_id = $1
		ORDER BY created_at ASC
	`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]uploads.Upload, 0)
	for rows.Next() {
		var u uploads.Upload
		if err := rows.Scan(&u.ID, &u.FileName, &u.MimeType, &u.Size, &u.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, u)
	}
	return list, rows.Err()
}

func (r *Repository) computeAggregatedStatus(
	ctx context.Context,
	conversationID, senderID string,
	msgCreatedAt time.Time,
) (string, error) {
	var totalPeers, deliveredPeers, readPeers int
	err := r.pool.QueryRow(ctx, `
		SELECT
			COUNT(*),
			COUNT(*) FILTER (
				WHERE COALESCE(md.created_at, mr.created_at) >= $3
			),
			COUNT(*) FILTER (
				WHERE mr.created_at >= $3
			)
		FROM conversation_members cm
		LEFT JOIN messages md ON md.id = cm.last_delivered_message_id
		LEFT JOIN messages mr ON mr.id = cm.last_read_message_id
		WHERE cm.conversation_id = $1 AND cm.user_id != $2
	`, conversationID, senderID, msgCreatedAt).Scan(&totalPeers, &deliveredPeers, &readPeers)
	if err != nil || totalPeers == 0 {
		return "sent", err
	}
	if readPeers == totalPeers {
		return "read", nil
	}
	if deliveredPeers == totalPeers {
		return "delivered", nil
	}
	return "sent", nil
}
