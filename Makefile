BINARY      := lazyobs
PKG         := github.com/CentreonLabs/lazyobs
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
	-X $(PKG)/internal/version.Version=$(VERSION) \
	-X $(PKG)/internal/version.Commit=$(COMMIT) \
	-X $(PKG)/internal/version.Date=$(DATE)

GOLANGCI_LINT_VERSION ?= v2.15.0
GOLANGCI_LINT ?= go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)

PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*##/ {printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.PHONY: build
build: ## Build the binary into ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/lazyobs

.PHONY: run
run: ## Run lazyobs (pass arguments with ARGS="...")
	go run ./cmd/lazyobs $(ARGS)

.PHONY: test
test: ## Run unit tests with the race detector
	go test -race -count=1 ./...

.PHONY: integration
integration: ## Run integration tests against the live API (needs LAZYOBS_TOKEN and LAZYOBS_ORG)
	go test -tags integration -run Integration -count=1 -v ./...

SINCE ?= 1h

.PHONY: seed
seed: ## Send the demo data of the last SINCE (default 1h) to the current context's org
	go run ./cmd/lazyobs-seed --since $(SINCE)

.PHONY: screenshots
screenshots: ## Regenerate the README screenshots (demo data) into docs/screenshots
	TZ=UTC LAZYOBS_SCREENSHOTS=$(CURDIR)/docs/screenshots go test -tags screenshots -run Screenshots -count=1 ./internal/cli/

.PHONY: cover
cover: ## Run tests and open the coverage report
	go test -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

.PHONY: lint
lint: ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: ## Format code
	$(GOLANGCI_LINT) fmt ./...

.PHONY: tidy
tidy: ## Tidy go.mod and go.sum
	go mod tidy

.PHONY: cross
cross: ## Build for every supported platform into ./dist
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=""; [ $$os = windows ] && ext=".exe"; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -trimpath -ldflags "$(LDFLAGS)" \
			-o dist/$(BINARY)-$$os-$$arch$$ext ./cmd/lazyobs || exit 1; \
	done

.PHONY: dist
dist: ## Build the release archives and checksums into ./dist (VERSION=v1.2.3)
	scripts/dist.sh $(VERSION)

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf bin dist coverage.out
