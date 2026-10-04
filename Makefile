APP := woovi-pix-mcp
MIGRATIONS_DIR := internal/charge/sqlite_migrations
GOOSE_VERSION := v3.28.0
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)

DATABASE_PATH ?=

.DEFAULT_GOAL := help
.PHONY: help run simulator build fmt fmt-check lint vet test test-race test-all govulncheck check migrate-create migrate-up migrate-status migrate-validate

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  make %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

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

test: ## Run the full test suite using real file-backed SQLite
	go test -count=1 ./...

test-race: ## Run the full race-enabled suite using real SQLite
	go test -race -count=1 ./...

test-all: test test-race ## Run normal and race-enabled tests

govulncheck: ## Scan the Go code and dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

check: fmt-check vet test test-race build lint govulncheck ## Run local quality checks

migrate-create: ## Create a timestamped Goose SQL migration (use name=...)
	@test -n "$(name)" || (echo "Usage: make migrate-create name=add_charge_metadata" >&2; exit 2)
	$(GOOSE) -dir $(MIGRATIONS_DIR) create $(name) sql

migrate-up: ## Apply migrations to an explicit development database (DATABASE_PATH=...)
	@test -n "$(DATABASE_PATH)" || (echo "Set DATABASE_PATH to a development SQLite database" >&2; exit 2)
	$(GOOSE) -dir $(MIGRATIONS_DIR) sqlite3 "$(DATABASE_PATH)" up

migrate-status: ## Show Goose migration status (DATABASE_PATH=...)
	@test -n "$(DATABASE_PATH)" || (echo "Set DATABASE_PATH" >&2; exit 2)
	$(GOOSE) -dir $(MIGRATIONS_DIR) sqlite3 "$(DATABASE_PATH)" status

migrate-validate: ## Validate Goose migration files without changing the database
	$(GOOSE) -dir $(MIGRATIONS_DIR) validate
