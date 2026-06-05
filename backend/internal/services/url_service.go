package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/shortener"
	"github.com/getsentry/sentry-go"
)

var (
	ErrInvalidURL   = errors.New("invalid url")
	ErrUnauthorized = errors.New("unauthorized")
)

type URLRepository interface {
	Create(ctx context.Context, longURL string, userID *uuid.UUID) (*repository.URL, error)
	SetShortCode(ctx context.Context, id int64, code string) error
	GetByShortCode(ctx context.Context, code string) (*repository.URL, error)
	ListByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error)
}

type URLCache interface {
	Get(ctx context.Context, shortCode string) (string, error)
	Set(ctx context.Context, shortCode, longURL string) error
	Delete(ctx context.Context, shortCode string) error
}

// ClickStatsRepository is what the service needs to read click analytics.
type ClickStatsRepository interface {
	GetStatsForURL(ctx context.Context, urlID int64) (*repository.ClickStats, error)
}

// ClickRecorder is what the service needs from the analytics layer.
// Defined as an interface so we can mock it in tests and so the service
// doesn't depend on the concrete worker.
type ClickRecorder interface {
	Record(click repository.Click) bool
}

// ClickContext carries the request metadata needed to record a click.
// We keep this separate from the cache lookup so the cache hit path
// stays fast and the metadata can be nil-checked.
type ClickContext struct {
	IP        string
	UserAgent string
	Referrer  string
}

type URLService struct {
	repo      URLRepository
	statsRepo ClickStatsRepository
	cache     URLCache
	recorder  ClickRecorder
	log       *slog.Logger
}

func NewURLService(
	repo URLRepository,
	statsRepo ClickStatsRepository,
	urlCache URLCache,
	recorder ClickRecorder,
	log *slog.Logger,
) *URLService {
	return &URLService{
		repo:      repo,
		statsRepo: statsRepo,
		cache:     urlCache,
		recorder:  recorder,
		log:       log,
	}
}

func (s *URLService) Shorten(ctx context.Context, longURL string, userID *uuid.UUID) (string, error) {
	span := sentry.StartSpan(ctx, "service.shorten")
	defer span.Finish()

	longURL = strings.TrimSpace(longURL)
	if !isValidURL(longURL) {
		return "", ErrInvalidURL
	}

	u, err := s.repo.Create(ctx, longURL, userID)
	if err != nil {
		return "", err
	}

	encodeSpan := sentry.StartSpan(ctx, "encode.base62")
	shortCode := shortener.Encode(uint64(u.ID))
	encodeSpan.Finish()

	if err := s.repo.SetShortCode(ctx, u.ID, shortCode); err != nil {
		return "", err
	}

	if err := s.cache.Set(ctx, shortCode, longURL); err != nil {
		s.log.Warn("failed to populate cache after shorten", "short_code", shortCode, "error", err)
	}

	return shortCode, nil
}

// Resolve looks up a short code and returns the long URL.
// If clickCtx is non-nil, it also fires a click event for async recording.
func (s *URLService) Resolve(ctx context.Context, shortCode string, clickCtx *ClickContext) (string, error) {
	span := sentry.StartSpan(ctx, "service.resolve")
	defer span.Finish()

	var (
		longURL string
		urlID   int64
	)

	cached, err := s.cache.Get(ctx, shortCode)
	if err == nil {
		longURL = cached
		span.SetTag("cache.hit", "true")
		if clickCtx != nil && s.recorder != nil {
			u, lookupErr := s.repo.GetByShortCode(ctx, shortCode)
			if lookupErr != nil {
				s.log.Warn("cache hit but db lookup for id failed",
					"short_code", shortCode, "error", lookupErr)
				return longURL, nil
			}
			urlID = u.ID
		}
	} else {
		span.SetTag("cache.hit", "false")
		if !errors.Is(err, cache.ErrCacheMiss) {
			s.log.Warn("cache get failed, falling back to db", "short_code", shortCode, "error", err)
		}

		u, err := s.repo.GetByShortCode(ctx, shortCode)
		if err != nil {
			return "", err
		}
		longURL = u.LongURL
		urlID = u.ID

		if cacheErr := s.cache.Set(ctx, shortCode, u.LongURL); cacheErr != nil {
			s.log.Warn("failed to populate cache after db lookup", "short_code", shortCode, "error", cacheErr)
		}
	}

	if clickCtx != nil && s.recorder != nil {
		s.recorder.Record(repository.Click{
			URLID:     urlID,
			IPHash:    hashIP(clickCtx.IP),
			UserAgent: clickCtx.UserAgent,
			Referrer:  clickCtx.Referrer,
			ClickedAt: time.Now(),
		})
	}

	return longURL, nil
}

// hashIP returns a SHA-256 hex hash of an IP address. We hash for privacy:
// we can still count unique visitors (same IP → same hash) without storing
// raw addresses.
func hashIP(ip string) string {
	if ip == "" {
		return ""
	}
	h := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(h[:])
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

// ListUserURLs returns paginated URLs owned by a user with their click counts.
func (s *URLService) ListUserURLs(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error) {
	// Defensive bounds — never trust the caller blindly
	if limit < 1 || limit > 100 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	return s.repo.ListByUserID(ctx, userID, limit, offset)
}

// GetStatsForCode returns click stats for a short code, but only if the
// requesting user owns the URL. Returns ErrUnauthorized otherwise.
func (s *URLService) GetStatsForCode(ctx context.Context, shortCode string, userID uuid.UUID) (*repository.ClickStats, error) {
	// First fetch the URL to check ownership
	u, err := s.repo.GetByShortCode(ctx, shortCode)
	if err != nil {
		return nil, err
	}

	// Ownership check — URL must belong to the requesting user
	if u.UserID == nil || *u.UserID != userID {
		// Return ErrUnauthorized rather than the URL info to avoid leaking
		// whether the URL exists. From the user's perspective, "not yours"
		// and "doesn't exist" should look the same.
		return nil, ErrUnauthorized
	}

	return s.statsRepo.GetStatsForURL(ctx, u.ID)
}
