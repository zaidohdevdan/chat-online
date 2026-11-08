package users

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUserNotFound  = errors.New("user not found")
	ErrCannotAddSelf = errors.New("cannot add self as contact")
)

func IsAdminUsername(username string) bool {
	cleaned := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if cleaned == "" {
		return false
	}
	adminList := []string{"daniel.almeida", "danfealmeidafilho@gmail.com"}
	if raw := strings.TrimSpace(os.Getenv("ADMIN_USERNAMES")); raw != "" {
		adminList = nil
		for _, item := range strings.Split(raw, ",") {
			if t := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(item), "@")); t != "" {
				adminList = append(adminList, t)
			}
		}
	}
	for _, admin := range adminList {
		if cleaned == admin {
			return true
		}
	}
	return false
}

type UserSummary struct {
	ID          string  `json:"id"`
	Username    string  `json:"username,omitempty"`
	DisplayName string  `json:"displayName"`
	AvatarURL   *string `json:"avatarUrl,omitempty"`
	PublicKey   *string `json:"publicKey,omitempty"`
}

type User struct {
	ID          string    `json:"id"`
	Username    string    `json:"username,omitempty"`
	DisplayName string    `json:"displayName"`
	AvatarURL   *string   `json:"avatarUrl,omitempty"`
	PublicKey   *string   `json:"publicKey,omitempty"`
	IsAdmin     bool      `json:"isAdmin,omitempty"`
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

func (r *Repository) Create(ctx context.Context, username, displayName, passwordHash string, avatarURL *string) (*UserRecord, []string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, nil, err
	}
	now := time.Now().UTC()
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))

	var rec UserRecord
	rec.Username = normalized
	rec.PasswordHash = passwordHash
	err = r.pool.QueryRow(ctx, `
		INSERT INTO users (id, username, display_name, avatar_url, password_hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (username) DO UPDATE
		SET display_name = EXCLUDED.display_name,
		    password_hash = EXCLUDED.password_hash
		RETURNING id::text, username, display_name, avatar_url, public_key, password_hash, created_at
	`, id, normalized, displayName, avatarURL, passwordHash, now).Scan(
		&rec.ID,
		&rec.Username,
		&rec.DisplayName,
		&rec.AvatarURL,
		&rec.PublicKey,
		&rec.PasswordHash,
		&rec.CreatedAt,
	)
	if err != nil {
		return nil, nil, err
	}
	rec.User.Username = rec.Username
	rec.User.IsAdmin = IsAdminUsername(rec.Username)

	// Auto-approve any pending account_requests for this username and link requester <-> new user bidirectionally
	_, _ = r.pool.Exec(ctx, `
		INSERT INTO user_contacts (owner_id, contact_id, created_at)
		SELECT requester_id, $1::uuid, NOW()
		FROM account_requests
		WHERE LOWER(requested_username) = $2 AND requester_id <> $1::uuid
		ON CONFLICT (owner_id, contact_id) DO NOTHING
	`, rec.ID, normalized)
	_, _ = r.pool.Exec(ctx, `
		INSERT INTO user_contacts (owner_id, contact_id, created_at)
		SELECT $1::uuid, requester_id, NOW()
		FROM account_requests
		WHERE LOWER(requested_username) = $2 AND requester_id <> $1::uuid
		ON CONFLICT (owner_id, contact_id) DO NOTHING
	`, rec.ID, normalized)

	var approvedRequesterIDs []string
	rows, qErr := r.pool.Query(ctx, `
		UPDATE account_requests
		SET status = 'approved'
		WHERE LOWER(requested_username) = $1 AND status = 'pending'
		RETURNING DISTINCT requester_id::text
	`, normalized)
	if qErr == nil {
		for rows.Next() {
			var reqID string
			if err := rows.Scan(&reqID); err == nil {
				approvedRequesterIDs = append(approvedRequesterIDs, reqID)
			}
		}
		rows.Close()
	}

	return &rec, approvedRequesterIDs, nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*User, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrUserNotFound
	}
	var u User
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, username, display_name, avatar_url, public_key, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.PublicKey, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	u.IsAdmin = IsAdminUsername(u.Username)
	return &u, nil
}

