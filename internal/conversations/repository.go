package conversations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"chat-online/internal/realtime"
	"chat-online/internal/uploads"
	"chat-online/internal/users"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrConversationNotFound = errors.New("conversation not found")
	ErrMessageNotFound      = errors.New("message not found")
)

type MessagePreview struct {
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

type Conversation struct {
	ID                string              `json:"id"`
	Type              string              `json:"type"`
	Title             *string             `json:"title,omitempty"`
	Members           []users.UserSummary `json:"members"`
	LastMessage       *MessagePreview     `json:"lastMessage,omitempty"`
	UnreadCount       int                 `json:"unreadCount"`
	LastReadMessageID *string             `json:"lastReadMessageId,omitempty"`
	UpdatedAt         time.Time           `json:"updatedAt"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) IsMember(ctx context.Context, conversationID, userID string) (bool, error) {
	if _, err := uuid.Parse(conversationID); err != nil {
		return false, nil
	}
	var exists bool
	err := r.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM conversation_members
			WHERE conversation_id = $1 AND user_id = $2
		)
	`, conversationID, userID).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

func makeDirectKey(userA, userB string) string {
	pair := []string{strings.ToLower(userA), strings.ToLower(userB)}
	sort.Strings(pair)
	return pair[0] + ":" + pair[1]
}

func (r *Repository) FindDirectByUsers(ctx context.Context, userA, userB string) (string, bool, error) {
	key := makeDirectKey(userA, userB)
	var id string
	err := r.pool.QueryRow(ctx, `
		SELECT id::text FROM conversations WHERE direct_key = $1
	`, key).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func (r *Repository) Create(
	ctx context.Context,
	convType string,
	title *string,
	memberIDs []string,
) (string, *realtime.Envelope, bool, error) {
	if convType == "direct" && len(memberIDs) == 2 {
		existingID, found, err := r.FindDirectByUsers(ctx, memberIDs[0], memberIDs[1])
		if err != nil {
			return "", nil, false, err
		}
		if found {
			_, _ = r.pool.Exec(ctx, `
				UPDATE conversation_members
				SET hidden_at = NULL
				WHERE conversation_id = $1 AND user_id::text = ANY($2)
			`, existingID, memberIDs)
			return existingID, nil, false, nil
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return "", nil, false, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	convID, err := uuid.NewV7()
	if err != nil {
		return "", nil, false, err
	}
	now := time.Now().UTC()

	var directKey *string
	if convType == "direct" && len(memberIDs) == 2 {
		dk := makeDirectKey(memberIDs[0], memberIDs[1])
		directKey = &dk
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO conversations (id, type, title, direct_key, last_sequence, updated_at, created_at)
		VALUES ($1, $2, $3, $4, 0, $5, $5)
	`, convID, convType, title, directKey, now)
	if err != nil {
		// Handle concurrent direct conversation creation race
		if directKey != nil {
			_ = tx.Rollback(ctx)
			existingID, found, findErr := r.FindDirectByUsers(ctx, memberIDs[0], memberIDs[1])
			if findErr == nil && found {
				return existingID, nil, false, nil
			}
		}
		return "", nil, false, err
	}

	for _, uid := range memberIDs {
		_, err = tx.Exec(ctx, `
			INSERT INTO conversation_members (conversation_id, user_id, joined_at)
			VALUES ($1, $2, $3)
			ON CONFLICT DO NOTHING
		`, convID, uid, now)
		if err != nil {
			return "", nil, false, err
		}
	}

	for _, u1 := range memberIDs {
		for _, u2 := range memberIDs {
			if u1 == u2 {
				continue
			}
			_, _ = tx.Exec(ctx, `
				INSERT INTO user_contacts (owner_id, contact_id, created_at)
				VALUES ($1, $2, $3)
				ON CONFLICT (owner_id, contact_id) DO NOTHING
			`, u1, u2, now)
		}
	}

	convStr := convID.String()
	members, err := queryMembersTx(ctx, tx, convStr)
	if err != nil {
		return "", nil, false, err
	}

	createdConv := Conversation{
		ID:          convStr,
		Type:        convType,
		Title:       title,
		Members:     members,
		UnreadCount: 0,
		UpdatedAt:   now,
	}

	env, err := realtime.AppendDurableEvent(ctx, tx, convStr, "conversation.updated", map[string]any{
		"conversation": createdConv,
	})
	if err != nil {
		return "", nil, false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", nil, false, err
	}

	return convStr, env, true, nil
}

func (r *Repository) GetByIDForUser(ctx context.Context, conversationID, userID string) (*Conversation, error) {
	isMember, err := r.IsMember(ctx, conversationID, userID)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrConversationNotFound
	}

	var conv Conversation
	err = r.pool.QueryRow(ctx, `
		SELECT
			c.id::text,
			c.type,
			c.title,
			c.updated_at,
			cm.last_read_message_id::text
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id AND cm.user_id = $2
		WHERE c.id = $1
	`, conversationID, userID).Scan(
		&conv.ID,
		&conv.Type,
		&conv.Title,
		&conv.UpdatedAt,
		&conv.LastReadMessageID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, err
	}

	if err := r.hydrateConversation(ctx, &conv, userID); err != nil {
		return nil, err
	}
	return &conv, nil
}

type convCursor struct {
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"id"`
}

func encodeConvCursor(t time.Time, id string) string {
	b, _ := json.Marshal(convCursor{UpdatedAt: t.UTC(), ID: id})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeConvCursor(raw string) (*convCursor, error) {
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var c convCursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if _, err := uuid.Parse(c.ID); err != nil {
		return nil, err
	}
	return &c, nil
}

func (r *Repository) ListForUser(ctx context.Context, userID string, cursor *string, limit int) ([]Conversation, *string, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	var rows pgx.Rows
	var err error
	if cursor != nil && *cursor != "" {
		decoded, decErr := decodeConvCursor(*cursor)
		if decErr != nil {
			return nil, nil, fmt.Errorf("invalid cursor: %w", decErr)
		}
		rows, err = r.pool.Query(ctx, `
			SELECT
				c.id::text,
				c.type,
				c.title,
				c.updated_at,
				cm.last_read_message_id::text
			FROM conversations c
			JOIN conversation_members cm ON cm.conversation_id = c.id AND cm.user_id = $1
			WHERE cm.hidden_at IS NULL
			  AND (c.updated_at, c.id) < ($2, $3::uuid)
			ORDER BY c.updated_at DESC, c.id DESC
			LIMIT $4
		`, userID, decoded.UpdatedAt, decoded.ID, limit+1)
	} else {
		rows, err = r.pool.Query(ctx, `
			SELECT
				c.id::text,
				c.type,
				c.title,
				c.updated_at,
				cm.last_read_message_id::text
			FROM conversations c
			JOIN conversation_members cm ON cm.conversation_id = c.id AND cm.user_id = $1
			WHERE cm.hidden_at IS NULL
			ORDER BY c.updated_at DESC, c.id DESC
			LIMIT $2
		`, userID, limit+1)
	}
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	convs := make([]Conversation, 0)
	for rows.Next() {
		var c Conversation
		if err := rows.Scan(&c.ID, &c.Type, &c.Title, &c.UpdatedAt, &c.LastReadMessageID); err != nil {
			return nil, nil, err
		}
		convs = append(convs, c)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	var nextCursor *string
	if len(convs) > limit {
		last := convs[limit-1]
		encoded := encodeConvCursor(last.UpdatedAt, last.ID)
		nextCursor = &encoded
		convs = convs[:limit]
	}

	for i := range convs {
		if err := r.hydrateConversation(ctx, &convs[i], userID); err != nil {
			return nil, nil, err
		}
	}

	return convs, nextCursor, nil
}

func (r *Repository) MarkRead(ctx context.Context, conversationID, userID, lastReadMessageID string) (*realtime.Envelope, error) {
	if _, err := uuid.Parse(lastReadMessageID); err != nil {
		return nil, ErrMessageNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var isMember bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM conversation_members
			WHERE conversation_id = $1 AND user_id = $2
		)
	`, conversationID, userID).Scan(&isMember)
	if err != nil {
		return nil, err
	}
	if !isMember {
		return nil, ErrConversationNotFound
	}

	_, err = tx.Exec(ctx, `
		UPDATE conversation_members
		SET last_read_message_id = $3,
		    last_delivered_message_id = COALESCE(last_delivered_message_id, $3)
		WHERE conversation_id = $1 AND user_id = $2
	`, conversationID, userID, lastReadMessageID)
	if err != nil {
		return nil, err
	}

	readAt := time.Now().UTC()
	env, err := realtime.AppendDurableEvent(ctx, tx, conversationID, "message.read", map[string]any{
		"userId":            userID,
		"lastReadMessageId": lastReadMessageID,
		"readAt":            readAt.Format(time.RFC3339Nano),
	})
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return env, nil
}

func (r *Repository) DeleteConversation(ctx context.Context, conversationID, userID string) (*realtime.Envelope, []string, error) {
	if _, err := uuid.Parse(conversationID); err != nil {
		return nil, nil, ErrConversationNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	var convType string
	err = tx.QueryRow(ctx, `
		SELECT c.type
		FROM conversations c
		JOIN conversation_members cm ON cm.conversation_id = c.id AND cm.user_id = $2
		WHERE c.id = $1
		FOR UPDATE OF c
	`, conversationID, userID).Scan(&convType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrConversationNotFound
	}
	if err != nil {
		return nil, nil, err
	}

	if convType == "direct" {
		now := time.Now().UTC()
		_, err = tx.Exec(ctx, `
			UPDATE conversation_members
			SET hidden_at = $3, cleared_at = $3
			WHERE conversation_id = $1 AND user_id = $2
		`, conversationID, userID, now)
		if err != nil {
			return nil, nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, nil, err
		}
		return nil, nil, nil
	}

	// Group conversation: hard delete the group, all members, and all messages for everyone
	members, err := queryMembersTx(ctx, tx, conversationID)
	if err != nil {
		return nil, nil, err
	}
	memberIDs := make([]string, 0, len(members))
	for _, m := range members {
		memberIDs = append(memberIDs, m.ID)
	}

	_, err = tx.Exec(ctx, `DELETE FROM conversations WHERE id = $1`, conversationID)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, err
	}

	convCopy := conversationID
	env, _ := realtime.NewEphemeralEnvelope(&convCopy, "conversation.deleted", map[string]any{
		"conversationId": conversationID,
	})
	return env, memberIDs, nil
}

func (r *Repository) hydrateConversation(ctx context.Context, conv *Conversation, currentUserID string) error {
	rows, err := r.pool.Query(ctx, `
		SELECT u.id::text, u.username, u.display_name, u.avatar_url, u.public_key
		FROM conversation_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.conversation_id = $1
		ORDER BY u.display_name ASC, u.id ASC
	`, conv.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	members := make([]users.UserSummary, 0)
	for rows.Next() {
		var m users.UserSummary
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.AvatarURL, &m.PublicKey); err != nil {
			return err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	conv.Members = members

	// Load last message if any (filtering out cleared_at and message_hidden_for for currentUserID)
	var lastMsg MessagePreview
	err = r.pool.QueryRow(ctx, `
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
		    (SELECT cleared_at FROM conversation_members WHERE conversation_id = $1 AND user_id = $2),
		    '1970-01-01T00:00:00Z'::timestamptz
		  )
		  AND NOT EXISTS (
		    SELECT 1 FROM message_hidden_for mhf WHERE mhf.message_id = m.id AND mhf.user_id = $2
		  )
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT 1
	`, conv.ID, currentUserID).Scan(
		&lastMsg.ID,
		&lastMsg.ConversationID,
		&lastMsg.Sender.ID,
		&lastMsg.Sender.DisplayName,
		&lastMsg.Sender.AvatarURL,
		&lastMsg.Body,
		&lastMsg.ReplyToMessageID,
		&lastMsg.CreatedAt,
		&lastMsg.EditedAt,
		&lastMsg.DeletedAt,
	)
	if err == nil {
		lastMsg.Attachments = make([]uploads.Upload, 0)
		if lastMsg.DeletedAt == nil {
			attRows, attErr := r.pool.Query(ctx, `
				SELECT id::text, file_name, mime_type, size_bytes, created_at
				FROM attachments
				WHERE message_id = $1
				ORDER BY created_at ASC
			`, lastMsg.ID)
			if attErr == nil {
				for attRows.Next() {
					var u uploads.Upload
					if sErr := attRows.Scan(&u.ID, &u.FileName, &u.MimeType, &u.Size, &u.CreatedAt); sErr == nil {
						lastMsg.Attachments = append(lastMsg.Attachments, u)
					}
				}
				attRows.Close()
			}
		}
		conv.LastMessage = &lastMsg
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	// Calculate unread count for currentUserID
	if conv.LastReadMessageID != nil {
		err = r.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM messages m
			WHERE m.conversation_id = $1
			  AND m.sender_id != $2
			  AND m.deleted_at IS NULL
			  AND m.created_at > COALESCE(
			    (SELECT created_at FROM messages WHERE id = $3),
			    '1970-01-01T00:00:00Z'::timestamptz
			  )
		`, conv.ID, currentUserID, *conv.LastReadMessageID).Scan(&conv.UnreadCount)
	} else {
		err = r.pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM messages m
			WHERE m.conversation_id = $1
			  AND m.sender_id != $2
			  AND m.deleted_at IS NULL
		`, conv.ID, currentUserID).Scan(&conv.UnreadCount)
	}
	return err
}

func queryMembersTx(ctx context.Context, tx pgx.Tx, conversationID string) ([]users.UserSummary, error) {
	rows, err := tx.Query(ctx, `
		SELECT u.id::text, u.username, u.display_name, u.avatar_url, u.public_key
		FROM conversation_members cm
		JOIN users u ON u.id = cm.user_id
		WHERE cm.conversation_id = $1
		ORDER BY u.display_name ASC, u.id ASC
	`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	members := make([]users.UserSummary, 0)
	for rows.Next() {
		var m users.UserSummary
		if err := rows.Scan(&m.ID, &m.Username, &m.DisplayName, &m.AvatarURL, &m.PublicKey); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	return members, rows.Err()
}
