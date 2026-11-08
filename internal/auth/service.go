package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"chat-online/internal/users"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrWeakPassword       = errors.New("weak password")
)

// Dummy hash used to mitigate timing side-channels when a user does not exist.
var dummyHash string

func init() {
	h, err := HashPassword("dummy-constant-time-mitigation-pass")
	if err == nil {
		dummyHash = h
	}
}

type AuthenticatedSession struct {
	User      users.User
	Session   Session
	RawCookie string
}

type Service struct {
	repo                 *Repository
	usersService         *users.Service
	sessionTTL           time.Duration
	autoProvisionDevUser bool
	onDevUserCreated     func(ctx context.Context, user users.User)
}

func NewService(
	repo *Repository,
	usersService *users.Service,
	sessionTTL time.Duration,
	autoProvisionDevUser bool,
) *Service {
	// TODO(security): Add OAuth2/OIDC provider support and Multi-Factor Authentication (MFA) for stronger account security.
	return &Service{
		repo:                 repo,
		usersService:         usersService,
		sessionTTL:           sessionTTL,
		autoProvisionDevUser: autoProvisionDevUser,
	}
}

func (s *Service) SetOnDevUserCreated(fn func(ctx context.Context, user users.User)) {
	s.onDevUserCreated = fn
}

func (s *Service) Login(ctx context.Context, username, password string) (*AuthenticatedSession, error) {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" || password == "" {
		return nil, ErrInvalidCredentials
	}

	userRec, err := s.usersService.GetByUsername(ctx, trimmedUsername)
	if err != nil {
		if errors.Is(err, users.ErrUserNotFound) {
			if s.autoProvisionDevUser && ValidatePasswordStrength(password) == nil {
				hash, hashErr := HashPassword(password)
				if hashErr == nil {
					displayName := deriveDisplayName(trimmedUsername)
					created, createErr := s.usersService.CreateUser(ctx, trimmedUsername, displayName, hash, nil)
					if createErr == nil {
						if s.onDevUserCreated != nil {
							s.onDevUserCreated(ctx, created.User)
						}
						return s.issueSession(ctx, created.User)
					}
				}
			}
			_ = VerifyPassword(password, dummyHash)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if !VerifyPassword(password, userRec.PasswordHash) {
		return nil, ErrInvalidCredentials
	}

	return s.issueSession(ctx, userRec.User)
}

func (s *Service) issueSession(ctx context.Context, u users.User) (*AuthenticatedSession, error) {
	rawToken, err := generateRandomHex(32)
	if err != nil {
		return nil, err
	}
	csrfToken, err := generateRandomHex(32)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().UTC().Add(s.sessionTTL)
	sess, err := s.repo.CreateSession(ctx, rawToken, u.ID, csrfToken, expiresAt)
	if err != nil {
		return nil, err
	}

	return &AuthenticatedSession{
		User:      u,
		Session:   *sess,
		RawCookie: rawToken,
	}, nil
}

func (s *Service) AuthenticateToken(ctx context.Context, rawToken string) (*AuthenticatedSession, error) {
	if strings.TrimSpace(rawToken) == "" {
		return nil, ErrSessionNotFound
	}
	sess, err := s.repo.GetSessionByToken(ctx, rawToken)
	if err != nil {
		return nil, err
	}
	u, err := s.usersService.GetByID(ctx, sess.UserID)
	if err != nil {
		return nil, ErrSessionNotFound
	}
	return &AuthenticatedSession{
		User:      *u,
		Session:   *sess,
		RawCookie: rawToken,
	}, nil
}

func (s *Service) Logout(ctx context.Context, rawToken, userID string) error {
	if rawToken != "" {
		_ = s.repo.DeleteByToken(ctx, rawToken)
	}
	if userID != "" {
		return s.repo.DeleteAllForUser(ctx, userID)
	}
	return nil
}

func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	if currentPassword == "" || newPassword == "" {
		return ErrInvalidCredentials
	}
	if err := ValidatePasswordStrength(newPassword); err != nil {
		return ErrWeakPassword
	}

	rec, err := s.usersService.GetRecordByID(ctx, userID)
	if err != nil {
		return err
	}

	if !VerifyPassword(currentPassword, rec.PasswordHash) {
		return ErrInvalidCredentials
	}

	newHash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.usersService.UpdatePasswordHash(ctx, userID, newHash)
}

func (s *Service) AdminCreateOrUpdateUser(ctx context.Context, username, displayName, rawPassword string) (*users.User, error) {
	cleanedUsername := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
	if cleanedUsername == "" {
		return nil, errors.New("username required")
	}
	if err := ValidatePasswordStrength(rawPassword); err != nil {
		return nil, ErrWeakPassword
	}
	cleanedDisplay := strings.TrimSpace(displayName)
	if cleanedDisplay == "" {
		cleanedDisplay = deriveDisplayName(cleanedUsername)
	}

	hash, err := HashPassword(rawPassword)
	if err != nil {
		return nil, err
	}

	created, err := s.usersService.CreateUser(ctx, cleanedUsername, cleanedDisplay, hash, nil)
	if err != nil {
		return nil, err
	}
	return &created.User, nil
}

func (s *Service) ListAccountRequests(ctx context.Context) ([]users.AccountRequest, error) {
	return s.usersService.ListAccountRequests(ctx)
}

func (s *Service) StartSessionCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	go func() {
		defer ticker.Stop()
		_, _ = s.repo.DeleteExpired(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_, _ = s.repo.DeleteExpired(ctx)
			}
		}
	}()
}

func generateRandomHex(numBytes int) (string, error) {
	buf := make([]byte, numBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func deriveDisplayName(username string) string {
	local := username
	if at := strings.IndexByte(username, '@'); at > 0 {
		local = username[:at]
	}
	parts := strings.FieldsFunc(local, func(r rune) bool {
		return r == '.' || r == '_' || r == '-'
	})
	if len(parts) == 0 {
		return username
	}
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

