APP_ENV ?= local
API_NAME := chat-platform-api
WORKER_NAME := chat-platform-worker
API_MAIN_PATH := ./cmd/api/main.go
WORKER_MAIN_PATH := ./cmd/worker/main.go
MIGRATION_DIR := ./internal/infrastructure/mysql/migrations
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0
DSN = "$(shell go run ./cmd/dsn/main.go)"

.PHONY: help
help: ## Show this help 
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: run
run: ## Run the application
	APP_ENV=$(APP_ENV) go run $(API_MAIN_PATH)

.PHONY: run-worker
run-worker: ## Run the standalone Redis Stream worker
	APP_ENV=$(APP_ENV) go run $(WORKER_MAIN_PATH)

.PHONY: build
build: ## Build the application
	go build -o bin/$(API_NAME) $(API_MAIN_PATH)

.PHONY: build-worker
build-worker: ## Build the standalone Redis Stream worker
	go build -o bin/$(WORKER_NAME) $(WORKER_MAIN_PATH)

.PHONY: tidy
tidy: ## Tidy the Go modules
	go mod tidy

.PHONY: clean
clean: ## Clean the build artifacts
	rm -rf bin/$(API_NAME) bin/$(WORKER_NAME)

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
gen: ## Generate query code with the pinned sqlc version
	$(SQLC) generate
	@echo "✓ sqlc generated"

.PHONY: gen_check
gen_check: ## Verify sqlc queries are valid
	$(SQLC) vet

.PHONY: lint
lint: ## Run linter
	golangci-lint run ./...

.PHONY: check
check: fmt-check ## Check format and Go code; include local tests when available
ifneq ($(wildcard test/main.go),)
	go run ./test vet ./...
	go run ./test test ./... -count=1
else
	go vet ./...
	go test ./... -count=1
endif

.PHONY: test
test: ## Run the untracked local unit/contract test suite
ifneq ($(wildcard test/main.go),)
	go run ./test test ./... -count=1
else
	$(error Local tests are unavailable; test/ is intentionally untracked)
endif

.PHONY: fmt
fmt: ## Format handwritten Go files
	go run ./scripts/format -w

.PHONY: fmt-check
fmt-check: ## Check handwritten Go formatting without changing files
	go run ./scripts/format

.DEFAULT: 
	@echo "No rule target. Please use 'make help'"
