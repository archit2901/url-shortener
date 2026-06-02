package handlers

import (
	"errors"
	"net/http"

	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
	"github.com/gin-gonic/gin"
)

type URLHandler struct {
	service *services.URLService
	baseURL string
}

func NewURLHandler(service *services.URLService, baseURL string) *URLHandler {
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

// Shorten handles POST /api/shorten.
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
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to shorten url"})
		return
	}

	c.JSON(http.StatusCreated, shortenResponse{
		ShortCode: shortCode,
		ShortURL:  h.baseURL + "/" + shortCode,
		LongURL:   req.URL,
	})
}

// Redirect handles GET /:code.
func (h *URLHandler) Redirect(c *gin.Context) {
	code := c.Param("code")

	longURL, err := h.service.Resolve(c.Request.Context(), code)
	if err != nil {
		if errors.Is(err, repository.ErrURLNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve url"})
		return
	}

	c.Redirect(http.StatusFound, longURL)
}
