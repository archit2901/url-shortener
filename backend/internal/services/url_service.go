package services

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/google/uuid"

	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/shortener"
)

var ErrInvalidURL = errors.New("invalid url")

type URLRepository interface {
	Create(ctx context.Context, longURL string, userID *uuid.UUID) (*repository.URL, error)
	SetShortCode(ctx context.Context, id int64, code string) error
	GetByShortCode(ctx context.Context, code string) (*repository.URL, error)
}

type URLCache interface {
	Get(ctx context.Context, shortCode string) (string, error)
	Set(ctx context.Context, shortCode, longURL string) error
	Delete(ctx context.Context, shortCode string) error
}

type URLService struct {
	repo  URLRepository
	cache URLCache
	log   *slog.Logger
}

func NewURLService(repo URLRepository, urlCache URLCache, log *slog.Logger) *URLService {
	return &URLService{repo: repo, cache: urlCache, log: log}
}

func (s *URLService) Shorten(ctx context.Context, longURL string, userID *uuid.UUID) (string, error) {
	longURL = strings.TrimSpace(longURL)
	if !isValidURL(longURL) {
		return "", ErrInvalidURL
	}

	u, err := s.repo.Create(ctx, longURL, userID)
	if err != nil {
		return "", err
	}

	shortCode := shortener.Encode(uint64(u.ID))

	if err := s.repo.SetShortCode(ctx, u.ID, shortCode); err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, shortCode, longURL); err != nil {
		s.log.Warn("failed to populate cache after shorten", "short_code", shortCode, "error", err)
	}

	return shortCode, nil
}

func (s *URLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	cached, err := s.cache.Get(ctx, shortCode)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, cache.ErrCacheMiss) {
		s.log.Warn("cache get failed, falling back to db", "short_code", shortCode, "error", err)
	}

	u, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, shortCode, u.LongURL); err != nil {
		s.log.Warn("failed to populate cache after db lookup", "short_code", shortCode, "error", err)
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
