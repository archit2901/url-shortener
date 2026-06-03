package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/handlers"
	"github.com/archit2901/url-shortener/backend/internal/observability"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

var version = "dev"

func main() {
	_ = godotenv.Load("../.env")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	flushSentry, err := observability.InitSentry(version)
	if err != nil {
		logger.Error("failed to init sentry", "error", err)
		os.Exit(1)
	}
	defer flushSentry()
	logger.Info("sentry initialized", "version", version)

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		logger.Error("DATABASE_URL not set")
		os.Exit(1)
	}

	redisURL := os.Getenv("REDIS_URL")
	if redisURL == "" {
		logger.Error("REDIS_URL not set")
		os.Exit(1)
	}

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	ctx := context.Background()

	// Postgres
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		logger.Error("failed to create connection pool", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Redis
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		logger.Error("invalid REDIS_URL", "error", err)
		os.Exit(1)
	}
	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to ping redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis")

	// Wire up the layers
	urlCache := cache.NewURLCache(redisClient, 24*time.Hour)
	urlRepo := repository.NewURLRepository(pool)
	urlService := services.NewURLService(urlRepo, urlCache, logger)
	urlHandler := handlers.NewURLHandler(urlService, baseURL)

	r := gin.Default()

	r.Use(sentrygin.New(sentrygin.Options{
		Repanic: true,
	}))

	r.GET("/health", func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "database: " + err.Error(),
			})
			return
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  "redis: " + err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/api/shorten", urlHandler.Shorten)
	r.GET("/:code", urlHandler.Redirect)

	logger.Info("starting server", "addr", ":8080")
	if err := r.Run(":8080"); err != nil {
		logger.Error("server failed", "error", err)
		os.Exit(1)
	}
}
