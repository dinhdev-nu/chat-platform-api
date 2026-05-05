APP_ENV ?= local
APP_NAME := chat-platform-api
MAIN_PATH := ./cmd/api/main.go
MIGRATION_DIR := ./internal/infrastructure/mysql/gorm/migrations
DSN = "$(shell go run ./cmd/dsn/main.go)"

.PHONY: help
help: ## Show this help 
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the application
	APP_ENV=$(APP_ENV) go run $(MAIN_PATH)

.PHONY: build
build: ## Build the application
	go build -o bin/$(APP_NAME) $(MAIN_PATH)

.PHONY: tidy
tidy: ## Tidy the Go modules
	go mod tidy

.PHONY: clean
clean: ## Clean the build artifacts
	rm -rf bin/$(APP_NAME)

.PHONY: migrate_up
migrate_up: ## Run all pending migrations
	APP_ENV=$(APP_ENV) goose -dir $(MIGRATION_DIR) mysql $(DSN) up

.PHONY: migrate_down
migrate_down: ## Rollback the last migration
	APP_ENV=$(APP_ENV) goose -dir $(MIGRATION_DIR) mysql $(DSN) down

.PHONY: migrate_status
migrate_status: ## Show the status of all migrations
	APP_ENV=$(APP_ENV) goose -dir $(MIGRATION_DIR) mysql $(DSN) status

.PHONY: migrate_create
migrate_create: ## Create a new migration file (usage: make migrate_create name=your_migration_name)
	goose -dir $(MIGRATION_DIR) create $(name) sql

.PHONY: migrate_reset
migrate_reset: ## Reset the database by rolling back all migrations and then applying them again
	APP_ENV=$(APP_ENV) goose -dir $(MIGRATION_DIR) mysql $(DSN) reset

.PHONY: gen
gen: ## Generate code using sqlc
	sqlc generate
	@echo "✓ sqlc generated"

.PHONY: gen_check
gen_check: ## Verify sqlc queries are valid
	@sqlc vet

.PHONY: lint
lint: ## Run linter
	golangci-lint run ./...

.PHONY: seed
seed: ## Run seed data
	APP_ENV=$(APP_ENV) go run ./cmd/seed/main.go


