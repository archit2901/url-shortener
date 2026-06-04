package handlers

import (
	"context"
	"errors"
	"net/http"

	"github.com/getsentry/sentry-go"
	"github.com/gin-gonic/gin"

	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

// AuthServiceAPI is what the handler needs from the auth service.
type AuthServiceAPI interface {
	Register(ctx context.Context, email, password string) (*repository.User, error)
	Login(ctx context.Context, email, password string) (string, *repository.User, error)
}

type AuthHandler struct {
	service AuthServiceAPI
}

func NewAuthHandler(service AuthServiceAPI) *AuthHandler {
	return &AuthHandler{service: service}
}

type registerRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type authResponse struct {
	Token string  `json:"token,omitempty"`
	User  userDTO `json:"user"`
}

type userDTO struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password required"})
		return
	}

	user, err := h.service.Register(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidEmail):
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid email"})
		case errors.Is(err, services.ErrPasswordTooShort):
			c.JSON(http.StatusBadRequest, gin.H{"error": "password too short (min 8 characters)"})
		case errors.Is(err, repository.ErrEmailAlreadyExists):
			c.JSON(http.StatusConflict, gin.H{"error": "email already registered"})
		default:
			captureUnexpected(c, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "registration failed"})
		}
		return
	}

	c.JSON(http.StatusCreated, authResponse{
		User: userDTO{ID: user.ID.String(), Email: user.Email},
	})
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password required"})
		return
	}

	token, user, err := h.service.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		if errors.Is(err, services.ErrInvalidCredentials) {
			// Same 401 regardless of which credential was wrong
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
			return
		}
		captureUnexpected(c, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}

	c.JSON(http.StatusOK, authResponse{
		Token: token,
		User:  userDTO{ID: user.ID.String(), Email: user.Email},
	})
}

// captureUnexpected reports an unexpected error to Sentry with request context.
func captureUnexpected(c *gin.Context, err error) {
	if hub := sentry.GetHubFromContext(c.Request.Context()); hub != nil {
		hub.CaptureException(err)
	} else {
		sentry.CaptureException(err)
	}
}
