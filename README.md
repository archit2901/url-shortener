# URL Shortener

A production-grade URL shortener built in Go and Next.js, with PostgreSQL persistence, Redis caching, async analytics processing, tiered rate limiting, and end-to-end observability via Sentry.

**[Try it live →](https://url-shortener-beige-six.vercel.app)**

[![CI](https://github.com/archit2901/url-shortener/actions/workflows/ci.yml/badge.svg)](https://github.com/archit2901/url-shortener/actions/workflows/ci.yml)

---

## Quick demo

Anonymous shortening via the API:

```bash
curl -X POST https://url-shortener-production-69cd.up.railway.app/api/shorten \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}'

# {"short_code":"3d7","short_url":"https://.../3d7","long_url":"https://example.com"}
```

Visit it — instant 302 redirect, sub-millisecond on cache hit. Or use the dashboard at the link above to register, sign in, and see per-URL click analytics.

---

## What's interesting about it

- **Full-stack** — Go API with a Next.js dashboard, type-safe API client, JWT-based session persistence, real-time click charts with Recharts
- **Sub-millisecond redirects** via Redis cache-aside, verified via Sentry tracing (screenshots below)
- **Sequential ID → base62 encoding** for collision-free short codes with dense output: 3.5 trillion unique codes in 7 characters
- **Async analytics pipeline** — clicks recorded via a buffered Go channel and batched worker pool. Redirects never wait for analytics writes.
- **Tiered rate limiting** — strict per-IP limit on public endpoints (auth, redirect) to deter credential stuffing and burst abuse; generous per-user limit on authenticated endpoints. Redis-backed for horizontal scaling.
- **JWT auth with security details** — algorithm-confusion attack prevention, user enumeration prevention via uniform error responses, opaque 404s for ownership checks
- **Privacy-conscious analytics** — IPs hashed at write time (SHA-256), so unique visitor counts work without storing PII
- **Production-grade observability** — Sentry APM with manual spans, custom tags (cache hit rate), error tracking with query string scrubbing
- **Graceful shutdown** — on SIGTERM, HTTP server drains in-flight requests, then the analytics worker flushes its remaining batch before exit. No lost clicks.
- **Real test coverage** — unit + integration (with disposable Postgres + Redis via `testcontainers-go`) + handler tests, all run in CI
- **Deployed** — Railway hosts the backend with managed Postgres and Redis; Vercel hosts the frontend

---

## Tech stack

| Layer            | Choice                                | Why                                                              |
| ---------------- | ------------------------------------- | ---------------------------------------------------------------- |
| Backend language | Go 1.26                               | Concurrency primitives, single binary, fast startup              |
| HTTP framework   | Gin                                   | Mature, low overhead, good middleware ecosystem                  |
| Database         | PostgreSQL 16                         | Reliable, supports `RETURNING`, `COPY` protocol for bulk inserts |
| DB driver        | `pgx/v5`                              | Postgres-native, faster than `database/sql`                      |
| Cache            | Redis 7                               | In-memory key-value, atomic ops for rate limiting                |
| Auth             | `golang-jwt/v5` + `bcrypt`            | Standard JWT lib, bcrypt at default cost (~50ms/hash)            |
| Observability    | Sentry                                | Error tracking + APM with manual spans                           |
| Testing          | `testify` + `testcontainers-go`       | Idiomatic asserts, real DB for integration tests                 |
| Frontend         | Next.js 15 (App Router)               | Modern React, file-based routing                                 |
| Frontend UI      | Tailwind + shadcn/ui                  | Utility CSS, owned-component pattern                             |
| Charts           | Recharts                              | Composable chart primitives, SVG-based                           |
| CI               | GitHub Actions                        | Free for public repos, service containers built in               |
| Deployment       | Railway (backend) + Vercel (frontend) | Generous free tiers, monorepo support                            |

---

## Architecture

The system is organized in three tiers:

**HTTP layer.** Gin router with a middleware chain: Sentry tracing → recovery → rate limiting → optional/required auth → handler.

**Service layer.** Business logic, input validation, and orchestration. Depends on repository interfaces (not concrete types), so any layer can be swapped in tests via dependency injection.

**Data layer.** PostgreSQL is source of truth. Redis is a write-through cache for hot redirects. An async worker pool drains a buffered channel of click events and batch-inserts them via Postgres `COPY`.

### The async worker, in detail

This is the most distinctive piece of the system:

1. A redirect handler resolves the short code (cache or DB) and fires a `Click{URLID, IPHash, UserAgent, Referrer, ClickedAt}` into a buffered channel via non-blocking `select`. Returns 302 to the client immediately.
2. Three worker goroutines drain the channel, accumulating events into batches of up to 100.
3. Each batch is flushed via `pgx.CopyFrom` (Postgres COPY protocol, 5-10× faster than multi-row INSERT) when batch size is reached, or every 5 seconds, whichever comes first.
4. If the channel is full under sustained burst (>1000 in-flight), new events drop with a logged counter rather than blocking the redirect.
5. On shutdown, the channel is closed; workers drain remaining events with a 10-second deadline before exit.

Load test: 1000 sequential redirects followed by a 15-second drain → all 1000 clicks land in the DB. Zero loss at steady-state load.

---

## Performance & observability

Every request creates a Sentry transaction with manual spans on Redis and Postgres calls, plus a `cache.hit` tag for filtering.

### Cache hit — sub-millisecond redirect

The full redirect path on cache hit completes in ~1ms, almost entirely spent in Redis:

![Cache hit waterfall](docs/images/sentry-cache-hit.png)

### Cache miss — single DB query, still under 5ms

A cold lookup adds one indexed `SELECT` on `short_code`, then warms the cache for next time. The next request hits the cache:

![Cache miss waterfall](docs/images/sentry-cache-miss.png)

### Shorten — full write path

The shorten endpoint is the heaviest path: INSERT into urls, base62-encode the returned ID, UPDATE the row with the generated code, warm the cache:

![Shorten waterfall](docs/images/sentry-shorten.png)

---

## Security highlights

Decisions that distinguish this from a tutorial-grade implementation:

**No user enumeration on login.** Failed login returns the same response whether the email exists or the password is wrong. Otherwise an attacker could probe which emails are registered by timing/error differences.

**Algorithm-confusion attack prevention.** The JWT validator explicitly verifies the signing method is HMAC-SHA256 before trusting the signature. Without this check, a token with `alg: none` would be accepted by naive code — a well-known JWT vulnerability.

**Opaque 404s for ownership.** Trying to view stats for a URL you don't own returns 404, not 403. This prevents enumeration of short codes across users.

**Tiered rate limiting.** Public endpoints (register, login, redirect) limit per-IP to deter credential stuffing and burst abuse. Authenticated endpoints limit per-user for fair access across IPs. Redis-backed, atomic, fail-open on Redis errors.

**IP hashing at write time.** Click events store `SHA-256(ip)`, not the raw address. Unique visitor counts (`COUNT(DISTINCT ip_hash)`) still work, but the system never has plaintext IPs to leak. GDPR-friendly.

**Sensitive data scrubbed in Sentry.** A `BeforeSend` hook strips query strings (often contain tokens), and `SendDefaultPII: false` prevents auto-capture of cookies and raw IPs.

**Fail-open on cache/rate-limit errors, fail-closed on auth errors.** If Redis is unreachable, redirects fall back to the DB and rate limiting passes through with a logged warning — the user notices slightly slower latency, not an outage. Auth errors do the opposite: any token problem is a hard 401, never a silent pass-through.

---

## Getting started locally

### Prerequisites

- Go 1.26+
- Node.js 18+
- Docker (Postgres + Redis via `docker compose`)
- `golang-migrate` (`brew install golang-migrate` on macOS)

### Setup

```bash
git clone https://github.com/archit2901/url-shortener.git
cd url-shortener

cp .env.example .env
# Edit .env. Generate a JWT secret with:
openssl rand -base64 32

docker compose up -d        # start Postgres + Redis
make migrate-up             # apply schema
make run                    # boot the API on :8080
```

In a second terminal, start the frontend:

```bash
cd frontend
cp .env.example .env.local
npm install
npm run dev                 # open http://localhost:3000
```

### Try the API directly

```bash
# Anonymous shortening — works without auth
curl -X POST http://localhost:8080/api/shorten \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com"}'

# Register + login → get a JWT
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"supersecret123"}'

TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"you@example.com","password":"supersecret123"}' \
  | jq -r .token)

# Shorten as authenticated user (URL gets owned)
curl -X POST http://localhost:8080/api/shorten \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"url":"https://github.com"}'

# List your URLs with click counts
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/urls

# Detailed stats for one URL
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/urls/<code>/stats
```

---

## Testing

Three layers, all run by `make test`:

- **Unit tests** with hand-rolled mocks — service logic, base62 encode/decode, JWT generation/validation, worker pool behaviors including drop-on-overflow and graceful shutdown drain, rate limiter logic
- **Handler tests** with `httptest` — request/response cycle, status codes, JSON shape, security boundaries (e.g. 404 not 403 for unauthorized stats)
- **Integration tests** with `testcontainers-go` — disposable Postgres + Redis containers per test, real SQL queries against real data, real migrations applied automatically

```bash
make test         # everything, ~10s with testcontainers
make test-unit    # fast tests only (-short flag)
```

CI runs the full suite on every push via GitHub Actions, using the same testcontainers setup. Lint job runs `go vet` and `gofmt -l .` in parallel.

---

## Project structure

```
url-shortener/
├── .github/workflows/ci.yml          # tests + lint on every push
├── docker-compose.yml                # local Postgres + Redis
├── Makefile                          # run, test, migrate
├── docs/images/                      # screenshots
├── backend/
│   ├── Dockerfile                    # multi-stage build for Railway
│   ├── cmd/api/main.go               # entry point + DI wiring + graceful shutdown
│   ├── db/migrations/                # SQL migrations (golang-migrate format)
│   ├── internal/
│   │   ├── analytics/                # async worker pool
│   │   ├── auth/                     # JWT + bcrypt
│   │   ├── cache/                    # Redis wrapper
│   │   ├── handlers/                 # HTTP handlers (URL, auth)
│   │   ├── middleware/               # OptionalAuth, RequireAuth, RateLimiter
│   │   ├── observability/            # Sentry init
│   │   ├── repository/               # DB access (urls, users, clicks, click stats)
│   │   ├── services/                 # business logic
│   │   └── shortener/                # base62 encode/decode
│   └── tests/                        # integration tests with testcontainers
└── frontend/
    ├── src/
    │   ├── app/                      # App Router pages (auth, dashboard)
    │   ├── components/ui/            # shadcn/ui components
    │   └── lib/                      # API client, auth context, types
    └── package.json
```

The `internal/` directory uses Go's standard convention: nothing outside this module can import from it. Forces clean architecture by compiler enforcement.

---

## Design decisions

Trade-offs worth explaining:

**Why base62 from sequential DB IDs (not random codes)?** Three reasons. Sequential IDs are guaranteed unique by Postgres — no collision-check round trips needed. Base62 encoding produces dense codes (every value from "0" to "ZZZZZZZ" is reachable; no wasted keyspace). And encoding is a pure function, no I/O during generation. Trade-off: short codes are enumerable. For a portfolio project this is fine; production would obfuscate with a Feistel cipher or hash-then-encode.

**Why cache-aside, not write-through?** Cache-aside is simpler and naturally resilient — if Redis is empty (cold start, eviction, restart), the next request just hits the DB and repopulates. Write-through requires keeping cache and DB consistent on every write, which adds failure modes for marginal latency win.

**Why a separate clicks table, not a counter column on urls?** A counter would answer "how many clicks?" but nothing else. The clicks table preserves the full event stream — when, from where (hashed), on what device — enabling richer queries. The cost of more writes is mitigated by the async worker.

**Why drop-on-overflow instead of blocking?** A channel that blocks the producer when full would cascade backpressure into the redirect handler, slowing every user. Dropping analytics events under sustained burst is a tolerable failure mode; slowing redirects is not.

**Why three workers, not one?** Multiple goroutines can write to Postgres concurrently using different connections from the pool. With one worker, batches would serialize and bottleneck on DB latency.

**Why fixed-window rate limiting, not sliding window?** Fixed window with `INCR`+`EXPIRE` is two atomic Redis operations; sliding window needs a sorted set with multiple operations and trimming. The 2× burst at window boundaries that fixed window allows is acceptable; the operational simplicity is worth it.

---

## Future improvements

Tracked, intentionally deferred:

- **Cache the URL ID alongside the URL.** Currently, cache hits still do a small DB lookup to fetch the numeric ID needed for click recording. Storing `{id, long_url}` as JSON in Redis would eliminate this — saves ~0.5ms per cached redirect.
- **Custom aliases.** Let users pick their own short codes (`/my-talk-2026`), backed by the same unique constraint.
- **QR code generation.** PNG QR code endpoint for any short URL.

---

## Roadmap

- [x] Base62 short code generation from sequential IDs
- [x] PostgreSQL persistence with migrations
- [x] Redis cache-aside for fast redirects
- [x] JWT auth (register, login, optional + required)
- [x] Async analytics worker with batched COPY inserts and graceful shutdown
- [x] List-my-URLs endpoint with pagination
- [x] Click stats endpoint with ownership check
- [x] Sentry error tracking + performance tracing
- [x] Comprehensive test suite (unit + integration + handler)
- [x] CI/CD with GitHub Actions
- [x] Rate limiting middleware (per-IP for public, per-user for authenticated)
- [x] Next.js dashboard with auth, URL list, and click charts
- [x] Deployment to Railway (backend) + Vercel (frontend)
- [ ] Custom aliases
- [ ] QR code generation

---

## Learnings

What this project taught me, beyond "I know Go now":

- **Concurrency patterns** — buffered channels, worker pools, non-blocking sends with `select { default: }`, graceful shutdown sequencing with `sync.WaitGroup` and signal handlers
- **System design trade-offs** — read-heavy caching, async event processing, fail-open vs fail-closed, the dual-trigger flush pattern (size OR time), tiered rate limiting
- **SQL fluency** — schema design with foreign keys + nullable ownership, LEFT JOINs with COALESCE for zero-or-many relationships, `FILTER` clauses inside aggregates, `DATE_TRUNC` for time-series rollups, `CopyFrom` for high-throughput inserts
- **Operational concerns** — structured logging with `slog`, distributed tracing with manual spans, privacy-conscious error reporting, bounded graceful shutdown, multi-stage Docker builds
- **Security mindset** — algorithm confusion attacks, user enumeration, opaque 404s for ownership, why bcrypt cost matters, fail-open vs fail-closed for different middleware types
- **Full-stack integration** — type-safe API clients, JWT session persistence, CORS configuration, monorepo deployment to two different platforms

---

## License

MIT
