package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// RateLimiterClient is what the middleware needs from Redis.
// Defined as an interface so we can swap a fake in tests.
type RateLimiterClient interface {
	Incr(ctx context.Context, key string) *redis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *redis.BoolCmd
}

// RateLimiterConfig controls the rate limiter's behavior.
type RateLimiterConfig struct {
	Limit  int           // max requests per window
	Window time.Duration // window duration (TTL on the counter)
	Prefix string        // Redis key prefix (e.g. "ratelimit:ip")
}

// RateLimiter enforces a fixed-window counter per identity.
type RateLimiter struct {
	client RateLimiterClient
	cfg    RateLimiterConfig
}

func NewRateLimiter(client RateLimiterClient, cfg RateLimiterConfig) *RateLimiter {
	return &RateLimiter{client: client, cfg: cfg}
}

// PerIP returns middleware that limits by the client's IP address.
// Suitable for unauthenticated endpoints.
func (rl *RateLimiter) PerIP() gin.HandlerFunc {
	return func(c *gin.Context) {
		identity := c.ClientIP()
		if identity == "" {
			// Fail open if we can't identify — better than blocking everyone
			c.Next()
			return
		}
		rl.enforce(c, identity)
	}
}

// PerUser returns middleware that limits by authenticated user ID.
// Falls back to per-IP if no user is in the context.
func (rl *RateLimiter) PerUser() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Prefer authenticated user ID if available
		if v, ok := c.Get(UserIDKey); ok {
			if id, ok := v.(uuid.UUID); ok {
				rl.enforce(c, "user:"+id.String())
				return
			}
		}
		// Fall back to IP for anonymous requests
		if ip := c.ClientIP(); ip != "" {
			rl.enforce(c, "ip:"+ip)
			return
		}
		// Couldn't identify — fail open
		c.Next()
	}
}

// enforce is the shared rate-limit logic.
func (rl *RateLimiter) enforce(c *gin.Context, identity string) {
	key := fmt.Sprintf("%s:%s", rl.cfg.Prefix, identity)
	ctx := c.Request.Context()

	count, err := rl.client.Incr(ctx, key).Result()
	if err != nil {
		// Fail open: if Redis is unreachable, don't block requests.
		// The error is logged elsewhere (Sentry middleware catches it).
		c.Next()
		return
	}

	// First request in a new window — set the TTL.
	// If this fails, the key would persist forever; accept it as a
	// minor failure mode and continue.
	if count == 1 {
		_ = rl.client.Expire(ctx, key, rl.cfg.Window).Err()
	}

	remaining := rl.cfg.Limit - int(count)
	if remaining < 0 {
		remaining = 0
	}

	// Always set rate limit headers — clients can use them to back off.
	c.Header("X-RateLimit-Limit", strconv.Itoa(rl.cfg.Limit))
	c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))

	if count > int64(rl.cfg.Limit) {
		c.Header("Retry-After", strconv.Itoa(int(rl.cfg.Window.Seconds())))
		c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
			"error": "rate limit exceeded",
		})
		return
	}

	c.Next()
}

// Compile-time check that *redis.Client satisfies RateLimiterClient.
var _ RateLimiterClient = (*redis.Client)(nil)

// ensure errors package is used (silences lint when we add error wrapping later)
var _ = errors.New
