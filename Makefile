.PHONY: help test vet preflight check-install-script test-install-behavior test-install-behavior-ps test-integration test-tui-pty-smoke test-cover-restore build build-cli build-versioned

# This workspace is not always a Git checkout; disable VCS stamping for local builds.
export GOFLAGS ?= -buildvcs=false

VERSION ?= 0.0.0-local
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo local)
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
BINARY ?= ./bin/dolly
LDFLAGS := -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-20s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run unit tests
	go test ./...

vet: ## Run go vet
	go vet ./...

preflight: check-install-script test-install-behavior test vet ## Lint and test, then build
	go build -buildvcs=false ./cmd/dolly

check-install-script: ## Validate install.sh syntax
	sh -n install.sh

test-install-behavior: ## Test install.sh checksum policy
	sh test/install-behavior.sh

test-install-behavior-ps: ## Test install.ps1 checksum policy (requires pwsh)
	@if command -v pwsh >/dev/null 2>&1; then \
		pwsh -NoProfile -File test/install-behavior.ps1; \
	else \
		echo "pwsh not found; skipping install.ps1 behavior tests (Windows CI covers this)"; \
	fi

build: ## Build all packages
	go build ./...

build-cli: ## Build the dolly binary
	go build -buildvcs=false -o $(BINARY) ./cmd/dolly

build-versioned: ## Build with version stamp
	go build -buildvcs=false -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/dolly


test-cover-restore: ## Test restore package with coverage
	go test -cover ./internal/restore/...

test-integration: ## Run integration tests (requires DOLLY_TEST_PG_DSN)
	@test -n "$$DOLLY_TEST_PG_DSN" || (echo "Set DOLLY_TEST_PG_DSN (see README.md)"; exit 1)
	go test -p 1 -tags=integration ./internal/testutil/pgintegration/ ./internal/db/... ./internal/dump/... ./internal/restore/... ./internal/clone/...

test-tui-pty-smoke: ## Run opt-in real terminal TUI smoke (Unix only)
	DOLLY_TUI_PTY_SMOKE=1 go test -tags=integration ./cmd/dolly -run 'TestTUIPTY' -count=1 -v
