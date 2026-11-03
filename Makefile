.DEFAULT_GOAL := help

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.15.0
GOVULNCHECK ?= govulncheck
GOFUMPT ?= gofumpt

COVER_PROFILE ?= build/coverage/coverage.out
# cmd/ is a thin main; coverage gates apply to internal packages (library parity).
COVER_PACKAGES ?= ./internal/...

DOCKER_IMAGE ?= tracewick-agent:local

.PHONY: help tidy fmt fmt-check lint vuln test test-race coverage ci build docker-build e2e

help: ## Show available targets
	@grep -E '^[a-zA-Z0-9_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "} {printf "  %-14s %s\n", $$1, $$2}'

tidy: ## go mod tidy and verify go.mod/go.sum are clean
	$(GO) mod tidy
	@git diff --exit-code -- go.mod || (echo 'go.mod dirty after tidy' && exit 1)
	@if [ -f go.sum ]; then git diff --exit-code -- go.sum || (echo 'go.sum dirty after tidy' && exit 1); fi

fmt: ## Format with gofumpt
	@command -v $(GOFUMPT) >/dev/null || $(GO) install mvdan.cc/gofumpt@latest
	$(GOFUMPT) -extra -w .

fmt-check: ## Check gofumpt formatting
	@command -v $(GOFUMPT) >/dev/null || $(GO) install mvdan.cc/gofumpt@latest
	@diff=$$($(GOFUMPT) -extra -l .); \
	if [ -n "$$diff" ]; then echo "gofumpt needed on:"; echo "$$diff"; exit 1; fi

lint: ## Run golangci-lint (strict config)
	@command -v $(GOLANGCI_LINT) >/dev/null || $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	$(GOLANGCI_LINT) run ./...

vuln: ## Run govulncheck
	@command -v $(GOVULNCHECK) >/dev/null || $(GO) install golang.org/x/vuln/cmd/govulncheck@latest
	$(GOVULNCHECK) ./...

test: ## Run unit tests
	$(GO) test ./...

test-race: ## Run tests with the race detector
	$(GO) test -race ./...

coverage: ## Write coverage profile under build/coverage
	mkdir -p build/coverage
	$(GO) test -race -covermode=atomic -coverprofile=$(COVER_PROFILE) $(COVER_PACKAGES)
	$(GO) tool cover -func=$(COVER_PROFILE)

build: ## Build the tracewick-agent binary into bin/
	mkdir -p bin
	$(GO) build -o bin/tracewick-agent ./cmd/tracewick-agent

docker-build: ## Build the local container image (tracewick-agent:local)
	docker build -t $(DOCKER_IMAGE) .

e2e: ## Cross-process E2E (build tag e2e; not part of make ci)
	$(GO) test -tags=e2e -count=1 -timeout=5m ./test/e2e/

ci: tidy fmt-check lint vuln test-race coverage ## Local PR gate (mirrors Checks)
