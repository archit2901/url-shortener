package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/archit2901/url-shortener/backend/internal/auth"
)

// Context keys for storing user data in the request context.
const (
	UserIDKey    = "user_id"
	UserEmailKey = "user_email"
)

// RequireAuth returns middleware that validates JWTs and rejects unauthorized requests.
func RequireAuth(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := extractAndValidate(c, authSvc)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
			return
		}
		c.Set(UserIDKey, claims.UserID)
		c.Set(UserEmailKey, claims.Email)
		c.Next()
	}
}

// OptionalAuth returns middleware that extracts user info if a valid token is present
// but doesn't reject anonymous requests.
func OptionalAuth(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		claims, err := extractAndValidate(c, authSvc)
		if err == nil {
			c.Set(UserIDKey, claims.UserID)
			c.Set(UserEmailKey, claims.Email)
		}
		c.Next()
	}
}

func extractAndValidate(c *gin.Context, authSvc *auth.Service) (*auth.Claims, error) {
	header := c.GetHeader("Authorization")
	if header == "" {
		return nil, errors.New("missing authorization header")
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return nil, errors.New("invalid authorization format")
	}
	claims, err := authSvc.ValidateToken(parts[1])
	if err != nil {
		if errors.Is(err, auth.ErrTokenExpired) {
			return nil, errors.New("token expired")
		}
		return nil, errors.New("invalid token")
	}
	return claims, nil
}
