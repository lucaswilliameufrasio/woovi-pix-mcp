APP := woovi-pix-mcp
MIGRATIONS_DIR := internal/charge/sqlite_migrations
GOOSE_VERSION := v3.28.0
GOOSE := go run github.com/pressly/goose/v3/cmd/goose@$(GOOSE_VERSION)
RUFF := uvx --from ruff==0.16.10 ruff

DATABASE_PATH ?=

.DEFAULT_GOAL := help
.PHONY: help run simulator build fmt fmt-check lint vet test test-race test-all govulncheck check migrate-create migrate-up migrate-status migrate-validate test-release release-check release-snapshot workflow-check

help: ## Show available targets
	@awk 'BEGIN { FS = ":.*## " } /^[a-zA-Z0-9_-]+:.*## / { printf "  make %-18s %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

.PHONY: test-litestream
test-litestream: ## Full tests with real Litestream and disposable S3-compatible storage (TEST_LITESTREAM_BINARY=...)
	bash scripts/test-litestream.sh

run: ## Run the stdio MCP server (requires Woovi environment variables)
	go run ./cmd/woovi-pix-mcp

simulator: ## Run the local HTTP Woovi simulator
	go run ./cmd/woovi-simulator

build: ## Compile all Go packages
	go build ./...

fmt: ## Format Go/Python and organize imports
	golangci-lint fmt
	$(RUFF) format scripts
	$(RUFF) check --fix scripts

fmt-check: ## Verify Go/Python source formatting
	@diff="$$(golangci-lint fmt --diff)" && test -z "$$diff"
	$(RUFF) format --check scripts

lint: ## Run Go whitespace/static analysis and Python lint
	golangci-lint run ./...
	$(RUFF) check scripts

vet: ## Run go vet
	go vet ./...

test: ## Run the full test suite using real file-backed SQLite
	go test -count=1 ./...

test-race: ## Run the full race-enabled suite using real SQLite
	go test -race -count=1 ./...

test-all: test test-race ## Run normal and race-enabled tests

govulncheck: ## Scan the Go code and dependencies for known vulnerabilities
	go run golang.org/x/vuln/cmd/govulncheck@v1.7.0 ./...

test-release: ## Test release version and tag validation without publishing
	python3 -m unittest discover -s scripts -p 'test_*.py'

workflow-check: ## Lint GitHub Actions syntax and permissions with pinned actionlint
	go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12

release-check: ## Validate GoReleaser configuration (requires GoReleaser 2.18.2)
	goreleaser check

release-snapshot: release-check ## Build six platform archives/checksums locally, without publishing
	goreleaser release --snapshot --clean
	python3 scripts/check_release_artifacts.py dist

check: fmt-check vet test test-race build lint govulncheck test-release workflow-check ## Run local quality checks

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
