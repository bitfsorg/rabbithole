# Module Specification: cmd/bitfs

## PURPOSE

Main CLI binary for BitFS -- the owner/writer interface for the decentralized encrypted file system. Implements all file management, encryption, trading, wallet, publishing, and daemon commands. Built with Cobra for subcommand management and Viper for configuration.

Design references: ConceptDesign #1, #2, #19; SystemDesign sections 9, 10; DetailedDesign section 9-B.

## PUBLIC API

### Subcommands

```
bitfs init [--network <net>]           Initialize wallet + first vault
bitfs put <local> <remote>             Upload file (create or update)
bitfs put --encrypt <local> <remote>   Upload encrypted (private mode)
bitfs mkdir <path>                     Create directory
bitfs rm <path>                        Delete file (remove ChildEntry from parent)
bitfs rm -r <path>                     Recursive delete
bitfs rmdir <path>                     Delete empty directory
bitfs mv <src> <dst>                   Move/rename
bitfs cp <src> <dst>                   Copy (independent new node)
bitfs link <target> <name>             Hard link
bitfs link -s <target> <name>          Soft link (local)
bitfs link -s <domain/path> <name>     Soft link (remote)
bitfs encrypt <path>                   Free -> Private
bitfs decrypt <path>                   Private -> Free
bitfs sell <path> --price <sat/KB>     Set price (PAID mode)
bitfs sell <path> --recursive          Recursive pricing
bitfs sales [path]                     View sales history
bitfs publish <domain> [path]          Bind domain via DNSLink
bitfs unpublish <domain>               Unbind domain
bitfs publish                          List bindings
bitfs vault create <name>              Create new vault
bitfs vault list                       List vaults
bitfs vault use <name>                 Switch active vault
bitfs vault info [name]                Show vault details
bitfs vault rename <old> <new>         Rename vault
bitfs vault delete <name>              Delete vault (soft)
bitfs wallet init                      Create HD wallet
bitfs wallet restore                   Restore from mnemonic
bitfs wallet info                      Balance, address, network
bitfs wallet fund                      Show deposit address
bitfs daemon start [-d]                Start daemon (optionally background)
bitfs daemon stop                      Stop daemon
bitfs daemon status                    Show daemon status
bitfs daemon config                    Show daemon configuration
bitfs shell                            FTP-style interactive REPL
```

### Global Flags

```
--json              JSON output (agent-friendly)
--no-cache          Disable local cache
--timeout N         Request timeout (seconds)
--offline           Force cache-only mode
--home <path>       Override BITFS_HOME (default ~/.bitfs)
--vault <name>      Override active vault for this command
```

### Exit Codes

```
0 = success
1 = general error
2 = argument error
3 = network error
4 = data validation error
5 = authentication error
6 = not found
7 = payment error
```

## DEPENDENCIES

- `github.com/spf13/cobra` -- CLI framework
- `github.com/spf13/viper` -- Configuration
- `internal/wallet` -- HD wallet operations
- `internal/method42` -- Encryption
- `internal/tx` -- Transaction construction
- `internal/metanet` -- Filesystem operations
- `internal/storage` -- Content storage
- `internal/spv` -- SPV verification
- `internal/daemon` -- Daemon management
- `internal/paymail` -- URI resolution
- `internal/x402` -- Payment protocol

## DATA STRUCTURES

### Configuration File (~/.bitfs/config.toml)
```toml
network = "mainnet"
output = "plain"

[cache]
enabled = true
max_size = "5GB"
meta_ttl = 3600
data_ttl = 86400
eviction = "lru"

[daemon]
listen = "0.0.0.0:80"
```

## ERROR HANDLING

All commands follow the same pattern:
1. Parse arguments, validate inputs
2. Load wallet (if needed), unlock with password
3. Perform operation
4. Output result (plain text or JSON based on --json flag)
5. Return appropriate exit code

Network errors: retry 3x with exponential backoff (1s/2s/4s).
UTXO conflicts: auto-reconstruct and retry (up to 3x).
Insufficient balance: show deficit amount + `bitfs wallet fund` address.

## SECURITY CONSIDERATIONS

1. **Password prompts**: Wallet password is read from terminal (not command line) to avoid shell history exposure.
2. **Session management**: When daemon is running, CLI uses Unix socket (in-memory). Otherwise, uses session files with restricted permissions.
3. **Mnemonic display**: Mnemonic is shown only once during `wallet init`, never stored in plaintext.
