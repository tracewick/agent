.DEFAULT_GOAL := help

GO ?= go
GOLANGCI_LINT ?= golangci-lint
GOLANGCI_LINT_VERSION ?= v2.15.0
GOVULNCHECK ?= govulncheck
GOFUMPT ?= gofumpt
GREMLINS ?= gremlins
GREMLINS_VERSION ?= v0.6.0
# Core packages for the MSI gate (expand carefully; never lower .gremlins.yaml thresholds).
MUTATION_PACKAGES ?= ./internal/delivery ./internal/aggregate ./internal/spool

COVER_PROFILE ?= build/coverage/coverage.out
# cmd/ is a thin main; coverage gates apply to internal packages (library parity).
COVER_PACKAGES ?= ./internal/...

DOCKER_IMAGE ?= tracewick-agent:local

PHP_DRAIN_DIR ?= $(CURDIR)/.cache/php-drain
PHP_DRAIN_BIN ?= $(PHP_DRAIN_DIR)/vendor/bin/tracewick-drain

.PHONY: help tidy fmt fmt-check lint vuln test test-race coverage ci build docker-build e2e \
	stand-up stand-down stand-smoke pprof-check fuzz-smoke php-drain mutation

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

php-drain: ## Install Packagist tracewick/tracewick for Go+PHP-drain e2e
	@command -v composer >/dev/null || (echo 'composer required for make php-drain' && exit 1)
	@command -v php >/dev/null || (echo 'php required for make php-drain' && exit 1)
	mkdir -p $(PHP_DRAIN_DIR)
	@if [ ! -f $(PHP_DRAIN_DIR)/composer.json ]; then \
		printf '%s\n' '{"name":"tracewick/agent-e2e-drain","require":{"tracewick/tracewick":"^0.1"},"config":{"sort-packages":true}}' \
			> $(PHP_DRAIN_DIR)/composer.json; \
	fi
	cd $(PHP_DRAIN_DIR) && composer update --no-interaction --no-progress --prefer-dist --no-dev
	@test -x $(PHP_DRAIN_BIN) || test -f $(PHP_DRAIN_BIN)

e2e: ## Cross-process E2E (build tag e2e; not part of make ci; -v for useful progress)
	@if [ -f "$(PHP_DRAIN_BIN)" ]; then \
		TRACEWICK_DRAIN_BIN="$(PHP_DRAIN_BIN)" $(GO) test -tags=e2e -count=1 -timeout=5m -v ./test/e2e/; \
	else \
		$(GO) test -tags=e2e -count=1 -timeout=5m -v ./test/e2e/; \
	fi

stand-up: ## Start Pyroscope + Toxiproxy (127.0.0.1)
	docker compose up -d --wait
	./docker/wait-for-url.sh http://127.0.0.1:4040/ready
	./docker/wait-for-url.sh http://127.0.0.1:14040/ready

stand-down: ## Stop the stand and remove volumes
	docker compose down --volumes --remove-orphans

stand-smoke: ## Check a running stand is wired (Pyroscope + Toxiproxy)
	./docker/smoke-test.sh

pprof-check: ## Validate testdata/pprof/*.pb with go tool pprof (library CI parity)
	@for f in testdata/pprof/*.pb; do \
		echo "==> $$f"; \
		$(GO) tool pprof -raw "$$f" >/dev/null; \
	done

fuzz-smoke: ## Short SpoolFile fuzz run for CI
	$(GO) test ./internal/spool/ -fuzz=FuzzParse -fuzztime=15s

mutation: ## Gremlins MSI gate on core packages (.gremlins.yaml thresholds)
	@command -v $(GREMLINS) >/dev/null || $(GO) install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)
	@for pkg in $(MUTATION_PACKAGES); do \
		echo "==> gremlins unleash $$pkg"; \
		$(GREMLINS) unleash $$pkg || exit $$?; \
	done

ci: tidy fmt-check lint vuln test-race coverage pprof-check ## Local PR gate (mirrors Checks)
