# Simple Makefile for a Go project

# Which env file every target reads. Override per invocation, e.g.
#   make run ENV_FILE=./secrets/.env_local
ENV_FILE ?= ./secrets/.env

-include $(ENV_FILE)
export

# Build the application
all: build test

build:
	@echo "Building..."
	@go build -o main.exe ./cmd/api

# Run the application
run:
	@go run ./cmd/api

# Create DB container
docker-run:
	@docker compose --env-file $(ENV_FILE) up --build
# Shutdown DB container
docker-down:
	@docker compose --env-file $(ENV_FILE) down

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
	@rm -f main.exe

migrate-up:
	@goose -dir migrations postgres "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_DATABASE)?sslmode=$(SSL_MODE)&search_path=$(DB_SCHEMA)" up

migrate-down:
	@goose -dir migrations postgres "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_DATABASE)?sslmode=$(SSL_MODE)&search_path=$(DB_SCHEMA)" down

migrate-status:
	@goose -dir migrations postgres "postgres://$(DB_USER):$(DB_PASSWORD)@$(DB_HOST):$(DB_PORT)/$(DB_DATABASE)?sslmode=$(SSL_MODE)&search_path=$(DB_SCHEMA)" status

migrate-create:
	@goose -dir migrations create $(name) sql


.PHONY: all build run test clean watch docker-run docker-down itest
