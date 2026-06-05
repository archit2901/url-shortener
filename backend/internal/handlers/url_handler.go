package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/archit2901/url-shortener/backend/internal/middleware"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

type URLServiceAPI interface {
	Shorten(ctx context.Context, longURL string, userID *uuid.UUID) (string, error)
	Resolve(ctx context.Context, shortCode string, clickCtx *services.ClickContext) (string, error)
	ListUserURLs(ctx context.Context, userID uuid.UUID, limit, offset int) ([]repository.URLWithStats, error)
	GetStatsForCode(ctx context.Context, shortCode string, userID uuid.UUID) (*repository.ClickStats, error)
}

type URLHandler struct {
	service URLServiceAPI
	baseURL string
}

func NewURLHandler(service URLServiceAPI, baseURL string) *URLHandler {
	return &URLHandler{service: service, baseURL: baseURL}
}

type shortenRequest struct {
	URL string `json:"url" binding:"required"`
}

type shortenResponse struct {
	ShortCode string `json:"short_code"`
	ShortURL  string `json:"short_url"`
	LongURL   string `json:"long_url"`
}

type urlListItem struct {
	ShortCode  string `json:"short_code"`
	ShortURL   string `json:"short_url"`
	LongURL    string `json:"long_url"`
	ClickCount int64  `json:"click_count"`
	CreatedAt  string `json:"created_at"`
}

type urlListResponse struct {
	URLs   []urlListItem `json:"urls"`
	Limit  int           `json:"limit"`
	Offset int           `json:"offset"`
}

type dailyClickDTO struct {
	Day    string `json:"day"`
	Clicks int64  `json:"clicks"`
}

type statsResponse struct {
	TotalClicks    int64           `json:"total_clicks"`
	UniqueVisitors int64           `json:"unique_visitors"`
	ClicksByDay    []dailyClickDTO `json:"clicks_by_day"`
}

func (h *URLHandler) Shorten(c *gin.Context) {
	var req shortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url field is required"})
		return
	}

	var userID *uuid.UUID
	if v, ok := c.Get(middleware.UserIDKey); ok {
		if id, ok := v.(uuid.UUID); ok {
			userID = &id
		}
	}

	shortCode, err := h.service.Shorten(c.Request.Context(), req.URL, userID)
	if err != nil {
		if errors.Is(err, services.ErrInvalidURL) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
			return
		}
		captureUnexpected(c, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to shorten url"})
		return
	}

	c.JSON(http.StatusCreated, shortenResponse{
		ShortCode: shortCode,
		ShortURL:  h.baseURL + "/" + shortCode,
		LongURL:   req.URL,
	})
}

func (h *URLHandler) Redirect(c *gin.Context) {
	code := c.Param("code")

	clickCtx := &services.ClickContext{
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		Referrer:  c.Request.Referer(),
	}

	longURL, err := h.service.Resolve(c.Request.Context(), code, clickCtx)
	if err != nil {
		if errors.Is(err, repository.ErrURLNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
			return
		}
		captureUnexpected(c, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve url"})
		return
	}

	c.Redirect(http.StatusFound, longURL)
}

// ListMyURLs returns the authenticated user's URLs. Requires RequireAuth middleware.
func (h *URLHandler) ListMyURLs(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	limit := parseIntQuery(c, "limit", 20)
	offset := parseIntQuery(c, "offset", 0)

	urls, err := h.service.ListUserURLs(c.Request.Context(), userID, limit, offset)
	if err != nil {
		captureUnexpected(c, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list urls"})
		return
	}

	items := make([]urlListItem, 0, len(urls))
	for _, u := range urls {
		code := ""
		if u.ShortCode != nil {
			code = *u.ShortCode
		}
		items = append(items, urlListItem{
			ShortCode:  code,
			ShortURL:   h.baseURL + "/" + code,
			LongURL:    u.LongURL,
			ClickCount: u.ClickCount,
			CreatedAt:  u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	c.JSON(http.StatusOK, urlListResponse{
		URLs:   items,
		Limit:  limit,
		Offset: offset,
	})
}

// GetStats returns click stats for one of the user's URLs. Requires RequireAuth.
func (h *URLHandler) GetStats(c *gin.Context) {
	userID, ok := userIDFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	code := c.Param("code")
	stats, err := h.service.GetStatsForCode(c.Request.Context(), code, userID)
	if err != nil {
		// Both "not found" and "not yours" return 404 so we don't leak
		// the existence of other users' URLs.
		if errors.Is(err, repository.ErrURLNotFound) || errors.Is(err, services.ErrUnauthorized) {
			c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
			return
		}
		captureUnexpected(c, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch stats"})
		return
	}

	daily := make([]dailyClickDTO, 0, len(stats.ClicksByDay))
	for _, d := range stats.ClicksByDay {
		daily = append(daily, dailyClickDTO{
			Day:    d.Day.UTC().Format("2006-01-02"),
			Clicks: d.Clicks,
		})
	}

	c.JSON(http.StatusOK, statsResponse{
		TotalClicks:    stats.TotalClicks,
		UniqueVisitors: stats.UniqueVisitors,
		ClicksByDay:    daily,
	})
}

// userIDFromContext extracts the authenticated user's UUID, if any.
func userIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(middleware.UserIDKey)
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

// parseIntQuery reads an int from a query parameter with a default.
func parseIntQuery(c *gin.Context, key string, def int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}
