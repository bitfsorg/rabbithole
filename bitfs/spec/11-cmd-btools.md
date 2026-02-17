# Module Specification: cmd/b* (Read-only Tools)

## PURPOSE

Five independent read-only CLI tools for querying the BitFS filesystem. These are stateless visitor tools that do not require a wallet. They follow Unix conventions for composability via pipes.

Design references: ConceptDesign #1, #2, #3; SystemDesign section 8.

## TOOLS

### cmd/bls -- List Directory (like `ls`)

```
bls [OPTIONS] bitfs://<authority>/<path>

Options:
  -l, --long       Detailed listing
  --json           JSON output
  --keyword <kw>   Filter by keyword
  --no-cache       Skip cache
  --timeout N      Timeout in seconds
  --offline        Cache-only mode

Output (default):
  readme.txt    4.2 KB    2026-02-14    [free]
  images/       dir       2026-02-13
  report.pdf    1.2 MB    2026-02-10    [paid: 50 sat/KB]

Output (--json):
  [{"name":"readme.txt","type":"file","size":4300,"access":"free",...}]
```

### cmd/bcat -- Output File Content (like `cat`)

```
bcat [OPTIONS] bitfs://<authority>/<path>

Options:
  --buy            Auto-purchase (paid content)
  --json           JSON envelope output
  --no-cache       Skip cache
  --timeout N      Timeout

Behavior:
  - Free content: auto-decrypt using D_node=1 trick
  - Cached paid content: use cached key
  - Uncached paid content: show price, suggest --buy
```

### cmd/bget -- Download File (like `wget`)

```
bget [OPTIONS] bitfs://<authority>/<path>

Options:
  -o <file>        Output filename
  --buy            Auto-purchase
  --version N      Download specific version
  --json           JSON progress output
  --no-cache       Skip cache
  --timeout N      Timeout

Behavior:
  - Downloads file to local filesystem
  - Free content: auto-decrypt
  - Paid content without --buy: show price and usage hint
  - Paid with --buy: HTLC purchase + download + cache key
```

### cmd/bstat -- File Metadata (like `stat`)

```
bstat [OPTIONS] bitfs://<authority>/<path>

Options:
  --versions       Show all versions
  --json           JSON output
  --no-cache       Skip cache

Output:
    File: readme.txt
    Type: file
    Hash: 3a7bd3e2...
    Size: 4.2 KB
   Owner: 02a1b2c3...
    TxID: abc123...
    Time: 2026-02-14 10:30:00 UTC
  Access: free
```

### cmd/btree -- Directory Tree (like `tree`)

```
btree [OPTIONS] bitfs://<authority>/<path>

Options:
  -d N             Max depth
  --json           JSON output
  --no-cache       Skip cache

Output:
  example.com/
  +-- docs/
  |   +-- readme.txt (4.2 KB) [free]
  |   +-- images/
  |       +-- logo.png (12 KB) [free]
  +-- premium/
  |   +-- data.csv (10 KB) [paid: 50 sat/KB]
  +-- LICENSE (1.1 KB) [free]
```

## SHARED IMPLEMENTATION

All b* tools share:
- URI parsing via `internal/paymail.ParseURI()`
- Endpoint resolution via `internal/paymail.ResolveURI()`
- Metadata fetching from daemon via HTTP
- Local cache in `~/.bitfs/cache/meta/` (optional)
- Common flags: `--json`, `--no-cache`, `--timeout`, `--offline`
- Exit codes: same as cmd/bitfs

## DEPENDENCIES

- `github.com/spf13/cobra` -- CLI framework
- `internal/paymail` -- URI resolution
- `internal/method42` -- Decryption (for free content)
- `internal/x402` -- Payment handling (for --buy)
- `net/http` -- Daemon API client

## ERROR HANDLING

Same exit codes as cmd/bitfs (0-7). All tools gracefully handle:
- Network timeouts with retry
- Missing cache entries
- Invalid URIs
- Payment required responses

## SECURITY CONSIDERATIONS

1. **Read-only**: These tools never write to the blockchain or modify local wallet state.
2. **Key caching**: Purchased keys are cached locally in encrypted form. Tools read cached keys but only b-tools with --buy flag trigger purchases.
3. **No wallet required**: Default operation requires no wallet. Only --buy flag triggers wallet interaction.
