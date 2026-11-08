package users

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrUserNotFound = errors.New("user not found")

type UserSummary struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
}

type User struct {
	ID          string    `json:"id"`
	DisplayName string    `json:"displayName"`
	AvatarURL   *string   `json:"avatarUrl,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type UserRecord struct {
	User
	Username     string
	PasswordHash string
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, username, displayName, passwordHash string, avatarURL *string) (*UserRecord, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	normalized := strings.ToLower(strings.TrimSpace(username))

	var rec UserRecord
	rec.Username = normalized
	rec.PasswordHash = passwordHash
	err = r.pool.QueryRow(ctx, `
		INSERT INTO users (id, username, display_name, avatar_url, password_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (username) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    password_hash = EXCLUDED.password_hash
		RETURNING id::text, username, display_name, avatar_url, password_hash, created_at
	`, id, normalized, displayName, avatarURL, passwordHash, now).Scan(
		&rec.ID,
		&rec.Username,
		&rec.DisplayName,
		&rec.AvatarURL,
		&rec.PasswordHash,
		&rec.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*User, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrUserNotFound
	}
	var u User
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, display_name, avatar_url, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&u.ID, &u.DisplayName, &u.AvatarURL, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repository) GetByIDs(ctx context.Context, ids []string) ([]UserSummary, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, display_name, avatar_url
		FROM users
		WHERE id::text = ANY($1)
		ORDER BY display_name ASC
	`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []UserSummary
	for rows.Next() {
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.AvatarURL); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func (r *Repository) GetByUsername(ctx context.Context, username string) (*UserRecord, error) {
	normalized := strings.ToLower(strings.TrimSpace(username))
	var rec UserRecord
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, username, display_name, avatar_url, password_hash, created_at
		FROM users
		WHERE username = $1
	`, normalized).Scan(
		&rec.ID,
		&rec.Username,
		&rec.DisplayName,
		&rec.AvatarURL,
		&rec.PasswordHash,
		&rec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) Search(ctx context.Context, query string, limit int) ([]UserSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"

	rows, err := r.pool.Query(ctx, `
		SELECT id::text, display_name, avatar_url
		FROM users
		WHERE LOWER(display_name) LIKE $1 OR LOWER(username) LIKE $1
		ORDER BY display_name ASC, id ASC
		LIMIT $2
	`, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]UserSummary, 0)
	for rows.Next() {
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.AvatarURL); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

type ExportedMessage struct {
	ID             string     `json:"id"`
	ConversationID string     `json:"conversationId"`
	Body           *string    `json:"body"`
	CreatedAt      time.Time  `json:"createdAt"`
	EditedAt       *time.Time `json:"editedAt,omitempty"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type LGPDExport struct {
	User            User              `json:"user"`
	Username        string            `json:"username"`
	ConversationIDs []string          `json:"conversationIds"`
	Messages        []ExportedMessage `json:"messages"`
	ExportedAt      time.Time         `json:"exportedAt"`
}

func (r *Repository) ExportUserData(ctx context.Context, userID string) (*LGPDExport, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, ErrUserNotFound
	}

	var exp LGPDExport
	exp.ExportedAt = time.Now().UTC()
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, username, display_name, avatar_url, created_at
		FROM users
		WHERE id = $1
	`, userID).Scan(&exp.User.ID, &exp.Username, &exp.User.DisplayName, &exp.User.AvatarURL, &exp.User.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}

	convRows, err := r.pool.Query(ctx, `
		SELECT conversation_id::text FROM conversation_members WHERE user_id = $1 ORDER BY joined_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer convRows.Close()
	exp.ConversationIDs = make([]string, 0)
	for convRows.Next() {
		var cid string
		if err := convRows.Scan(&cid); err != nil {
			return nil, err
		}
		exp.ConversationIDs = append(exp.ConversationIDs, cid)
	}

	msgRows, err := r.pool.Query(ctx, `
		SELECT id::text, conversation_id::text, body, created_at, edited_at, deleted_at
		FROM messages
		WHERE sender_id = $1
		ORDER BY created_at ASC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer msgRows.Close()
	exp.Messages = make([]ExportedMessage, 0)
	for msgRows.Next() {
		var m ExportedMessage
		if err := msgRows.Scan(&m.ID, &m.ConversationID, &m.Body, &m.CreatedAt, &m.EditedAt, &m.DeletedAt); err != nil {
			return nil, err
		}
		exp.Messages = append(exp.Messages, m)
	}
	return &exp, msgRows.Err()
}

// AnonymizeAndDeleteUser implements LGPD right-to-erasure and invalidates all active sessions.
func (r *Repository) AnonymizeAndDeleteUser(ctx context.Context, userID string) ([]string, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return nil, ErrUserNotFound
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	// 1. Invalidate all active sessions immediately.
	_, err = tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}

	// 2. Collect storage keys for user attachments and delete attachment records.
	attRows, err := tx.Query(ctx, `
		DELETE FROM attachments WHERE uploader_id = $1 RETURNING storage_key
	`, userID)
	if err != nil {
		return nil, err
	}
	var storageKeys []string
	for attRows.Next() {
		var key string
		if sErr := attRows.Scan(&key); sErr == nil {
			storageKeys = append(storageKeys, key)
		}
	}
	attRows.Close()

	// 3. Soft-delete and clear body of all messages sent by this user.
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE messages
		SET body = NULL, deleted_at = COALESCE(deleted_at, $2)
		WHERE sender_id = $1
	`, userID, now)
	if err != nil {
		return nil, err
	}

	// 4. Remove active conversation memberships.
	_, err = tx.Exec(ctx, `DELETE FROM conversation_members WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}

	// 5. Anonymize personal identifiers on the user record.
	anonUsername := "deleted-" + userID
	tag, err := tx.Exec(ctx, `
		UPDATE users
		SET username = $2,
		    display_name = 'Conta removida',
		    avatar_url = NULL,
		    password_hash = 'deleted'
		WHERE id = $1
	`, userID, anonUsername)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrUserNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return storageKeys, nil
}
