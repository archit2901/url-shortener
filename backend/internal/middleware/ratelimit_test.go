package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

// fakeRedis is an in-memory stand-in for the methods we use.
// Lets us test rate limit behavior without a real Redis.
type fakeRedis struct {
	mu      sync.Mutex
	counts  map[string]int64
	ttls    map[string]time.Duration
	incrErr error
}

func newFakeRedis() *fakeRedis {
	return &fakeRedis{
		counts: make(map[string]int64),
		ttls:   make(map[string]time.Duration),
	}
}

func (f *fakeRedis) Incr(ctx context.Context, key string) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.incrErr != nil {
		cmd.SetErr(f.incrErr)
		return cmd
	}
	f.counts[key]++
	cmd.SetVal(f.counts[key])
	return cmd
}

func (f *fakeRedis) Expire(ctx context.Context, key string, ttl time.Duration) *redis.BoolCmd {
	cmd := redis.NewBoolCmd(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ttls[key] = ttl
	cmd.SetVal(true)
	return cmd
}

func setupGin() {
	gin.SetMode(gin.TestMode)
}

func TestRateLimiter_PerIP_BelowLimit(t *testing.T) {
	setupGin()
	rdb := newFakeRedis()
	rl := NewRateLimiter(rdb, RateLimiterConfig{Limit: 3, Window: time.Minute, Prefix: "ratelimit"})

	r := gin.New()
	r.Use(rl.PerIP())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	for i := 1; i <= 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code, "request %d should pass", i)
		assert.Equal(t, "3", rec.Header().Get("X-RateLimit-Limit"))
	}
}

func TestRateLimiter_PerIP_BlocksAfterLimit(t *testing.T) {
	setupGin()
	rdb := newFakeRedis()
	rl := NewRateLimiter(rdb, RateLimiterConfig{Limit: 2, Window: time.Minute, Prefix: "ratelimit"})

	r := gin.New()
	r.Use(rl.PerIP())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	hit := func(ip string) int {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusOK, hit("1.2.3.4"))
	assert.Equal(t, http.StatusOK, hit("1.2.3.4"))
	assert.Equal(t, http.StatusTooManyRequests, hit("1.2.3.4"), "3rd should be limited")
	// A different IP gets its own bucket
	assert.Equal(t, http.StatusOK, hit("5.6.7.8"))
}

func TestRateLimiter_RedisError_FailsOpen(t *testing.T) {
	setupGin()
	rdb := newFakeRedis()
	rdb.incrErr = errors.New("redis exploded")
	rl := NewRateLimiter(rdb, RateLimiterConfig{Limit: 1, Window: time.Minute, Prefix: "ratelimit"})

	r := gin.New()
	r.Use(rl.PerIP())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	// Even with limit=1, redis errors → fail open → all pass
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "should fail open on redis error")
	}
}

func TestRateLimiter_PerUser_UsesUserID(t *testing.T) {
	setupGin()
	rdb := newFakeRedis()
	rl := NewRateLimiter(rdb, RateLimiterConfig{Limit: 2, Window: time.Minute, Prefix: "ratelimit"})

	user1 := uuid.New()
	user2 := uuid.New()

	r := gin.New()
	// Inject a user_id like RequireAuth would
	r.Use(func(c *gin.Context) {
		switch c.GetHeader("X-Test-User") {
		case "user1":
			c.Set(UserIDKey, user1)
		case "user2":
			c.Set(UserIDKey, user2)
		}
		c.Next()
	})
	r.Use(rl.PerUser())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	hitAs := func(user string) int {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.2.3.4:1234"
		if user != "" {
			req.Header.Set("X-Test-User", user)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// user1 hits limit
	assert.Equal(t, http.StatusOK, hitAs("user1"))
	assert.Equal(t, http.StatusOK, hitAs("user1"))
	assert.Equal(t, http.StatusTooManyRequests, hitAs("user1"))

	// user2 has its own bucket
	assert.Equal(t, http.StatusOK, hitAs("user2"))
	assert.Equal(t, http.StatusOK, hitAs("user2"))
	assert.Equal(t, http.StatusTooManyRequests, hitAs("user2"))
}

func TestRateLimiter_PerUser_FallsBackToIP(t *testing.T) {
	setupGin()
	rdb := newFakeRedis()
	rl := NewRateLimiter(rdb, RateLimiterConfig{Limit: 2, Window: time.Minute, Prefix: "ratelimit"})

	r := gin.New()
	r.Use(rl.PerUser())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	// No user_id → falls back to IP
	hit := func(ip string) int {
		req := httptest.NewRequest("GET", "/test", nil)
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	assert.Equal(t, http.StatusOK, hit("1.2.3.4"))
	assert.Equal(t, http.StatusOK, hit("1.2.3.4"))
	assert.Equal(t, http.StatusTooManyRequests, hit("1.2.3.4"))
}

func TestRateLimiter_SetsTTLOnFirstRequest(t *testing.T) {
	setupGin()
	rdb := newFakeRedis()
	rl := NewRateLimiter(rdb, RateLimiterConfig{Limit: 5, Window: 30 * time.Second, Prefix: "ratelimit"})

	r := gin.New()
	r.Use(rl.PerIP())
	r.GET("/test", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "1.2.3.4:1234"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	rdb.mu.Lock()
	defer rdb.mu.Unlock()
	ttl, ok := rdb.ttls["ratelimit:1.2.3.4"]
	assert.True(t, ok, "TTL should be set on first request")
	assert.Equal(t, 30*time.Second, ttl)
}
