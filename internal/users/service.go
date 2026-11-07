package users

import (
	"context"
)

type Service struct {
	repo *Repository
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) GetByID(ctx context.Context, id string) (*User, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) GetByIDs(ctx context.Context, ids []string) ([]UserSummary, error) {
	return s.repo.GetByIDs(ctx, ids)
}

func (s *Service) GetByUsername(ctx context.Context, username string) (*UserRecord, error) {
	return s.repo.GetByUsername(ctx, username)
}

func (s *Service) CreateUser(ctx context.Context, username, displayName, passwordHash string, avatarURL *string) (*UserRecord, error) {
	return s.repo.Create(ctx, username, displayName, passwordHash, avatarURL)
}

func (s *Service) Search(ctx context.Context, query string, limit int) ([]UserSummary, error) {
	return s.repo.Search(ctx, query, limit)
}

func (s *Service) ExportUserData(ctx context.Context, userID string) (*LGPDExport, error) {
	return s.repo.ExportUserData(ctx, userID)
}

func (s *Service) AnonymizeAndDeleteUser(ctx context.Context, userID string) ([]string, error) {
	return s.repo.AnonymizeAndDeleteUser(ctx, userID)
}

