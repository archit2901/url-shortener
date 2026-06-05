package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"

	"github.com/archit2901/url-shortener/backend/internal/analytics"
	"github.com/archit2901/url-shortener/backend/internal/auth"
	"github.com/archit2901/url-shortener/backend/internal/cache"
	"github.com/archit2901/url-shortener/backend/internal/handlers"
	"github.com/archit2901/url-shortener/backend/internal/middleware"
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

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		logger.Error("JWT_SECRET not set")
		os.Exit(1)
	}

	jwtExpiryHours := 24
	if v := os.Getenv("JWT_EXPIRY_HOURS"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			jwtExpiryHours = parsed
		}
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

	// Repositories
	userRepo := repository.NewUserRepository(pool)
	urlRepo := repository.NewURLRepository(pool)
	clickRepo := repository.NewClickRepository(pool)

	// Analytics worker — must be started before the URL service uses it
	analyticsWorker := analytics.New(analytics.DefaultConfig(), clickRepo, logger)
	analyticsWorker.Start()

	// Auth
	authSvc := auth.NewService(jwtSecret, time.Duration(jwtExpiryHours)*time.Hour)
	authService := services.NewAuthService(userRepo, authSvc)
	authHandler := handlers.NewAuthHandler(authService)

	// URL service depends on the worker
	urlCache := cache.NewURLCache(redisClient, 24*time.Hour)
	urlService := services.NewURLService(urlRepo, urlCache, analyticsWorker, logger)
	urlHandler := handlers.NewURLHandler(urlService, baseURL)

	r := gin.Default()
	r.Use(sentrygin.New(sentrygin.Options{Repanic: true}))

	r.GET("/health", func(c *gin.Context) {
		ctx := c.Request.Context()
		if err := pool.Ping(ctx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "database: " + err.Error()})
			return
		}
		if err := redisClient.Ping(ctx).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unhealthy", "error": "redis: " + err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/api/auth/register", authHandler.Register)
	r.POST("/api/auth/login", authHandler.Login)

	r.POST("/api/shorten", middleware.OptionalAuth(authSvc), urlHandler.Shorten)
	r.GET("/:code", urlHandler.Redirect)

	// HTTP server with graceful shutdown support
	srv := &http.Server{
		Addr:    ":8080",
		Handler: r,
	}

	// Run the server in a goroutine so we can listen for shutdown signals
	go func() {
		logger.Info("starting server", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for SIGINT (Ctrl+C) or SIGTERM (sent by Docker/Kubernetes on stop)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutdown signal received, draining...")

	// Step 1: Stop accepting new HTTP requests, finish in-flight ones (10s budget)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("http shutdown failed", "error", err)
	} else {
		logger.Info("http server stopped")
	}

	// Step 2: Drain the analytics worker (10s budget for any queued events)
	workerCtx, cancelWorker := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelWorker()
	if err := analyticsWorker.Stop(workerCtx); err != nil {
		logger.Error("analytics worker stop failed", "error", err)
	}

	logger.Info("shutdown complete")
}
