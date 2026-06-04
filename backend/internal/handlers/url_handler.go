package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/archit2901/url-shortener/backend/internal/middleware"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

type URLServiceAPI interface {
	Shorten(ctx context.Context, longURL string, userID *uuid.UUID) (string, error)
	Resolve(ctx context.Context, shortCode string) (string, error)
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

func (h *URLHandler) Shorten(c *gin.Context) {
	var req shortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "url field is required"})
		return
	}

	// Extract user ID if authenticated (via OptionalAuth middleware)
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

	longURL, err := h.service.Resolve(c.Request.Context(), code)
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
