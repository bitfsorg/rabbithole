VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
BINS    := bitfs bls bcat bget bstat btree
OUTDIR  := bin

.PHONY: all build test lint clean e2e install help
.PHONY: regtest regtest-down testnet testnet-down stn stn-down
.PHONY: dashboard dashboard-dev

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

regtest: ## Start regtest node (RPC localhost:18332)
	cd e2e && docker compose up -d
	@echo "Regtest node running. RPC at localhost:18332 (user: bitfs, pass: bitfs)"

regtest-down: ## Stop regtest node
	cd e2e && docker compose down

testnet: ## Start testnet node (RPC localhost:18333)
	cd e2e && docker compose -f docker-compose.testnet.yml up -d
	@echo "Testnet node syncing. RPC at localhost:18333 (user: bitfs, pass: bitfs)"
	@echo "Use a faucet to fund: https://faucet.bitcoincloud.net"

testnet-down: ## Stop testnet node
	cd e2e && docker compose -f docker-compose.testnet.yml down

stn: ## Start Teranode STN node (RPC localhost:18334)
	cd e2e && docker compose -f docker-compose.stn.yml up -d
	@echo "STN node syncing. RPC at localhost:18334 (user: bitfs, pass: bitfs)"
	@echo "Use the faucet to fund: https://faucet.teranode.bsvblockchain.org"

stn-down: ## Stop STN node
	cd e2e && docker compose -f docker-compose.stn.yml down

e2e-up: regtest ## Alias for regtest

e2e-down: regtest-down ## Alias for regtest-down

install: build ## Install binaries to $GOPATH/bin
	@for b in $(BINS); do cp $(OUTDIR)/$$b $(shell go env GOPATH)/bin/$$b; done
	@echo "Installed to $$(go env GOPATH)/bin"

clean: ## Remove build artifacts
	rm -rf $(OUTDIR)

dashboard: ## Build dashboard for embedding
	cd dashboard && npm install && npm run build

dashboard-dev: ## Start dashboard dev server
	cd dashboard && npm install && npm run dev

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'
