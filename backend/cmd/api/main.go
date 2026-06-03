package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/archit2901/url-shortener/backend/internal/handlers"
	"github.com/archit2901/url-shortener/backend/internal/observability"
	"github.com/archit2901/url-shortener/backend/internal/repository"
	"github.com/archit2901/url-shortener/backend/internal/services"
)

// version is set at build time via -ldflags. Defaults to "dev" for local runs.
var version = "dev"

func main() {
	_ = godotenv.Load("../.env")

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Initialize Sentry first so it can catch errors from the rest of init
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

	baseURL := os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	ctx := context.Background()
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

	urlRepo := repository.NewURLRepository(pool)
	urlService := services.NewURLService(urlRepo)
	urlHandler := handlers.NewURLHandler(urlService, baseURL)

	r := gin.Default()

	// Sentry middleware: captures panics and creates a transaction per request.
	// Must come early so it sees every request.
	r.Use(sentrygin.New(sentrygin.Options{
		Repanic: true, // re-throw after capturing so gin.Recovery can return 500
	}))

	r.GET("/health", func(c *gin.Context) {
		if err := pool.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"error":  err.Error(),
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
