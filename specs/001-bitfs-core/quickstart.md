# Quickstart: BitFS Core Filesystem

**Feature**: 001-bitfs-core

## Prerequisites

- Go 1.25.6+
- BSV testnet/regtest wallet with funds (for transaction testing)

## Build

```bash
# Build all binaries
cd bitfs/
go build ./cmd/...

# Verify builds
./bitfs --version
./bls --help
./bcat --help
```

## Run Tests

```bash
# All unit tests
go test ./...

# Single package
go test ./internal/method42/ -v

# Integration tests (requires build tag)
go test -tags=integration ./integration/ -v

# Count test cases
go test ./... -v -count=1 2>&1 | grep -c "=== RUN"
```

## Basic Usage

### 1. Initialize Wallet

```bash
# Create a new wallet with 24-word mnemonic
bitfs init

# Create a named vault
bitfs vault create my-files
```

### 2. Store Files

```bash
# Store a file (encrypted by default, Private mode)
bitfs put document.pdf /docs/

# Store as free (anyone can read)
bitfs put --mode free readme.txt /public/

# Create directory structure
bitfs mkdir /docs/reports/2026/
```

### 3. Read Files

```bash
# List directory (JSON for agents)
bls --json bitfs://your-pubkey/docs/

# Read file contents
bcat bitfs://your-pubkey/docs/document.pdf > document.pdf

# Download file
bget bitfs://your-pubkey/docs/document.pdf

# File metadata
bstat bitfs://your-pubkey/docs/document.pdf

# Directory tree
btree bitfs://your-pubkey/
```

### 4. Publish via Daemon

```bash
# Start daemon (serves content over HTTP)
bitfs daemon --listen :8080

# With TLS
bitfs daemon --listen :8443 --tls-cert cert.pem --tls-key key.pem
```

### 5. Sell Content

```bash
# Set price for a file
bitfs sell /docs/premium-report.pdf --price 1000

# Buyers use --buy flag
bget --buy bitfs://owner@example.com/docs/premium-report.pdf
```

## Configuration

Default config location: `~/.bitfs/config.yaml`

```yaml
network: testnet
storage:
  path: ~/.bitfs/storage
daemon:
  listen: ":8080"
  tls: false
x402:
  pricePerMB: 1000
  freeQuotaMB: 10
  invoiceExpiry: 3600
```

## Module Dependencies

```
method42 ← (no deps, core crypto)
wallet   ← go-sdk (bip32, bip39)
tx       ← wallet, go-sdk (transaction)
metanet  ← tx (Metanet DAG parsing)
spv      ← (crypto/sha256 only)
storage  ← (os, encoding/hex only)
paymail  ← (net, net/http)
x402     ← method42, go-sdk (transaction)
daemon   ← all internal packages
config   ← (viper)
cmd/*    ← daemon, config + relevant internal packages
```
