package services

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"strings"

	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/shortener"
)

var ErrInvalidURL = errors.New("invalid url")

type URLService struct {
	repo  *repository.URLRepository
	cache *cache.URLCache
	log   *slog.Logger
}

func NewURLService(repo *repository.URLRepository, urlCache *cache.URLCache, log *slog.Logger) *URLService {
	return &URLService{repo: repo, cache: urlCache, log: log}
}

func (s *URLService) Shorten(ctx context.Context, longURL string) (string, error) {
	longURL = strings.TrimSpace(longURL)
	if !isValidURL(longURL) {
		return "", ErrInvalidURL
	}

	u, err := s.repo.Create(ctx, longURL)
	if err != nil {
		return "", err
	}

	shortCode := shortener.Encode(uint64(u.ID))

	if err := s.repo.SetShortCode(ctx, u.ID, shortCode); err != nil {
		return "", err
	}

	// Warm the cache eagerly — a newly shortened URL is likely to be clicked soon.
	if err := s.cache.Set(ctx, shortCode, longURL); err != nil {
		// Log but don't fail the request: cache failures shouldn't break shorten
		s.log.Warn("failed to populate cache after shorten", "short_code", shortCode, "error", err)
	}

	return shortCode, nil
}

func (s *URLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	// 1. Try the cache first
	cached, err := s.cache.Get(ctx, shortCode)
	if err == nil {
		return cached, nil
	}
	if !errors.Is(err, cache.ErrCacheMiss) {
		// Some other Redis error: log it but continue to DB (graceful degradation)
		s.log.Warn("cache get failed, falling back to db", "short_code", shortCode, "error", err)
	}

	// 2. Cache miss (or Redis error): look up in Postgres
	u, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return "", err
	}

	// 3. Populate the cache for next time
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
