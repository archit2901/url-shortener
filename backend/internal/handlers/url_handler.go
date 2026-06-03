package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"
)

// URLServiceAPI is what the handler needs from the service layer.
type URLServiceAPI interface {
	Shorten(ctx context.Context, longURL string) (string, error)
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

	shortCode, err := h.service.Shorten(c.Request.Context(), req.URL)
	if err != nil {
		if errors.Is(err, services.ErrInvalidURL) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
			return
		}
		// Unexpected error: report to Sentry with request context
		if hub := sentry.GetHubFromContext(c.Request.Context()); hub != nil {
			hub.CaptureException(err)
		} else {
			sentry.CaptureException(err)
		}
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
		// Unexpected error
		if hub := sentry.GetHubFromContext(c.Request.Context()); hub != nil {
			hub.CaptureException(err)
		} else {
			sentry.CaptureException(err)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve url"})
		return
	}

	c.Redirect(http.StatusFound, longURL)
}
