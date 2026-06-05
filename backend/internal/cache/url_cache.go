package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/redis/go-redis/v9"
)

var ErrCacheMiss = errors.New("cache miss")

type URLCache struct {
	client *redis.Client
	ttl    time.Duration
}

func NewURLCache(client *redis.Client, ttl time.Duration) *URLCache {
	return &URLCache{client: client, ttl: ttl}
}

func (c *URLCache) keyFor(shortCode string) string {
	return fmt.Sprintf("url:%s", shortCode)
}

func (c *URLCache) Get(ctx context.Context, shortCode string) (string, error) {
	span := sentry.StartSpan(ctx, "cache.get", sentry.WithDescription("redis GET url:"+shortCode))
	defer span.Finish()

	val, err := c.client.Get(ctx, c.keyFor(shortCode)).Result()
	if errors.Is(err, redis.Nil) {
		span.SetTag("cache.hit", "false")
		return "", ErrCacheMiss
	}
	if err != nil {
		span.SetTag("cache.error", "true")
		return "", err
	}
	span.SetTag("cache.hit", "true")
	return val, nil
}

func (c *URLCache) Set(ctx context.Context, shortCode, longURL string) error {
	span := sentry.StartSpan(ctx, "cache.set", sentry.WithDescription("redis SET url:"+shortCode))
	defer span.Finish()
	return c.client.Set(ctx, c.keyFor(shortCode), longURL, c.ttl).Err()
}

func (c *URLCache) Delete(ctx context.Context, shortCode string) error {
	span := sentry.StartSpan(ctx, "cache.delete", sentry.WithDescription("redis DEL url:"+shortCode))
	defer span.Finish()
	return c.client.Del(ctx, c.keyFor(shortCode)).Err()
}
