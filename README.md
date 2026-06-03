# URL Shortener

A production-grade URL shortener built with Go, PostgreSQL, and Redis. Features distributed-systems-friendly design patterns including cache-aside, structured observability with Sentry, and a layered architecture suitable for horizontal scaling.

> **Status:** Active development — building out features iteratively as a learning project. See [Roadmap](#roadmap) for what's done and what's next.

## Features

- **Fast redirects** via Redis cache-aside pattern (~1ms cache hit vs ~5ms cold)
- **Base62 short-code generation** from sequential database IDs (no collisions, no wasted space)
- **Structured observability** with Sentry for error tracking and (coming soon) performance tracing
- **Clean layered architecture** (handlers → services → repositories) for testability and growth
- **Graceful degradation** — Redis or DB hiccups don't break the user experience
- **Privacy-conscious** error reporting with query string scrubbing

## Tech stack

- **Language:** Go 1.23+
- **Web framework:** Gin
- **Database:** PostgreSQL 16 (via pgx)
- **Cache:** Redis 7
- **Observability:** Sentry
- **Local infra:** Docker Compose

## Architecture

The system follows a cache-aside pattern for read-heavy redirect traffic, with async-friendly design ready for horizontal scaling.
[Client] → [Go API (Gin)] → [Redis cache]
↓ miss        ↘
[PostgreSQL]  → [Sentry]

Key design decisions are documented in [`docs/architecture.md`](docs/architecture.md) (coming soon).

## Getting started

### Prerequisites

- Go 1.23+
- Docker Desktop or OrbStack
- `golang-migrate` (`brew install golang-migrate` on macOS)

### Setup

```bash
# Clone the repo
git clone git@github.com:yourusername/url-shortener.git
cd url-shortener

# Copy environment template and fill in secrets
cp .env.example .env
# Edit .env to add your Sentry DSN (optional — works without it)

# Start Postgres and Redis
docker compose up -d

# Run database migrations
export $(cat .env | xargs)
migrate -path backend/db/migrations -database "$DATABASE_URL" up

# Run the server
cd backend
go run ./cmd/api
```

The API will be available at `http://localhost:8080`.

### Try it out

```bash
# Shorten a URL
curl -X POST http://localhost:8080/api/shorten \
  -H "Content-Type: application/json" \
  -d '{"url": "https://github.com"}'

# Response: {"short_code":"1","short_url":"http://localhost:8080/1","long_url":"https://github.com"}

# Follow the redirect
curl -i http://localhost:8080/1
```

## Project structure
url-shortener/
├── backend/
│   ├── cmd/api/              # API entry point
│   ├── internal/
│   │   ├── cache/            # Redis cache layer
│   │   ├── handlers/         # HTTP handlers
│   │   ├── observability/    # Sentry setup
│   │   ├── repository/       # Database access
│   │   ├── services/         # Business logic
│   │   └── shortener/        # Base62 encoder
│   └── db/migrations/        # SQL migrations
├── docker-compose.yml        # Local Postgres + Redis
└── .env.example              # Required environment variables

## Roadmap

- [x] Core URL shortening with base62 encoding
- [x] PostgreSQL persistence with migrations
- [x] Redis cache-aside for fast redirects
- [x] Sentry error tracking
- [ ] Async analytics worker for click tracking
- [ ] User authentication with JWT
- [ ] Rate limiting middleware
- [ ] API key authentication
- [ ] Custom aliases
- [ ] QR code generation
- [ ] Analytics dashboard (Next.js)
- [ ] Performance tracing with Sentry APM
- [ ] Comprehensive test suite
- [ ] Deployment to Railway/Fly.io

## Learnings

This project is a deep dive into backend engineering with Go. Key concepts explored:

- **System design:** read-heavy caching, async event processing, graceful degradation
- **Go idioms:** dependency injection, error wrapping, context propagation
- **Database design:** schema normalization, indexes, migrations
- **Observability:** structured logging, error tracking, request tracing
- **Operational concerns:** privacy-conscious error reporting, healthchecks, configuration

## License

MIT (to be added)
