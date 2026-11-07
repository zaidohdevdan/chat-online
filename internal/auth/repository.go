package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type Session struct {
	ID        string
	UserID    string
	CSRFToken string
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func hashToken(rawToken string) string {
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func (r *Repository) CreateSession(ctx context.Context, rawToken, userID, csrfToken string, expiresAt time.Time) (*Session, error) {
	tokenHash := hashToken(rawToken)
	now := time.Now().UTC()

	_, err := r.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, csrf_token, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, tokenHash, userID, csrfToken, expiresAt.UTC(), now)
	if err != nil {
		return nil, err
	}

	return &Session{
		ID:        tokenHash,
		UserID:    userID,
		CSRFToken: csrfToken,
		ExpiresAt: expiresAt.UTC(),
		CreatedAt: now,
	}, nil
}

func (r *Repository) GetSessionByToken(ctx context.Context, rawToken string) (*Session, error) {
	tokenHash := hashToken(rawToken)
	var s Session
	err := r.pool.QueryRow(ctx, `
		SELECT id, user_id::text, csrf_token, expires_at, created_at
		FROM sessions
		WHERE id = $1
	`, tokenHash).Scan(&s.ID, &s.UserID, &s.CSRFToken, &s.ExpiresAt, &s.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	if time.Now().UTC().After(s.ExpiresAt) {
		_ = r.DeleteByToken(ctx, rawToken)
		return nil, ErrSessionExpired
	}
	return &s, nil
}

func (r *Repository) DeleteByToken(ctx context.Context, rawToken string) error {
	tokenHash := hashToken(rawToken)
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, tokenHash)
	return err
}

func (r *Repository) DeleteAllForUser(ctx context.Context, userID string) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (r *Repository) DeleteExpired(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= NOW()`)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

