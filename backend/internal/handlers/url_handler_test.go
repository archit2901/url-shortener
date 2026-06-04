package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

type mockURLService struct {
	shortenFn func(ctx context.Context, longURL string, userID *uuid.UUID) (string, error)
	resolveFn func(ctx context.Context, shortCode string) (string, error)
}

func (m *mockURLService) Shorten(ctx context.Context, longURL string, userID *uuid.UUID) (string, error) {
	return m.shortenFn(ctx, longURL, userID)
}
func (m *mockURLService) Resolve(ctx context.Context, shortCode string) (string, error) {
	return m.resolveFn(ctx, shortCode)
}

func setupRouter(h *URLHandler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/api/shorten", h.Shorten)
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
	r := setupRouter(h)

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
	r := setupRouter(h)

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
	r := setupRouter(h)

	body := bytes.NewBufferString(`{"url":"not-a-url"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/shorten", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestRedirectHandler_Success(t *testing.T) {
	svc := &mockURLService{
		resolveFn: func(ctx context.Context, code string) (string, error) {
			return "https://example.com", nil
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Equal(t, "https://example.com", rec.Header().Get("Location"))
}

func TestRedirectHandler_NotFound(t *testing.T) {
	svc := &mockURLService{
		resolveFn: func(ctx context.Context, code string) (string, error) {
			return "", repository.ErrURLNotFound
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRedirectHandler_InternalError(t *testing.T) {
	svc := &mockURLService{
		resolveFn: func(ctx context.Context, code string) (string, error) {
			return "", errors.New("database down")
		},
	}
	h := NewURLHandler(svc, "http://localhost:8080")
	r := setupRouter(h)

	req := httptest.NewRequest(http.MethodGet, "/abc", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
