APP_ENV ?= local
API_NAME := chat-platform-api
WORKER_NAME := chat-platform-worker
API_MAIN_PATH := ./cmd/api/main.go
WORKER_MAIN_PATH := ./cmd/worker/main.go
MIGRATION_DIR := ./internal/infrastructure/mysql/migrations
SQLC := go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0
DSN = "$(shell go run ./cmd/dsn/main.go)"

# Seed uses the API's local config; SEED_DATABASE confirms, rather than overrides, it.
# Make defaults match the 50-user / 150-room / 50,000-message demo profile.
SEED_DATABASE ?= chat_platform_api
SEED_DATASET ?= local-demo-v1
SEED_CONVERSATIONS ?= 150
SEED_MESSAGES ?= 50000
WRITERS_STOPPED ?= 0
SEED_PROFILE_FLAGS = --users 50 --conversations $(SEED_CONVERSATIONS) --messages $(SEED_MESSAGES)
SEED_TARGET_FLAGS = --database "$(SEED_DATABASE)" --dataset "$(SEED_DATASET)"

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

# Export through Make so seed recipes work with both POSIX shells and Windows cmd.
seed-plan seed-apply seed-verify seed-verify-baseline seed-sync seed-reset: export APP_ENV := $(APP_ENV)

.PHONY: seed-plan
seed-plan: ## Preview seed data offline (default: 50 users, 150 rooms, 50000 messages)
	go run ./cmd/seed plan --dataset "$(SEED_DATASET)" $(SEED_PROFILE_FLAGS)

.PHONY: seed-check-writers
seed-check-writers:
	@$(if $(filter 1,$(WRITERS_STOPPED)),,$(error Stop API and worker first, then rerun with WRITERS_STOPPED=1))

.PHONY: seed-apply
seed-apply: seed-check-writers ## Insert seed data and sync Redis (requires WRITERS_STOPPED=1)
	go run ./cmd/seed apply $(SEED_TARGET_FLAGS) --writers-stopped $(SEED_PROFILE_FLAGS)

.PHONY: seed-verify
seed-verify: ## Verify the saved dataset against MySQL and Redis after normal app use
	go run ./cmd/seed verify $(SEED_TARGET_FLAGS)

.PHONY: seed-verify-baseline
seed-verify-baseline: ## Verify exact original totals immediately after seeding
	go run ./cmd/seed verify $(SEED_TARGET_FLAGS) --baseline

.PHONY: seed-sync
seed-sync: seed-check-writers ## Repair seed Redis counters from current MySQL data (requires WRITERS_STOPPED=1)
	go run ./cmd/seed sync $(SEED_TARGET_FLAGS) --writers-stopped

.PHONY: seed-reset
seed-reset: seed-check-writers ## Delete seeded rooms and their later messages; keep both test users (requires WRITERS_STOPPED=1)
	go run ./cmd/seed reset $(SEED_TARGET_FLAGS) --writers-stopped

.PHONY: seed-lint
seed-lint: ## Lint the seed CLI, generator and storage
	golangci-lint run ./cmd/seed/... ./internal/seed/...

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
