package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/repository"
)

type mockRepo struct {
	createFn       func(ctx context.Context, longURL string, userID *uuid.UUID) (*repository.URL, error)
	setShortCodeFn func(ctx context.Context, id int64, code string) error
	getByCodeFn    func(ctx context.Context, code string) (*repository.URL, error)
}

func (m *mockRepo) Create(ctx context.Context, longURL string, userID *uuid.UUID) (*repository.URL, error) {
	return m.createFn(ctx, longURL, userID)
}
func (m *mockRepo) SetShortCode(ctx context.Context, id int64, code string) error {
	return m.setShortCodeFn(ctx, id, code)
}
func (m *mockRepo) GetByShortCode(ctx context.Context, code string) (*repository.URL, error) {
	return m.getByCodeFn(ctx, code)
}

type mockCache struct {
	store    map[string]string
	getErr   error
	setErr   error
	getCalls int
	setCalls int
}

func newMockCache() *mockCache {
	return &mockCache{store: make(map[string]string)}
}

func (m *mockCache) Get(ctx context.Context, code string) (string, error) {
	m.getCalls++
	if m.getErr != nil {
		return "", m.getErr
	}
	v, ok := m.store[code]
	if !ok {
		return "", cache.ErrCacheMiss
	}
	return v, nil
}

func (m *mockCache) Set(ctx context.Context, code, longURL string) error {
	m.setCalls++
	if m.setErr != nil {
		return m.setErr
	}
	m.store[code] = longURL
	return nil
}

func (m *mockCache) Delete(ctx context.Context, code string) error {
	delete(m.store, code)
	return nil
}

func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestShorten_Success(t *testing.T) {
	repo := &mockRepo{
		createFn: func(ctx context.Context, longURL string, userID *uuid.UUID) (*repository.URL, error) {
			return &repository.URL{ID: 42, LongURL: longURL, CreatedAt: time.Now()}, nil
		},
		setShortCodeFn: func(ctx context.Context, id int64, code string) error {
			return nil
		},
	}
	mc := newMockCache()
	svc := NewURLService(repo, mc, silentLogger())

	code, err := svc.Shorten(context.Background(), "https://example.com", nil)

	require.NoError(t, err)
	assert.Equal(t, "G", code)
	assert.Equal(t, 1, mc.setCalls, "shorten should warm the cache")
	assert.Equal(t, "https://example.com", mc.store["G"])
}

func TestShorten_InvalidURL(t *testing.T) {
	svc := NewURLService(&mockRepo{}, newMockCache(), silentLogger())

	tests := []string{"", "not-a-url", "ftp://example.com", "   "}
	for _, badURL := range tests {
		_, err := svc.Shorten(context.Background(), badURL, nil)
		assert.ErrorIs(t, err, ErrInvalidURL, "should reject %q", badURL)
	}
}

func TestResolve_CacheHit(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			t.Fatal("repository should not be called on cache hit")
			return nil, nil
		},
	}
	mc := newMockCache()
	mc.store["abc"] = "https://cached.com"

	svc := NewURLService(repo, mc, silentLogger())
	got, err := svc.Resolve(context.Background(), "abc")

	require.NoError(t, err)
	assert.Equal(t, "https://cached.com", got)
	assert.Equal(t, 1, mc.getCalls)
}

func TestResolve_CacheMiss_PopulatesCache(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return &repository.URL{ID: 1, LongURL: "https://fromdb.com"}, nil
		},
	}
	mc := newMockCache()

	svc := NewURLService(repo, mc, silentLogger())
	got, err := svc.Resolve(context.Background(), "1")

	require.NoError(t, err)
	assert.Equal(t, "https://fromdb.com", got)
	assert.Equal(t, "https://fromdb.com", mc.store["1"], "miss should populate cache")
}

func TestResolve_NotFound(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return nil, repository.ErrURLNotFound
		},
	}
	svc := NewURLService(repo, newMockCache(), silentLogger())

	_, err := svc.Resolve(context.Background(), "nope")
	assert.ErrorIs(t, err, repository.ErrURLNotFound)
}

func TestResolve_CacheError_FallsBackToDB(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return &repository.URL{ID: 1, LongURL: "https://db.com"}, nil
		},
	}
	mc := newMockCache()
	mc.getErr = errors.New("redis is down")

	svc := NewURLService(repo, mc, silentLogger())
	got, err := svc.Resolve(context.Background(), "1")

	require.NoError(t, err, "cache failure should not break resolution")
	assert.Equal(t, "https://db.com", got)
}
