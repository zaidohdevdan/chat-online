package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var ErrInvalidStorageKey = errors.New("invalid storage key")

type Storage interface {
	Save(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

type LocalStorage struct {
	rootDir string
}

func NewLocalStorage(rootDir string) (*LocalStorage, error) {
	absRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve upload storage root: %w", err)
	}
	// Create directory with 0750 permissions (non-world-accessible).
	if err := os.MkdirAll(absRoot, 0o750); err != nil {
		return nil, fmt.Errorf("create upload storage dir: %w", err)
	}
	return &LocalStorage{rootDir: absRoot}, nil
}

func (s *LocalStorage) resolveSafePath(key string) (string, error) {
	cleanKey := filepath.Base(key)
	if cleanKey == "." || cleanKey == "/" || cleanKey == ".." || cleanKey != key {
		return "", ErrInvalidStorageKey
	}
	fullPath := filepath.Join(s.rootDir, cleanKey)
	absPath, err := filepath.Abs(fullPath)
	if err != nil {
		return "", ErrInvalidStorageKey
	}
	prefix := s.rootDir + string(filepath.Separator)
	if !strings.HasPrefix(absPath, prefix) {
		return "", ErrInvalidStorageKey
	}
	return absPath, nil
}

func (s *LocalStorage) Save(_ context.Context, key string, r io.Reader) error {
	targetPath, err := s.resolveSafePath(key)
	if err != nil {
		return err
	}
	// 0600 ensures uploaded files are non-executable and readable only by the service user.
	f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
	}()

	_, err = io.Copy(f, r)
	return err
}

func (s *LocalStorage) Open(_ context.Context, key string) (io.ReadCloser, error) {
	targetPath, err := s.resolveSafePath(key)
	if err != nil {
		return nil, err
	}
	return os.Open(targetPath)
}

func (s *LocalStorage) Delete(_ context.Context, key string) error {
	targetPath, err := s.resolveSafePath(key)
	if err != nil {
		return err
	}
	err = os.Remove(targetPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

