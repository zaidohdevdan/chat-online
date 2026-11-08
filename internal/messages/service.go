package messages

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"chat-online/internal/conversations"
	"chat-online/internal/realtime"
	"chat-online/internal/users"
)

const (
	MaxBodyRunes      = 4000
	MaxAttachmentsNum = 10
)

var (
	ErrBodyTooLong        = errors.New("body exceeds 4000 characters")
	ErrTooManyAttachments = errors.New("maximum of 10 attachments exceeded")
	ErrEmptyMessage       = errors.New("message must contain body or attachments")
	ErrMissingClientID    = errors.New("clientMessageId is required")
)

type SendInput struct {
	Body             string   `json:"body"`
	ClientMessageID  string   `json:"clientMessageId"`
	ReplyToMessageID *string  `json:"replyToMessageId"`
	AttachmentIDs    []string `json:"attachmentIds"`
}

type Service struct {
	repo        *Repository
	convService *conversations.Service
	publisher   realtime.Publisher
}

func NewService(
	repo *Repository,
	convService *conversations.Service,
	publisher realtime.Publisher,
) *Service {
	return &Service{
		repo:        repo,
		convService: convService,
		publisher:   publisher,
	}
}

func (s *Service) List(
	ctx context.Context,
	conversationID, userID string,
	cursor *string,
	limit int,
) ([]Message, *string, error) {
	isMember, err := s.convService.IsMember(ctx, conversationID, userID)
	if err != nil {
		return nil, nil, err
	}
	if !isMember {
		return nil, nil, conversations.ErrConversationNotFound
	}

	return s.repo.ListByConversation(ctx, conversationID, userID, cursor, limit)
}

func (s *Service) Send(
	ctx context.Context,
	conversationID string,
	sender users.UserSummary,
	input SendInput,
) (*Message, bool, error) {
	isMember, err := s.convService.IsMember(ctx, conversationID, sender.ID)
	if err != nil {
		return nil, false, err
	}
	if !isMember {
		return nil, false, conversations.ErrConversationNotFound
	}

	clientID := strings.TrimSpace(input.ClientMessageID)
	if clientID == "" || len(clientID) > 128 {
		return nil, false, ErrMissingClientID
	}

	trimmedBody := strings.TrimSpace(input.Body)
	if utf8.RuneCountInString(trimmedBody) > MaxBodyRunes {
		return nil, false, ErrBodyTooLong
	}
	if len(input.AttachmentIDs) > MaxAttachmentsNum {
		return nil, false, ErrTooManyAttachments
	}
	if trimmedBody == "" && len(input.AttachmentIDs) == 0 {
		return nil, false, ErrEmptyMessage
	}

	msg, env, created, err := s.repo.CreateOrGetIdempotent(ctx, SendParams{
		ConversationID:   conversationID,
		Sender:           sender,
		ClientMessageID:  clientID,
		Body:             trimmedBody,
		ReplyToMessageID: input.ReplyToMessageID,
		AttachmentIDs:    input.AttachmentIDs,
	})
	if err != nil {
		return nil, false, err
	}

	if created && env != nil && s.publisher != nil {
		s.publisher.PublishConversationEvent(ctx, *env)
	}

	return msg, created, nil
}

func (s *Service) Edit(ctx context.Context, messageID, userID, body string) (*Message, error) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, ErrEmptyMessage
	}
	if utf8.RuneCountInString(trimmed) > MaxBodyRunes {
		return nil, ErrBodyTooLong
	}

	msg, env, err := s.repo.EditMessage(ctx, messageID, userID, trimmed)
	if err != nil {
		return nil, err
	}
	if env != nil && s.publisher != nil {
		s.publisher.PublishConversationEvent(ctx, *env)
	}
	return msg, nil
}

func (s *Service) Delete(ctx context.Context, messageID, userID, scope, conversationIDHint string) error {
	env, err := s.repo.DeleteMessage(ctx, messageID, userID, scope, conversationIDHint)
	if err != nil {
		return err
	}
	if env != nil && s.publisher != nil {
		s.publisher.PublishConversationEvent(ctx, *env)
	}
	return nil
}

func (s *Service) RecordDeliveryAck(ctx context.Context, userID, messageID string) (*realtime.Envelope, error) {
	return s.repo.RecordDeliveryAck(ctx, userID, messageID)
}
