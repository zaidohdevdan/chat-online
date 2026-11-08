package users

import (
	"context"
)

type Service struct {
	repo               *Repository
	onAccountRequested func(ctx context.Context, req *AccountRequest)
	onAccountApproved  func(ctx context.Context, createdUser User, requesterIDs []string)
}

func NewService(repo *Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) SetOnAccountRequested(fn func(ctx context.Context, req *AccountRequest)) {
	s.onAccountRequested = fn
}

func (s *Service) SetOnAccountApproved(fn func(ctx context.Context, createdUser User, requesterIDs []string)) {
	s.onAccountApproved = fn
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

func (s *Service) GetRecordByID(ctx context.Context, id string) (*UserRecord, error) {
	return s.repo.GetRecordByID(ctx, id)
}

func (s *Service) UpdatePasswordHash(ctx context.Context, userID, passwordHash string) error {
	return s.repo.UpdatePasswordHash(ctx, userID, passwordHash)
}

func (s *Service) CreateUser(ctx context.Context, username, displayName, passwordHash string, avatarURL *string) (*UserRecord, error) {
	rec, approvedRequesters, err := s.repo.Create(ctx, username, displayName, passwordHash, avatarURL)
	if err != nil {
		return nil, err
	}
	if len(approvedRequesters) > 0 && s.onAccountApproved != nil {
		s.onAccountApproved(ctx, rec.User, approvedRequesters)
	}
	return rec, nil
}

func (s *Service) SearchContacts(ctx context.Context, ownerID, query string, limit int) ([]UserSummary, error) {
	return s.repo.SearchContacts(ctx, ownerID, query, limit)
}

func (s *Service) AddContactByUsername(ctx context.Context, ownerID, username string) (*UserSummary, bool, error) {
	return s.repo.AddContactByUsername(ctx, ownerID, username)
}

func (s *Service) CreateAccountRequest(ctx context.Context, requesterID, requestedUsername, requestedDisplayName string) (*AccountRequest, error) {
	req, err := s.repo.CreateAccountRequest(ctx, requesterID, requestedUsername, requestedDisplayName)
	if err != nil {
		return nil, err
	}
	if s.onAccountRequested != nil {
		s.onAccountRequested(ctx, req)
	}
	return req, nil
}

func (s *Service) ListAccountRequests(ctx context.Context) ([]AccountRequest, error) {
	return s.repo.ListAccountRequests(ctx)
}

func (s *Service) ExportUserData(ctx context.Context, userID string) (*LGPDExport, error) {
	return s.repo.ExportUserData(ctx, userID)
}

func (s *Service) AnonymizeAndDeleteUser(ctx context.Context, userID string) ([]string, error) {
	return s.repo.AnonymizeAndDeleteUser(ctx, userID)
}
