VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
BINS    := bitfs bls bcat bget bstat btree
OUTDIR  := bin

.PHONY: all build test lint clean e2e install help

all: build ## Build all binaries (default)

build: ## Build all binaries to bin/
	@mkdir -p $(OUTDIR)
	go build -ldflags "-X main.Version=$(VERSION)" -o $(OUTDIR)/ ./cmd/...

test: ## Run all unit tests
	go test ./...

test-v: ## Run all unit tests (verbose, no cache)
	go test ./... -v -count=1

test-integration: ## Run integration tests
	go test -tags integration ./integration/... -v -count=1

lint: ## Run golangci-lint
	golangci-lint run ./...

e2e: ## Run e2e tests (requires Docker)
	cd e2e && docker compose up -d
	go test -tags e2e ./e2e/... -v -timeout 120s

e2e-up: ## Start regtest node
	cd e2e && docker compose up -d

e2e-down: ## Stop regtest node
	cd e2e && docker compose down

install: build ## Install binaries to $GOPATH/bin
	@for b in $(BINS); do cp $(OUTDIR)/$$b $(shell go env GOPATH)/bin/$$b; done
	@echo "Installed to $$(go env GOPATH)/bin"

clean: ## Remove build artifacts
	rm -rf $(OUTDIR)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
