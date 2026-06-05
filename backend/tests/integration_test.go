package tests

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

// nopRecorder is a stand-in ClickRecorder for integration tests that don't
// exercise the analytics path.
type nopRecorder struct{}

func (nopRecorder) Record(repository.Click) bool { return true }

func setupTestPostgres(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	migrationsPath, err := filepath.Abs("../db/migrations")
	require.NoError(t, err)

	pgContainer, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(30*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })

	dsn, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	require.NoError(t, applyAllMigrations(ctx, pool, migrationsPath))

	return pool
}

func setupTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	ctx := context.Background()

	redisContainer, err := tcredis.Run(ctx, "redis:7-alpine")
	require.NoError(t, err)
	t.Cleanup(func() { _ = redisContainer.Terminate(ctx) })

	uri, err := redisContainer.ConnectionString(ctx)
	require.NoError(t, err)

	opts, err := redis.ParseURL(uri)
	require.NoError(t, err)
	client := redis.NewClient(opts)
	t.Cleanup(func() { _ = client.Close() })

	return client
}

func applyAllMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	var ups []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".up.sql") {
			ups = append(ups, e.Name())
		}
	}
	sort.Strings(ups)

	for _, name := range ups {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(data)); err != nil {
			return err
		}
	}
	return nil
}

func TestIntegration_ShortenAndResolve(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	pool := setupTestPostgres(t)
	redisClient := setupTestRedis(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	repo := repository.NewURLRepository(pool)
	urlCache := cache.NewURLCache(redisClient, time.Hour)
	svc := services.NewURLService(repo, nil, urlCache, nopRecorder{}, logger)

	ctx := context.Background()

	code, err := svc.Shorten(ctx, "https://example.com/integration", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, code)

	got, err := svc.Resolve(ctx, code, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/integration", got)

	require.NoError(t, urlCache.Delete(ctx, code))
	got, err = svc.Resolve(ctx, code, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/integration", got)

	cached, err := urlCache.Get(ctx, code)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/integration", cached)
}

func TestIntegration_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}

	pool := setupTestPostgres(t)
	redisClient := setupTestRedis(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	repo := repository.NewURLRepository(pool)
	urlCache := cache.NewURLCache(redisClient, time.Hour)
	svc := services.NewURLService(repo, nil, urlCache, nopRecorder{}, logger)

	_, err := svc.Resolve(context.Background(), "definitely-not-real", nil)
	assert.ErrorIs(t, err, repository.ErrURLNotFound)
}
