# BitFS User Guide

BitFS is a Unix-style decentralized encrypted file system built on the BSV
blockchain. All content is encrypted by default using Method 42 (ECDH-based
per-file key derivation). Content is organized into **vaults** -- top-level
directory trees, each backed by its own BIP32 key hierarchy. Files and
directories are addressed by public key (pnode), not by content hash.

BitFS has three access modes for content:

- **free** -- anyone can read the content (default for uploads)
- **private** -- only the owner can decrypt the content
- **paid** -- buyers pay via an atomic HTLC swap to receive a decryption capsule

This guide walks through every major workflow, from wallet setup to serving
content over HTTP.

## Table of Contents

1. [Installation](#1-installation)
2. [Wallet Setup](#2-wallet-setup)
3. [Managing Vaults](#3-managing-vaults)
4. [Creating Directories and Uploading Files](#4-creating-directories-and-uploading-files)
5. [Reading Content with b-tools](#5-reading-content-with-b-tools)
6. [Selling Content](#6-selling-content)
7. [Buying Content](#7-buying-content)
8. [Publishing with DNSLink](#8-publishing-with-dnslink)
9. [Running the Daemon](#9-running-the-daemon)
10. [Shell REPL Usage](#10-shell-repl-usage)
11. [Additional File Operations](#11-additional-file-operations)
12. [Common Options](#12-common-options)

---

## 1. Installation

Build the CLI tools from source (requires Go 1.25.6 or later):

```bash
# Build the main CLI binary
go build ./cmd/bitfs

# Build all binaries (bitfs, bls, bcat, bget, bstat, btree)
go build ./cmd/...
```

After building, place the binaries somewhere on your `$PATH`:

```bash
# Example: install into ~/bin
go install ./cmd/...
```

Verify the installation:

```bash
bitfs --version
# bitfs version 0.1.0-dev
```

Run `bitfs --help` to see all available commands.

---

## 2. Wallet Setup

Before using BitFS you must initialize an HD wallet. The wallet generates a
BIP39 mnemonic phrase and encrypts the seed with a password using Argon2id.

### Initialize a wallet

```bash
bitfs wallet init
```

You will be prompted to create a password. The command outputs:

- The data directory location (default: `~/.bitfs`)
- A fee address for funding transactions
- Your mnemonic phrase (shown only once -- write it down)

Options:

| Flag | Default | Description |
|------|---------|-------------|
| `--words` | `12` | Mnemonic word count: 12 or 24 |
| `--datadir` | `~/.bitfs` | Data directory path |
| `--password` | (prompted) | Wallet password (for scripting/testing) |

Example with 24-word mnemonic:

```bash
bitfs wallet init --words 24
```

A `default` vault is created automatically during wallet initialization.

### Show wallet information

```bash
bitfs wallet show
```

Displays the network, fee address, fee key derivation path, and a list of all
vaults with their account indices and root public key prefixes.

---

## 3. Managing Vaults

A vault is a top-level directory tree with its own BIP32 account key. Each
vault has an independent key hierarchy, so files in different vaults are
cryptographically isolated.

### Create a vault

```bash
bitfs vault create <name>
```

Outputs the account index, root key path, and root public key.

### List vaults

```bash
bitfs vault list
```

### Rename a vault

```bash
bitfs vault rename <old-name> <new-name>
```

### Delete a vault

```bash
bitfs vault delete <name>
```

This is a soft delete; the key material is not destroyed.

All vault commands accept `--datadir` and `--password` flags.

---

## 4. Creating Directories and Uploading Files

### Create a directory

```bash
bitfs mkdir <remote-path> [--vault <name>]
```

Example:

```bash
bitfs mkdir /docs
bitfs mkdir /docs/reports --vault myproject
```

### Upload a file

```bash
bitfs put <local-file> <remote-path> [--vault <name>] [--access free|private]
```

The first argument is the local filesystem path; the second is the destination
path inside your vault.

Examples:

```bash
# Upload a file with free access (default)
bitfs put ./README.md /docs/README.md

# Upload as private (encrypted, owner-only)
bitfs put ./secrets.json /private/secrets.json --access private

# Upload to a specific vault
bitfs put ./report.pdf /reports/Q1.pdf --vault work
```

On success the command prints the transaction ID and raw transaction hex.

---

## 5. Reading Content with b-tools

BitFS ships five read-only utilities that mirror familiar Unix commands. They
connect to a running BitFS daemon and accept `bitfs://` URIs.

### URI formats

BitFS URIs support three authority formats. Domain-based URIs are the
recommended form for human use:

| Format | Example | Resolution |
|--------|---------|------------|
| **Domain** | `bitfs://example.com/path` | DNS TXT `_bitfs.example.com` → pubkey |
| **Paymail** | `bitfs://alice@example.com/path` | Paymail PKI → pubkey |
| **Pubkey** | `bitfs://02abc.../path` | Direct 66-char hex pubkey (requires `--host`) |

Domain and paymail URIs resolve the daemon endpoint automatically via DNS.
Bare pubkey URIs require `--host` to specify which daemon to connect to.

All b-tools share these common flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--host` | `http://localhost:8080` | Daemon URL |
| `--timeout` | (none) | Request timeout (e.g. `10s`, `1m`) |

### bls -- list directory contents

Like `ls`. Lists children of a directory node.

```bash
bls bitfs://example.com/docs/
```

Options:

| Flag | Description |
|------|-------------|
| `--long`, `-l` | Detailed listing (type, access, size, name) |
| `--json` | JSON output |

Examples:

```bash
# Domain-based URI (recommended)
bls bitfs://example.com/

# Paymail URI
bls bitfs://alice@example.com/docs/

# Detailed listing
bls -l bitfs://example.com/docs/

# Machine-readable JSON
bls --json bitfs://example.com/docs/

# Bare pubkey (requires --host)
bls --host http://localhost:8080 bitfs://02abc123.../docs/
```

### bstat -- show file metadata

Like `stat`. Displays detailed metadata for a single file or directory.

```bash
bstat bitfs://example.com/docs/README.md
```

Output fields include: path, type, owner (pnode), access mode, MIME type,
size, content hash, price (if paid), and transaction ID.

Options:

| Flag | Description |
|------|-------------|
| `--json` | JSON output |
| `--versions` | Show version history (planned) |

### bcat -- output file content to stdout

Like `cat`. Fetches file content and writes it to standard output.

```bash
bcat bitfs://example.com/docs/README.md
```

For free content, `bcat` streams the data directly. For paid content, see
[Section 7: Buying Content](#7-buying-content).

Private content cannot be accessed remotely.

Options:

| Flag | Description |
|------|-------------|
| `--buy` | Attempt to purchase paid content |
| `--wallet-key` | Hex-encoded buyer private key (32 bytes (raw scalar)) |

### bget -- download file to disk

Like `wget`. Downloads a file and saves it locally.

```bash
bget bitfs://example.com/docs/report.pdf
```

The output filename is derived from the URI path. Override it with `-o`:

```bash
bget -o my-report.pdf bitfs://example.com/docs/report.pdf
```

Options:

| Flag | Description |
|------|-------------|
| `-o`, `--output` | Output filename |
| `--buy` | Attempt to purchase paid content |
| `--wallet-key` | Hex-encoded buyer private key |

### btree -- recursive directory tree

Like `tree`. Displays a visual tree of directories and files.

```bash
btree bitfs://example.com/
```

Example output:

```
/
+-- docs/
|   +-- README.md
|   +-- report.pdf
+-- images/
    +-- logo.png

2 directories, 3 files
```

Options:

| Flag | Description |
|------|-------------|
| `-d`, `--depth` | Max depth (0 = unlimited) |
| `--json` | JSON output |

---

## 6. Selling Content

The `sell` command sets a price on content, changing its access mode to
**paid**. Buyers must complete an HTLC atomic swap to receive a decryption
capsule.

```bash
bitfs sell <remote-path> --price <sats-per-KB> [--vault <name>]
```

The price is specified in satoshis per kilobyte.

Example:

```bash
# Price a file at 100 sats/KB
bitfs sell /docs/premium-report.pdf --price 100

# Price a file in a specific vault
bitfs sell /music/track01.mp3 --price 50 --vault media
```

On success the command prints the transaction ID for the price-setting
transaction.

---

## 7. Buying Content

When a file has the **paid** access mode, `bcat` and `bget` require the
`--buy` flag along with a `--wallet-key` to complete the purchase.

The purchase flow is fully automated:

1. The tool queries the daemon for buy info (capsule hash, price, payment address)
2. An HTLC (Hash Time-Locked Contract) transaction is built and submitted
3. The daemon returns a decryption capsule
4. The tool fetches the encrypted content and decrypts it using the capsule

### Buy and print to stdout

```bash
bcat --buy --wallet-key <hex-private-key> bitfs://example.com/docs/premium-report.pdf
```

### Buy and download to disk

```bash
bget --buy --wallet-key <hex-private-key> bitfs://example.com/docs/premium-report.pdf
bget --buy --wallet-key <hex-private-key> -o report.pdf bitfs://example.com/docs/premium-report.pdf
```

The `--wallet-key` accepts a 32-byte raw scalar or 33-byte compressed private
key, both hex-encoded.

If you attempt to access paid content without `--buy`, the tool prints the
price and file size, then exits:

```
bcat: content requires payment: 100 sat/KB (51200 bytes)
Use --buy to purchase
```

---

## 8. Publishing with DNSLink

Publishing binds a DNS domain to your vault, allowing human-readable access
via `bitfs://example.com/path` instead of raw public keys.

### Publish a domain

```bash
bitfs publish <domain> [--vault <name>]
```

The command outputs the DNS TXT record you need to add at your DNS provider:

```
_bitfs.example.com  TXT  "bitfs=<vault-root-pubkey>"
```

Add this TXT record with your DNS registrar, then wait for DNS propagation.

### List current bindings

Run `publish` with no arguments to see all active domain bindings and their
verification status:

```bash
bitfs publish
```

### Unpublish a domain

```bash
bitfs unpublish <domain>
```

This prints instructions for removing the DNS TXT record:

```
To unpublish from example.com, remove the following DNS TXT record:

  _bitfs.example.com  TXT  (delete this record)

After DNS propagation, the domain will no longer resolve to your BitFS vault.
```

### Complete publish-to-access example

Once a domain is published and DNS propagated, anyone can access your content
using human-readable URIs:

```bash
# Owner: upload content and publish domain
bitfs put ./report.pdf /docs/report.pdf
bitfs publish example.com

# Add the DNS TXT record as instructed, then verify:
bitfs publish    # shows "verified" status

# Visitor: access via domain URI (no --host needed)
bls bitfs://example.com/docs/
bcat bitfs://example.com/docs/report.pdf
bget bitfs://example.com/docs/report.pdf
btree bitfs://example.com/
```

---

## 9. Running the Daemon

The BitFS daemon serves content over HTTP, implements the x402 payment
protocol for paid content, and provides the API that b-tools connect to.

### Start the daemon

```bash
bitfs daemon start [--listen <addr>] [--datadir <path>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--listen` | `:8080` | Listen address (host:port) |
| `--datadir` | `~/.bitfs` | Data directory |
| `--password` | (prompted) | Wallet password |

Example:

```bash
# Start on the default port
bitfs daemon start

# Start on a custom port
bitfs daemon start --listen :9090
```

The daemon runs in the foreground and writes a PID file to
`<datadir>/daemon.pid`. It shuts down gracefully on SIGINT (Ctrl+C) or
SIGTERM.

### Stop the daemon

From another terminal:

```bash
bitfs daemon stop
```

This reads the PID file and sends SIGTERM to the running daemon process.

---

## 10. Shell REPL Usage

The `shell` command opens an FTP-style interactive session for managing files
inside a vault. It is useful for performing multiple operations without
re-entering your password each time.

```bash
bitfs shell [--vault <name>]
```

You will see a prompt like:

```
BitFS Shell (vault 0). Type 'help' for commands, 'quit' to exit.
bitfs:/>
```

### Available shell commands

| Command | Description |
|---------|-------------|
| `ls [path]` | List directory contents |
| `cd [path]` | Change remote working directory |
| `lcd [path]` | Change (or print) local working directory |
| `pwd` | Print remote working directory |
| `cat <path> [--force]` | Display file contents |
| `mkdir <path>` | Create a directory |
| `put <local> <remote> [access]` | Upload a file (access: free/private/paid) |
| `get <remote> [local]` | Download a file |
| `mput <dir> [remote-dir]` | Batch upload files |
| `mget <dir> [local-dir]` | Batch download files |
| `cp <src> <dst>` | Copy a file |
| `rm [-r] <path>` | Remove a file or directory (-r recursive) |
| `mv <src> <dst>` | Move or rename |
| `link [-s] <target> <name>` | Create a hard or soft link (-s for soft link) |
| `sell <path> <price> [--recursive]` | Set price in sats/KB |
| `encrypt <path>` | Change access from free to private |
| `decrypt <path>` | Change access from private to free |
| `publish [domain]` | Publish or list DNSLink bindings |
| `unpublish <domain>` | Unbind a domain |
| `sales` | View sales records |
| `help` | Show command list |
| `quit` or `exit` | Exit the shell |

### Example session

```
bitfs:/> mkdir /photos
Created directory /photos
bitfs:/> cd /photos
bitfs:/photos> put ~/vacation.jpg vacation.jpg
Uploaded vacation.jpg
bitfs:/photos> ls
  file  vacation.jpg
bitfs:/photos> sell vacation.jpg 10
Set price for /photos/vacation.jpg: 10 sat/KB
bitfs:/photos> cd /
bitfs:/> ls
  dir   photos
bitfs:/> quit
Bye.
```

Paths in the shell are resolved relative to the current remote working
directory. Absolute paths (starting with `/`) are also accepted.

---

## 11. Additional File Operations

### Remove a file or directory

```bash
bitfs rm <remote-path> [--vault <name>]
```

### Move or rename

```bash
bitfs mv <src> <dst> [--vault <name>]
```

### Copy a file

```bash
bitfs cp <src> <dst> [--vault <name>]
```

### Create a link

Hard link (default):

```bash
bitfs link <target> <link-path> [--vault <name>]
```

Soft (symbolic) link:

```bash
bitfs link <target> <link-path> --soft [--vault <name>]
```

### Encrypt content

Convert a free file to private (encrypted):

```bash
bitfs encrypt <remote-path> [--vault <name>]
```

This changes the access mode from `free` to `private`. The content is
re-encrypted so that only the vault owner can decrypt it.

### Register external UTXOs

If you funded your fee address externally, register the UTXOs so BitFS can
spend them for transaction fees:

```bash
bitfs fund
```

---

## 12. Common Options

Most commands accept these shared flags:

| Flag | Default | Description |
|------|---------|-------------|
| `--datadir` | `~/.bitfs` | Path to the BitFS data directory |
| `--vault` | (first vault) | Vault name to operate on |
| `--password` | (prompted) | Wallet password (avoid in production; use for scripting/testing) |

### Getting help

Every command supports `--help`:

```bash
bitfs --help
bitfs wallet --help
bitfs wallet init --help
bitfs daemon --help
```

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | General error |
| 2 | Usage error |
| 3 | Wallet error |
| 4 | Network error |
| 5 | Permission / payment error |
| 6 | Not found |
| 7 | Conflict |

---

## Quick Start Summary

```bash
# 1. Initialize wallet
bitfs wallet init

# 2. Create a directory
bitfs mkdir /mysite

# 3. Upload a file
bitfs put ./index.html /mysite/index.html

# 4. Start the daemon
bitfs daemon start &

# 5. Browse your files (after publishing a domain)
bls bitfs://example.com/mysite/
bcat bitfs://example.com/mysite/index.html

# 6. Sell premium content
bitfs put ./ebook.pdf /mysite/ebook.pdf
bitfs sell /mysite/ebook.pdf --price 200

# 7. Publish to a domain
bitfs publish example.com
# Then add the DNS TXT record as instructed

# 8. Interactive session
bitfs shell
```