func (r *Repository) GetRecordByID(ctx context.Context, id string) (*UserRecord, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrUserNotFound
	}
	var rec UserRecord
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, username, display_name, avatar_url, public_key, password_hash, created_at
		FROM users
		WHERE id = $1
	`, id).Scan(
		&rec.ID,
		&rec.Username,
		&rec.DisplayName,
		&rec.AvatarURL,
		&rec.PublicKey,
		&rec.PasswordHash,
		&rec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.User.Username = rec.Username
	rec.User.IsAdmin = IsAdminUsername(rec.Username)
	return &rec, nil
}

func (r *Repository) UpdatePasswordHash(ctx context.Context, userID, passwordHash string) error {
	if _, err := uuid.Parse(userID); err != nil {
		return ErrUserNotFound
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE users
		SET password_hash = $2
		WHERE id = $1
	`, userID, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *Repository) UpdatePublicKey(ctx context.Context, userID, publicKey string) error {
	if _, err := uuid.Parse(userID); err != nil {
		return ErrUserNotFound
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE users
		SET public_key = $2
		WHERE id = $1
	`, userID, strings.TrimSpace(publicKey))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (r *Repository) GetByIDs(ctx context.Context, ids []string) ([]UserSummary, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id::text, username, display_name, avatar_url, public_key
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
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.PublicKey); err != nil {
			return nil, err
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func (r *Repository) GetByUsername(ctx context.Context, username string) (*UserRecord, error) {
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	var rec UserRecord
	err := r.pool.QueryRow(ctx, `
		SELECT id::text, username, display_name, avatar_url, public_key, password_hash, created_at
		FROM users
		WHERE username = $1
	`, normalized).Scan(
		&rec.ID,
		&rec.Username,
		&rec.DisplayName,
		&rec.AvatarURL,
		&rec.PublicKey,
		&rec.PasswordHash,
		&rec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	rec.User.Username = rec.Username
	rec.User.IsAdmin = IsAdminUsername(rec.Username)
	return &rec, nil
}

func (r *Repository) AddContactByUsername(ctx context.Context, ownerID, username string) (*UserSummary, bool, error) {
	target, err := r.GetByUsername(ctx, username)
	if err != nil {
		return nil, false, err
	}
	if target.ID == ownerID {
		return nil, false, ErrCannotAddSelf
	}

	tag, err := r.pool.Exec(ctx, `
		INSERT INTO user_contacts (owner_id, contact_id, created_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (owner_id, contact_id) DO NOTHING
	`, ownerID, target.ID)
	if err != nil {
		return nil, false, err
	}

	return &UserSummary{
		ID:          target.ID,
		Username:    target.Username,
		DisplayName: target.DisplayName,
		AvatarURL:   target.AvatarURL,
		PublicKey:   target.PublicKey,
	}, tag.RowsAffected() > 0, nil
}

func (r *Repository) SearchContacts(ctx context.Context, ownerID, query string, limit int) ([]UserSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	pattern := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"

	rows, err := r.pool.Query(ctx, `
		SELECT u.id::text, u.username, u.display_name, u.avatar_url, u.public_key
		FROM user_contacts uc
		JOIN users u ON u.id = uc.contact_id
		WHERE uc.owner_id = $1
		  AND (LOWER(u.display_name) LIKE $2 OR LOWER(u.username) LIKE $2)
		ORDER BY u.display_name ASC, u.id ASC
		LIMIT $3
	`, ownerID, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]UserSummary, 0)
	for rows.Next() {
		var u UserSummary
		if err := rows.Scan(&u.ID, &u.Username, &u.DisplayName, &u.AvatarURL, &u.PublicKey); err != nil {
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

type AccountRequest struct {
	ID                   string    `json:"id"`
	RequesterID          string    `json:"requesterId"`
	RequesterUsername    string    `json:"requesterUsername"`
	RequesterDisplayName string    `json:"requesterDisplayName"`
	RequestedUsername    string    `json:"requestedUsername"`
	RequestedDisplayName string    `json:"requestedDisplayName,omitempty"`
	Status               string    `json:"status"`
	CreatedAt            time.Time `json:"createdAt"`
}

func (r *Repository) CreateAccountRequest(ctx context.Context, requesterID, requestedUsername, requestedDisplayName string) (*AccountRequest, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	normalized := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(requestedUsername), "@"))
	cleanDisplay := strings.TrimSpace(requestedDisplayName)
	now := time.Now().UTC()

	_, err = r.pool.Exec(ctx, `
		INSERT INTO account_requests (id, requester_id, requested_username, requested_display_name, status, created_at)
		VALUES ($1, $2, $3, $4, 'pending', $5)
	`, id, requesterID, normalized, cleanDisplay, now)
	if err != nil {
		return nil, err
	}

	var req AccountRequest
	err = r.pool.QueryRow(ctx, `
		SELECT ar.id::text, ar.requester_id::text, u.username, u.display_name, ar.requested_username, ar.requested_display_name, ar.status, ar.created_at
		FROM account_requests ar
		JOIN users u ON u.id = ar.requester_id
		WHERE ar.id = $1
	`, id).Scan(
		&req.ID,
		&req.RequesterID,
		&req.RequesterUsername,
		&req.RequesterDisplayName,
		&req.RequestedUsername,
		&req.RequestedDisplayName,
		&req.Status,
		&req.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &req, nil
}

func (r *Repository) ListAccountRequests(ctx context.Context) ([]AccountRequest, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT ar.id::text, ar.requester_id::text, u.username, u.display_name, ar.requested_username, ar.requested_display_name, ar.status, ar.created_at
		FROM account_requests ar
		JOIN users u ON u.id = ar.requester_id
		ORDER BY ar.created_at DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]AccountRequest, 0)
	for rows.Next() {
		var item AccountRequest
		if err := rows.Scan(
			&item.ID,
			&item.RequesterID,
			&item.RequesterUsername,
			&item.RequesterDisplayName,
			&item.RequestedUsername,
			&item.RequestedDisplayName,
			&item.Status,
			&item.CreatedAt,
		); err != nil {
			return nil, err
		}
		list = append(list, item)
	}
	return list, rows.Err()
}
