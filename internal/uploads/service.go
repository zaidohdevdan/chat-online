package uploads

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"chat-online/internal/storage"

	"github.com/google/uuid"
)

const MaxUploadSizeBytes = 25 * 1024 * 1024 // 25 MB

var (
	ErrUnsupportedMediaType = errors.New("unsupported media type")
	ErrFileTooLarge         = errors.New("file exceeds maximum allowed size")
	ErrEmptyFile            = errors.New("file is empty")
)

var allowedMimeExtensions = map[string][]string{
	"image/png":                 {".png"},
	"image/jpeg":                {".jpg", ".jpeg"},
	"image/gif":                 {".gif"},
	"image/webp":                {".webp"},
	"application/pdf":           {".pdf"},
	"text/plain; charset=utf-8": {".txt", ".md", ".csv", ".log"},
	"text/plain":                {".txt", ".md", ".csv", ".log"},
}

type MembershipChecker interface {
	IsMember(ctx context.Context, conversationID, userID string) (bool, error)
}

type Service struct {
	repo       *Repository
	store      storage.Storage
	membership MembershipChecker
}

func NewService(repo *Repository, store storage.Storage, membership MembershipChecker) *Service {
	// TODO(security): Integrate antivirus/malware scanning in a sandbox before persisting uploaded files.
	// TODO(security): Integrate Content Disarm and Reconstruction (CDR) to strip active content from PDFs/images.
	return &Service{
		repo:       repo,
		store:      store,
		membership: membership,
	}
}

func (s *Service) UploadFile(ctx context.Context, uploaderID, originalName string, r io.Reader) (*Upload, error) {
	safeFileName := sanitizeFileName(originalName)
	ext := strings.ToLower(filepath.Ext(safeFileName))

	limited := io.LimitReader(r, MaxUploadSizeBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, ErrEmptyFile
	}
	if int64(len(data)) > MaxUploadSizeBytes {
		return nil, ErrFileTooLarge
	}

	detectedMime := http.DetectContentType(data)
	allowedExts, ok := allowedMimeExtensions[detectedMime]
	if !ok {
		return nil, ErrUnsupportedMediaType
	}
	extMatch := false
	for _, allowedExt := range allowedExts {
		if ext == allowedExt {
			extMatch = true
			break
		}
	}
	if !extMatch {
		return nil, ErrUnsupportedMediaType
	}

	canonicalMime := strings.Split(detectedMime, ";")[0]

	storageUUID, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	storageKey := storageUUID.String() + ext

	if err := s.store.Save(ctx, storageKey, bytes.NewReader(data)); err != nil {
		return nil, err
	}

	rec, err := s.repo.Create(ctx, uploaderID, safeFileName, storageKey, canonicalMime, int64(len(data)))
	if err != nil {
		_ = s.store.Delete(ctx, storageKey)
		return nil, err
	}

	return &rec.Upload, nil
}

func (s *Service) OpenAuthorized(ctx context.Context, uploadID, userID string) (*AttachmentRecord, io.ReadCloser, error) {
	rec, err := s.repo.GetByID(ctx, uploadID)
	if err != nil {
		return nil, nil, err
	}

	if rec.ConversationID != nil {
		isMember, err := s.membership.IsMember(ctx, *rec.ConversationID, userID)
		if err != nil {
			return nil, nil, err
		}
		if !isMember {
			return nil, nil, ErrUploadNotFound
		}
	} else if rec.UploaderID != userID {
		return nil, nil, ErrUploadNotFound
	}

	rc, err := s.store.Open(ctx, rec.StorageKey)
	if err != nil {
		return nil, nil, ErrUploadNotFound
	}
	return rec, rc, nil
}

func sanitizeFileName(name string) string {
	normalized := strings.ReplaceAll(name, "\\", "/")
	base := filepath.Base(normalized)
	base = strings.Map(func(r rune) rune {
		if r < 32 || r == '"' || r == '\'' || r == ';' {
			return '_'
		}
		return r
	}, base)
	if base == "" || base == "." || base == "/" {
		return "arquivo.bin"
	}
	return base
}
