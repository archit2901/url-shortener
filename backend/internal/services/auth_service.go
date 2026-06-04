package services

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"

	"github.com/archit2901/url-shortener/backend/internal/auth"
	"github.com/archit2901/url-shortener/backend/internal/repository"
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrPasswordTooShort   = errors.New("password too short")
	ErrInvalidCredentials = errors.New("invalid credentials")
)

const minPasswordLength = 8

// UserRepository is what AuthService needs from the user repo.
type UserRepository interface {
	Create(ctx context.Context, email, passwordHash string) (*repository.User, error)
	GetByEmail(ctx context.Context, email string) (*repository.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*repository.User, error)
}

type AuthService struct {
	repo    UserRepository
	authSvc *auth.Service
}

func NewAuthService(repo UserRepository, authSvc *auth.Service) *AuthService {
	return &AuthService{repo: repo, authSvc: authSvc}
}

// Register creates a new user account.
func (s *AuthService) Register(ctx context.Context, email, password string) (*repository.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if !isValidEmail(email) {
		return nil, ErrInvalidEmail
	}
	if len(password) < minPasswordLength {
		return nil, ErrPasswordTooShort
	}

	hash, err := s.authSvc.HashPassword(password)
	if err != nil {
		return nil, err
	}

	return s.repo.Create(ctx, email, hash)
}

// Login verifies credentials and returns a signed JWT plus the user record.
func (s *AuthService) Login(ctx context.Context, email, password string) (string, *repository.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.repo.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, repository.ErrUserNotFound) {
			// Return the same error as a bad password to prevent
			// user enumeration via timing/error differences.
			return "", nil, ErrInvalidCredentials
		}
		return "", nil, err
	}

	if err := s.authSvc.VerifyPassword(user.PasswordHash, password); err != nil {
		return "", nil, ErrInvalidCredentials
	}

	token, err := s.authSvc.GenerateToken(user.ID, user.Email)
	if err != nil {
		return "", nil, err
	}

	return token, user, nil
}

func isValidEmail(s string) bool {
	if len(s) < 3 || len(s) > 254 {
		return false
	}
	at := strings.Index(s, "@")
	if at < 1 || at == len(s)-1 {
		return false
	}
	if strings.Contains(s[at+1:], "@") {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}
