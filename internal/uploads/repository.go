package uploads

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrUploadNotFound = errors.New("upload not found")
)

type Upload struct {
	ID        string    `json:"id"`
	FileName  string    `json:"fileName"`
	MimeType  string    `json:"mimeType"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
}

type AttachmentRecord struct {
	Upload
	UploaderID     string
	MessageID      *string
	ConversationID *string
	StorageKey     string
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, uploaderID, fileName, storageKey, mimeType string, sizeBytes int64) (*AttachmentRecord, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()

	_, err = r.pool.Exec(ctx, `
		INSERT INTO attachments (id, uploader_id, file_name, storage_key, mime_type, size_bytes, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, uploaderID, fileName, storageKey, mimeType, sizeBytes, now)
	if err != nil {
		return nil, err
	}

	return &AttachmentRecord{
		Upload: Upload{
			ID:        id.String(),
			FileName:  fileName,
			MimeType:  mimeType,
			Size:      sizeBytes,
			CreatedAt: now,
		},
		UploaderID: uploaderID,
		StorageKey: storageKey,
	}, nil
}

func (r *Repository) GetByID(ctx context.Context, id string) (*AttachmentRecord, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrUploadNotFound
	}

	var rec AttachmentRecord
	err := r.pool.QueryRow(ctx, `
		SELECT
			a.id::text,
			a.uploader_id::text,
			a.message_id::text,
			a.conversation_id::text,
			a.file_name,
			a.storage_key,
			a.mime_type,
			a.size_bytes,
			a.created_at
		FROM attachments a
		LEFT JOIN messages m ON m.id = a.message_id
		WHERE a.id = $1 AND (m.id IS NULL OR m.deleted_at IS NULL)
	`, id).Scan(
		&rec.ID,
		&rec.UploaderID,
		&rec.MessageID,
		&rec.ConversationID,
		&rec.FileName,
		&rec.StorageKey,
		&rec.MimeType,
		&rec.Size,
		&rec.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUploadNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}
