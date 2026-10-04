APP := woovi-pix-mcp
MIGRATIONS_DIR := internal/charge/migrations
GOOSE_VERSION := v3.28.0
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

export POSTGRES_DB ?= woovi
export POSTGRES_USER ?= woovi
export POSTGRES_PASSWORD ?= woovi-local-test
export POSTGRES_PORT ?= 55463

DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@127.0.0.1:$(POSTGRES_PORT)/$(POSTGRES_DB)?sslmode=disable
export DATABASE_URL
export TEST_DATABASE_URL ?= $(DATABASE_URL)

.DEFAULT_GOAL := help
.PHONY: help db-up db-down db-logs run simulator build fmt fmt-check lint vet test test-race test-all govulncheck check migrate-create migrate-up migrate-status migrate-validate

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  make %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

db-up: ## Start PostgreSQL 18 for local development/tests (keeps its volume)
	docker compose up -d --wait postgres

db-down: ## Stop PostgreSQL without deleting its data volume
	docker compose stop postgres

db-logs: ## Follow PostgreSQL logs
	docker compose logs -f postgres

run: ## Run the stdio MCP server (requires Woovi environment variables)
	go run ./cmd/woovi-pix-mcp

simulator: ## Run the local HTTP Woovi simulator
	go run ./cmd/woovi-simulator

build: ## Compile all Go packages
	go build ./...

fmt: ## Format Go source files
	gofmt -w $$(find . -type f -name '*.go' -not -path './.git/*')

fmt-check: ## Verify Go source formatting
	@test -z "$$(gofmt -l .)"

lint: ## Run golangci-lint
	golangci-lint run ./...

vet: ## Run go vet
	go vet ./...

test: db-up ## Run the full test suite using real PostgreSQL 18
	go test -count=1 ./...

test-race: db-up ## Run the full race-enabled suite using real PostgreSQL 18
	go test -race -count=1 ./...

test-all: test test-race ## Run normal and race-enabled tests

govulncheck: ## Scan the Go code and dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

check: fmt-check vet test test-race build lint govulncheck ## Run local quality checks

migrate-create: ## Create a timestamped Goose SQL migration (use name=...)
	@test -n "$(name)" || (echo "Usage: make migrate-create name=add_charge_metadata" >&2; exit 2)
	$(GOOSE) -dir $(MIGRATIONS_DIR) create $(name) sql

migrate-up: db-up ## Apply pending migrations with Goose CLI
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-status: db-up ## Show Goose migration status
	$(GOOSE) -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

migrate-validate: ## Validate Goose migration files without changing the database
	$(GOOSE) -dir $(MIGRATIONS_DIR) validate
