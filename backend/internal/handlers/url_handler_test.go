package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/archit2901/url-shortener/backend/internal/middleware"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

type mockURLService struct {
	shortenFn func(ctx context.Context, longURL string, userID *uuid.UUID) (string, error)
	resolveFn func(ctx context.Context, shortCode string, clickCtx *services.ClickContext) (string, error)
	listFn    func(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error)
	statsFn   func(ctx context.Context, shortCode string, userID uuid.UUID) (*repository.ClickStats, error)
}

func (m *mockURLService) Shorten(ctx context.Context, longURL string, userID *uuid.UUID) (string, error) {
	return m.shortenFn(ctx, longURL, userID)
}
func (m *mockURLService) Resolve(ctx context.Context, shortCode string, clickCtx *services.ClickContext) (string, error) {
	return m.resolveFn(ctx, shortCode, clickCtx)
}
func (m *mockURLService) ListUserURLs(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error) {
	return m.listFn(ctx, userID, limit, offset)
}
func (m *mockURLService) GetStatsForCode(ctx context.Context, shortCode string, userID uuid.UUID) (*repository.ClickStats, error) {
	return m.statsFn(ctx, shortCode, userID)
}

// setupRouter wires up routes for testing.
// If userID is non-nil, requests will appear authenticated as that user.
func setupRouter(h *URLHandler, userID *uuid.UUID) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Auth-simulating middleware must be registered BEFORE the routes
	if userID != nil {
		uid := *userID
		r.Use(func(c *gin.Context) {
			c.Set(middleware.UserIDKey, uid)
			c.Next()
		})
	}

	r.POST("/api/shorten", h.Shorten)
	r.GET("/api/urls", h.ListMyURLs)
	r.GET("/api/urls/:code/stats", h.GetStats)
	r.GET("/:code", h.Redirect)
	return r
}

func TestShortenHandler_Success(t *testing.T) {
	svc := &mockURLService{
		shortenFn: func(ctx context.Context, longURL string, userID *uuid.UUID) (string, error) {
			return "abc", nil
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil)

	body := bytes.NewBufferString(`{"url":"https://example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/shorten", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp shortenResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "abc", resp.ShortCode)
	assert.Equal(t, "http://localhost:8080/abc", resp.ShortURL)
	assert.Equal(t, "https://example.com", resp.LongURL)
}

func TestShortenHandler_MissingURLField(t *testing.T) {
	svc := &mockURLService{}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/shorten", bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestShortenHandler_InvalidURL(t *testing.T) {
	svc := &mockURLService{
		shortenFn: func(ctx context.Context, longURL string, userID *uuid.UUID) (string, error) {
			return "", services.ErrInvalidURL
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil)

	body := bytes.NewBufferString(`{"url":"not-a-url"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/shorten", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRedirectHandler_Success(t *testing.T) {
	var capturedClickCtx *services.ClickContext
	svc := &mockURLService{
		resolveFn: func(ctx context.Context, code string, clickCtx *services.ClickContext) (string, error) {
			capturedClickCtx = clickCtx
			return "https://example.com", nil
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil)

	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	req.Header.Set("User-Agent", "test-agent/1.0")
	req.Header.Set("Referer", "https://referrer.example.com")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Location"))

	require.NotNil(t, capturedClickCtx)
	assert.Equal(t, "test-agent/1.0", capturedClickCtx.UserAgent)
	assert.Equal(t, "https://referrer.example.com", capturedClickCtx.Referrer)
}

func TestRedirectHandler_NotFound(t *testing.T) {
	svc := &mockURLService{
		resolveFn: func(ctx context.Context, code string, clickCtx *services.ClickContext) (string, error) {
			return "", repository.ErrURLNotFound
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRedirectHandler_InternalError(t *testing.T) {
	svc := &mockURLService{
		resolveFn: func(ctx context.Context, code string, clickCtx *services.ClickContext) (string, error) {
			return "", errors.New("database down")
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil)

	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestListMyURLsHandler_Success(t *testing.T) {
	userID := uuid.New()
	codeA := "abc"
	codeB := "def"
	svc := &mockURLService{
		listFn: func(ctx context.Context, gotUserID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error) {
			assert.Equal(t, userID, gotUserID)
			return []repository.URLWithStats{
				{URL: repository.URL{ID: 1, ShortCode: &codeA, LongURL: "https://example.com/a", CreatedAt: time.Now()}, ClickCount: 5},
				{URL: repository.URL{ID: 2, ShortCode: &codeB, LongURL: "https://example.com/b", CreatedAt: time.Now()}, ClickCount: 0},
			}, nil
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, &userID)

	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp urlListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Len(t, resp.URLs, 2)
	assert.Equal(t, "abc", resp.URLs[0].ShortCode)
	assert.Equal(t, int64(5), resp.URLs[0].ClickCount)
	assert.Equal(t, "http://localhost:8080/abc", resp.URLs[0].ShortURL)
}

func TestListMyURLsHandler_Unauthenticated(t *testing.T) {
	svc := &mockURLService{}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, nil) // no withAuth call

	req := httptest.NewRequest(http.MethodGet, "/api/urls", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetStatsHandler_Success(t *testing.T) {
	userID := uuid.New()
	svc := &mockURLService{
		statsFn: func(ctx context.Context, code string, gotUserID uuid.UUID) (*repository.ClickStats, error) {
			assert.Equal(t, "abc", code)
			assert.Equal(t, userID, gotUserID)
			return &repository.ClickStats{
				TotalClicks:    42,
				UniqueVisitors: 17,
				ClicksByDay: []repository.DailyClicks{
					{Day: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Clicks: 10},
					{Day: time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC), Clicks: 32},
				},
			}, nil
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, &userID)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/abc/stats", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp statsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, int64(42), resp.TotalClicks)
	assert.Equal(t, int64(17), resp.UniqueVisitors)
	require.Len(t, resp.ClicksByDay, 2)
	assert.Equal(t, "2026-06-01", resp.ClicksByDay[0].Day)
	assert.Equal(t, int64(10), resp.ClicksByDay[0].Clicks)
}

func TestGetStatsHandler_NotMyURL(t *testing.T) {
	userID := uuid.New()
	svc := &mockURLService{
		statsFn: func(ctx context.Context, code string, gotUserID uuid.UUID) (*repository.ClickStats, error) {
			return nil, services.ErrUnauthorized
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, &userID)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/abc/stats", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should return 404, not 401 — don't leak existence of other users' URLs
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestGetStatsHandler_URLNotFound(t *testing.T) {
	userID := uuid.New()
	svc := &mockURLService{
		statsFn: func(ctx context.Context, code string, gotUserID uuid.UUID) (*repository.ClickStats, error) {
			return nil, repository.ErrURLNotFound
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h, &userID)

	req := httptest.NewRequest(http.MethodGet, "/api/urls/missing/stats", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
