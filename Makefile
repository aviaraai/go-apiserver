# Simple Makefile for a Go project

-include ./secrets/.env
export

# Build the application
all: build test

build:
	@echo "Building..."
	@go build -o main.exe cmd/api/main.go

# Run the application
run:
	@go run cmd/api/main.go

# Create DB container
docker-run:
	@docker compose up --build
# Shutdown DB container
docker-down:
	@docker compose down

# Test the application
test:
	@echo "Testing..."
	@go test ./... -v
# Integrations Tests for the application
itest:
	@echo "Running integration tests..."
	@go test ./internal/database -v

# Live reload using air
watch:
	air

# Clean the binary
clean:
	@echo "Cleaning..."
	@rm -f main

migrate-up:
	@goose -dir migrations postgres "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_DATABASE)?sslmode=$(SSL_MODE)&search_path=$(DB_SCHEMA)" up

migrate-down:
	@goose -dir migrations postgres "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_DATABASE)?sslmode=$(SSL_MODE)&search_path=$(DB_SCHEMA)" down

migrate-status:
	@goose -dir migrations postgres "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_DATABASE)?sslmode=$(SSL_MODE)&search_path=$(DB_SCHEMA)" status

migrate-create:
	@goose -dir migrations create $(name) sql


.PHONY: all build run test clean watch docker-run docker-down itest
