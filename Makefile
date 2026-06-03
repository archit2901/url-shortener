.PHONY: help run test test-unit test-integration migrate-up migrate-down build clean

help:
	@echo "Available commands:"
	@echo "  make run              - Run the API server"
	@echo "  make test             - Run all tests"
	@echo "  make test-unit        - Run only unit tests (fast)"
	@echo "  make test-integration - Run integration tests"
	@echo "  make migrate-up       - Apply database migrations"
	@echo "  make migrate-down     - Roll back the last migration"
	@echo "  make build            - Build the API binary"
	@echo "  make clean            - Remove build artifacts"

run:
	cd backend && go run ./cmd/api

test:
	cd backend && go test ./...

test-unit:
	cd backend && go test -short ./...

test-integration:
	cd backend && go test -run Integration ./tests/...

migrate-up:
	@export $$(cat .env | xargs) && \
	migrate -path backend/db/migrations -database "$$DATABASE_URL" up

migrate-down:
	@export $$(cat .env | xargs) && \
	migrate -path backend/db/migrations -database "$$DATABASE_URL" down 1

build:
	cd backend && go build -o ../bin/api ./cmd/api

clean:
	rm -rf bin/
