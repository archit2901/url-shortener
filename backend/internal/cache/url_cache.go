package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

var ErrCacheMiss = errors.New("cache miss")

// URLCache provides a simple key/value cache for short-code → long-url mappings.
type URLCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewURLCache(client *redis.Client, ttl time.Duration) *URLCache {
	return &URLCache{client: client, ttl: ttl}
}

// keyFor builds the Redis key for a given short code.
// Prefixing with "url:" is a convention that helps when inspecting
// Redis with `redis-cli KEYS url:*`.
func (c *URLCache) keyFor(shortCode string) string {
	return fmt.Sprintf("url:%s", shortCode)
}

// Get returns the cached long URL for a short code, or ErrCacheMiss if absent.
func (c *URLCache) Get(ctx context.Context, shortCode string) (string, error) {
	val, err := c.client.Get(ctx, c.keyFor(shortCode)).Result()
	if errors.Is(err, redis.Nil) {
		return "", ErrCacheMiss
	}
	if err != nil {
		return "", err
	}
	return val, nil
}

// Set caches a short-code → long-url mapping with the configured TTL.
func (c *URLCache) Set(ctx context.Context, shortCode, longURL string) error {
	return c.client.Set(ctx, c.keyFor(shortCode), longURL, c.ttl).Err()
}

// Delete removes a cached entry (used when URLs are updated or deleted).
func (c *URLCache) Delete(ctx context.Context, shortCode string) error {
	return c.client.Del(ctx, c.keyFor(shortCode)).Err()
}
