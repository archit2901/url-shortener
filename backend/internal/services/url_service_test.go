package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
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
	listByUserFn   func(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error)
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
func (m *mockRepo) ListByUserID(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error) {
	if m.listByUserFn == nil {
		return nil, nil
	}
	return m.listByUserFn(ctx, userID, limit, offset)
}

type mockStatsRepo struct {
	statsFn func(ctx context.Context, urlID int64) (*repository.ClickStats, error)
}

func (m *mockStatsRepo) GetStatsForURL(ctx context.Context, urlID int64) (*repository.ClickStats, error) {
	if m.statsFn == nil {
		return &repository.ClickStats{}, nil
	}
	return m.statsFn(ctx, urlID)
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

type mockRecorder struct {
	mu     sync.Mutex
	clicks []repository.Click
}

func (m *mockRecorder) Record(c repository.Click) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clicks = append(m.clicks, c)
	return true
}

func (m *mockRecorder) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.clicks)
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
	svc := NewURLService(repo, &mockStatsRepo{}, mc, &mockRecorder{}, silentLogger())

	code, err := svc.Shorten(context.Background(), "https://example.com", nil)

	require.NoError(t, err)
	assert.Equal(t, "G", code)
	assert.Equal(t, 1, mc.setCalls, "shorten should warm the cache")
	assert.Equal(t, "https://example.com", mc.store["G"])
}

func TestShorten_InvalidURL(t *testing.T) {
	svc := NewURLService(&mockRepo{}, &mockStatsRepo{}, newMockCache(), &mockRecorder{}, silentLogger())

	tests := []string{"", "not-a-url", "ftp://example.com", "   "}
	for _, badURL := range tests {
		_, err := svc.Shorten(context.Background(), badURL, nil)
		assert.ErrorIs(t, err, ErrInvalidURL, "should reject %q", badURL)
	}
}

func TestResolve_CacheHit_RecordsClick(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return &repository.URL{ID: 7, LongURL: "https://cached.com"}, nil
		},
	}
	mc := newMockCache()
	mc.store["abc"] = "https://cached.com"
	rec := &mockRecorder{}

	svc := NewURLService(repo, &mockStatsRepo{}, mc, rec, silentLogger())

	got, err := svc.Resolve(context.Background(), "abc", &ClickContext{IP: "1.2.3.4"})

	require.NoError(t, err)
	assert.Equal(t, "https://cached.com", got)
	assert.Equal(t, 1, rec.count(), "should have recorded one click")
}

func TestResolve_CacheHit_NoClickContext_NoRecord(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			t.Fatal("repository should not be called when clickCtx is nil and cache hit")
			return nil, nil
		},
	}
	mc := newMockCache()
	mc.store["abc"] = "https://cached.com"
	rec := &mockRecorder{}

	svc := NewURLService(repo, &mockStatsRepo{}, mc, rec, silentLogger())

	got, err := svc.Resolve(context.Background(), "abc", nil)

	require.NoError(t, err)
	assert.Equal(t, "https://cached.com", got)
	assert.Equal(t, 0, rec.count())
}

func TestResolve_CacheMiss_PopulatesCacheAndRecords(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return &repository.URL{ID: 1, LongURL: "https://fromdb.com"}, nil
		},
	}
	mc := newMockCache()
	rec := &mockRecorder{}

	svc := NewURLService(repo, &mockStatsRepo{}, mc, rec, silentLogger())
	got, err := svc.Resolve(context.Background(), "1", &ClickContext{IP: "1.2.3.4"})

	require.NoError(t, err)
	assert.Equal(t, "https://fromdb.com", got)
	assert.Equal(t, "https://fromdb.com", mc.store["1"], "miss should populate cache")
	assert.Equal(t, 1, rec.count())
}

func TestResolve_NotFound(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return nil, repository.ErrURLNotFound
		},
	}
	svc := NewURLService(repo, &mockStatsRepo{}, newMockCache(), &mockRecorder{}, silentLogger())

	_, err := svc.Resolve(context.Background(), "nope", &ClickContext{IP: "1.2.3.4"})
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
	rec := &mockRecorder{}

	svc := NewURLService(repo, &mockStatsRepo{}, mc, rec, silentLogger())
	got, err := svc.Resolve(context.Background(), "1", nil)

	require.NoError(t, err, "cache failure should not break resolution")
	assert.Equal(t, "https://db.com", got)
}

func TestResolve_RecordsHashedIP(t *testing.T) {
	repo := &mockRepo{
		getByCodeFn: func(ctx context.Context, code string) (*repository.URL, error) {
			return &repository.URL{ID: 1, LongURL: "https://example.com"}, nil
		},
	}
	mc := newMockCache()
	rec := &mockRecorder{}

	svc := NewURLService(repo, &mockStatsRepo{}, mc, rec, silentLogger())
	_, err := svc.Resolve(context.Background(), "1", &ClickContext{IP: "1.2.3.4"})
	require.NoError(t, err)

	require.Equal(t, 1, rec.count())
	rec.mu.Lock()
	defer rec.mu.Unlock()
	click := rec.clicks[0]
	assert.NotEqual(t, "1.2.3.4", click.IPHash, "IP should not be stored raw")
	assert.Equal(t, 64, len(click.IPHash), "SHA-256 hex hash is 64 chars")
}
