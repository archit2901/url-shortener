# syntax=docker/dockerfile:1.6

# Stage 1: build the binary
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Copy go.mod and go.sum first for better layer caching.
# Dependencies only re-download if these files change.
COPY go.mod go.sum ./
RUN go mod download

# Now copy the rest of the source and build.
COPY . .

# Build a static binary with optimizations.
# CGO_ENABLED=0 produces a binary with no libc dependency,
# so we can use a minimal final image.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=$(git rev-parse --short HEAD 2>/dev/null || echo dev)" \
    -o /out/api ./cmd/api

# Stage 2: minimal runtime image
FROM alpine:3.20

# tzdata is needed for time.LoadLocation if the app uses it.
# ca-certificates lets the binary make HTTPS calls (Sentry, etc).
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# Copy just the binary from the builder stage.
COPY --from=builder /out/api ./api

# Document which port we listen on (Railway will set $PORT).
EXPOSE 8080

# Run the binary.
CMD ["./api"]