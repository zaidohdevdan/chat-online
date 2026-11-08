package conversations

import (
	"context"
	"errors"
	"strings"

	"chat-online/internal/realtime"
	"chat-online/internal/users"
)

var (
	ErrInvalidConversationInput = errors.New("invalid conversation input")
)

type CreateInput struct {
	Type      string   `json:"type"`
	MemberIDs []string `json:"memberIds"`
	Title     *string  `json:"title,omitempty"`
}

type Service struct {
	repo         *Repository
	usersService *users.Service
	publisher    realtime.Publisher
}

func NewService(repo *Repository, usersService *users.Service, publisher realtime.Publisher) *Service {
	return &Service{
		repo:         repo,
		usersService: usersService,
		publisher:    publisher,
	}
}

func (s *Service) IsMember(ctx context.Context, conversationID, userID string) (bool, error) {
	return s.repo.IsMember(ctx, conversationID, userID)
}

func (s *Service) ListForUser(ctx context.Context, userID string, cursor *string, limit int) ([]Conversation, *string, error) {
	return s.repo.ListForUser(ctx, userID, cursor, limit)
}

func (s *Service) GetByIDForUser(ctx context.Context, conversationID, userID string) (*Conversation, error) {
	return s.repo.GetByIDForUser(ctx, conversationID, userID)
}

func (s *Service) Create(ctx context.Context, creatorID string, input CreateInput) (*Conversation, bool, error) {
	if input.Type != "direct" && input.Type != "group" {
		return nil, false, ErrInvalidConversationInput
	}

	uniqueSet := map[string]struct{}{creatorID: {}}
	for _, id := range input.MemberIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			uniqueSet[trimmed] = struct{}{}
		}
	}

	memberIDs := make([]string, 0, len(uniqueSet))
	for id := range uniqueSet {
		memberIDs = append(memberIDs, id)
	}

	if input.Type == "direct" && len(memberIDs) != 2 {
		return nil, false, ErrInvalidConversationInput
	}
	if input.Type == "group" {
		if len(memberIDs) < 2 {
			return nil, false, ErrInvalidConversationInput
		}
		if input.Title == nil || strings.TrimSpace(*input.Title) == "" {
			return nil, false, ErrInvalidConversationInput
		}
		trimmedTitle := strings.TrimSpace(*input.Title)
		input.Title = &trimmedTitle
	} else {
		input.Title = nil
	}

	// Verify that all target members exist
	foundUsers, err := s.usersService.GetByIDs(ctx, memberIDs)
	if err != nil || len(foundUsers) != len(memberIDs) {
		return nil, false, ErrInvalidConversationInput
	}

	convID, env, created, err := s.repo.Create(ctx, input.Type, input.Title, memberIDs)
	if err != nil {
		return nil, false, err
	}

	conv, err := s.repo.GetByIDForUser(ctx, convID, creatorID)
	if err != nil {
		return nil, false, err
	}

	if created && env != nil && s.publisher != nil {
		s.publisher.PublishConversationEvent(ctx, *env)
	}

	return conv, created, nil
}

func (s *Service) MarkRead(ctx context.Context, conversationID, userID, lastReadMessageID string) error {
	env, err := s.repo.MarkRead(ctx, conversationID, userID, lastReadMessageID)
	if err != nil {
		return err
	}
	if env != nil && s.publisher != nil {
		s.publisher.PublishConversationEvent(ctx, *env)
	}
	return nil
}

func (s *Service) Delete(ctx context.Context, conversationID, userID string) error {
	env, memberIDs, err := s.repo.DeleteConversation(ctx, conversationID, userID)
	if err != nil {
		return err
	}
	if env != nil && len(memberIDs) > 0 && s.publisher != nil {
		s.publisher.PublishToUsers(memberIDs, *env)
	}
	return nil
}
