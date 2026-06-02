package services

import (
	"context"
	"errors"
	"net/url"
	"strings"

	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/shortener"
)

var ErrInvalidURL = errors.New("invalid url")

type URLService struct {
	repo *repository.URLRepository
}

func NewURLService(repo *repository.URLRepository) *URLService {
	return &URLService{repo: repo}
}

// Shorten validates the long URL, persists it, and returns the generated short code.
func (s *URLService) Shorten(ctx context.Context, longURL string) (string, error) {
	longURL = strings.TrimSpace(longURL)
	if !isValidURL(longURL) {
		return "", ErrInvalidURL
	}

	// 1. Insert and get the auto-generated id
	u, err := s.repo.Create(ctx, longURL)
	if err != nil {
		return "", err
	}

	// 2. Encode the id to base62
	shortCode := shortener.Encode(uint64(u.ID))

	// 3. Update the row with the generated code
	if err := s.repo.SetShortCode(ctx, u.ID, shortCode); err != nil {
		return "", err
	}

	return shortCode, nil
}

// Resolve looks up a short code and returns the original long URL.
func (s *URLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	u, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}
	return u.LongURL, nil
}

func isValidURL(s string) bool {
	if s == "" {
		return false
	}
	parsed, err := url.Parse(s)
	if err != nil {
		return false
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return false
	}
	if parsed.Host == "" {
		return false
	}
	return true
}
