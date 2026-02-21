# BitFS Daemon API Reference

The BitFS daemon (LFCP -- Local Full-Copy Peer) exposes an HTTP API for content
retrieval, Metanet metadata queries, Method 42 ECDH identity handshake, x402
payment handling, Paymail/BSV Alias PKI resolution, and content negotiation.

Default listen address: `:8080`. TLS is optional. All endpoints apply per-IP
token-bucket rate limiting (default 60 RPM, burst 20) and CORS headers.

All error responses share a common JSON envelope:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable description",
    "retry": false,
    "cached": false
  }
}
```

The `retry` field is `true` for 429 and 5xx status codes.

---

## Table of Contents

- [Health](#health)
  - [GET /_bitfs/health](#get-_bitfshealth)
- [Method 42 Handshake](#method-42-handshake)
  - [POST /_bitfs/handshake](#post-_bitfshandshake)
- [Content](#content)
  - [GET /_bitfs/data/{hash}](#get-_bitfsdatahash)
- [Metadata](#metadata)
  - [GET /_bitfs/meta/{pnode}/{path...}](#get-_bitfsmetapnodepath)
- [Payment](#payment)
  - [GET /_bitfs/buy/{txid}](#get-_bitfsbuytxid)
  - [POST /_bitfs/buy/{txid}](#post-_bitfsbuytxid)
- [Paymail / BSV Alias](#paymail--bsv-alias)
  - [GET /.well-known/bsvalias](#get-well-knownbsvalias)
  - [GET /api/v1/pki/{handle}](#get-apiv1pkihandle)
- [Content Negotiation](#content-negotiation)
  - [GET /{path}](#get-path)

---

## Health

### GET /_bitfs/health

Return the daemon health status.

**URL Parameters:** None

**Query Parameters:** None

**Request Body:** None

**Response Body:**

```json
{
  "status": "ok"
}
```

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Daemon is healthy |
| 429  | Rate limited |

**Example:**

```bash
curl http://localhost:8080/_bitfs/health
```

---

## Method 42 Handshake

### POST /_bitfs/handshake

Initiate a Method 42 ECDH handshake. The buyer sends their compressed public key
and a random nonce. The seller responds with its own public key, nonce, and a
session ID. Both parties independently derive the session key as
`SHA256(ECDH(D_seller, P_buyer).x || nonce_b || nonce_s)`.

Sessions expire after 24 hours by default.

**URL Parameters:** None

**Query Parameters:** None

**Request Body:**

```json
{
  "buyer_pub": "<66-char hex, 33-byte compressed public key>",
  "nonce_b":   "<hex-encoded random nonce>",
  "timestamp": 1740000000
}
```

| Field       | Type   | Required | Description |
|-------------|--------|----------|-------------|
| `buyer_pub` | string | yes      | Buyer's compressed public key (66 hex chars / 33 bytes) |
| `nonce_b`   | string | yes      | Buyer's random nonce (hex-encoded, non-empty) |
| `timestamp` | int64  | no       | Unix timestamp of the request |

**Response Body (200):**

```json
{
  "seller_pub": "02abc123...def",
  "nonce_s":    "a1b2c3d4...64hex",
  "timestamp":  1740000001,
  "session_id": "f0e1d2c3b4a59687",
  "expires_at": 1740086401
}
```

| Field        | Type   | Description |
|--------------|--------|-------------|
| `seller_pub` | string | Seller's compressed public key (66 hex chars) |
| `nonce_s`    | string | Seller's random nonce (64 hex chars / 32 bytes) |
| `timestamp`  | int64  | Unix timestamp of the response |
| `session_id` | string | Session identifier for subsequent requests |
| `expires_at` | int64  | Unix timestamp when the session expires |

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Handshake successful |
| 400  | Missing or invalid field (`MISSING_FIELD`, `INVALID_PUBKEY`, `INVALID_NONCE`, `INVALID_JSON`, `INVALID_REQUEST`) |
| 429  | Rate limited |
| 500  | Server-side key or ECDH error (`KEY_ERROR`, `NONCE_ERROR`, `ECDH_ERROR`) |

**Example:**

```bash
curl -X POST http://localhost:8080/_bitfs/handshake \
  -H "Content-Type: application/json" \
  -d '{
    "buyer_pub": "02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2",
    "nonce_b": "deadbeefcafebabe0123456789abcdef0123456789abcdef0123456789abcdef",
    "timestamp": 1740000000
  }'
```

---

## Content

### GET /_bitfs/data/{hash}

Retrieve encrypted content by its SHA-256 key hash from the local content store.

**URL Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `hash`    | string | SHA-256 key hash as 64 hex characters (32 bytes) |

**Query Parameters:** None

**Request Body:** None

**Response Headers:**

| Header           | Description |
|------------------|-------------|
| `Content-Type`   | `application/octet-stream` |
| `Content-Length`  | Size of the encrypted content in bytes |
| `X-Key-Hash`     | Echo of the requested key hash |

**Response Body (200):** Raw encrypted bytes (binary).

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Content retrieved successfully |
| 400  | Missing hash (`MISSING_HASH`) or invalid hash format (`INVALID_HASH`) |
| 404  | Content not found (`NOT_FOUND`) |
| 429  | Rate limited |
| 500  | Storage error (`STORAGE_ERROR`) |

**Example:**

```bash
curl http://localhost:8080/_bitfs/data/e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855 \
  -o encrypted.bin
```

---

## Metadata

### GET /_bitfs/meta/{pnode}/{path...}

Query Metanet node metadata by the node's compressed public key (P_node) and
filesystem path. Returns file/directory metadata including access mode, MIME type,
size, key hash, and child entries for directories.

**URL Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `pnode`   | string | Compressed public key of the Metanet node (66 hex chars / 33 bytes) |
| `path`    | string | Filesystem path relative to the node (e.g., `docs/readme.txt`). A leading `/` is prepended automatically if missing. |

**Query Parameters:** None

**Request Body:** None

**Response Body (200) -- File:**

```json
{
  "pnode":       "02abc123...def",
  "path":        "/docs/readme.txt",
  "type":        "file",
  "access":      "free",
  "mime_type":   "text/plain",
  "file_size":   4096,
  "key_hash":    "e3b0c442...b855"
}
```

**Response Body (200) -- Directory:**

```json
{
  "pnode":    "02abc123...def",
  "path":     "/docs",
  "type":     "dir",
  "access":   "free",
  "children": [
    { "name": "readme.txt", "type": "file" },
    { "name": "images",     "type": "dir"  }
  ]
}
```

**Response Body (200) -- Paid file:**

```json
{
  "pnode":        "02abc123...def",
  "path":         "/premium/data.bin",
  "type":         "file",
  "access":       "paid",
  "mime_type":    "application/octet-stream",
  "file_size":    1048576,
  "key_hash":     "a1b2c3d4...f0e1",
  "price_per_kb": 10
}
```

| Field          | Type     | Presence    | Description |
|----------------|----------|-------------|-------------|
| `pnode`        | string   | always      | Compressed public key (hex) |
| `path`         | string   | always      | Resolved filesystem path |
| `type`         | string   | always      | `"file"`, `"dir"`, or `"link"` |
| `access`       | string   | always      | `"free"`, `"paid"`, or `"private"` |
| `mime_type`    | string   | files only  | MIME type of the file |
| `file_size`    | uint64   | files only  | Size in bytes |
| `key_hash`     | string   | files only  | SHA-256 key hash (hex) |
| `price_per_kb` | uint64   | paid only   | Price in satoshis per kilobyte |
| `children`     | array    | dirs only   | Array of `{name, type}` child entries |

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Metadata returned successfully |
| 400  | Missing P_node (`MISSING_PNODE`), invalid P_node (`INVALID_PNODE`), or path traversal (`INVALID_PATH`) |
| 404  | Path not found (`NOT_FOUND`) |
| 429  | Rate limited |
| 500  | Internal resolution error (`INTERNAL_ERROR`) |
| 503  | Metanet service unavailable (`SERVICE_UNAVAILABLE`) |

**Example:**

```bash
curl http://localhost:8080/_bitfs/meta/02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2/docs/readme.txt
```

---

## Payment

Payment follows the x402 protocol. When a client accesses paid content via
content negotiation (`GET /{path}`), the daemon responds with HTTP 402 and an
invoice. The client then uses the buy endpoints to inspect and fulfill that
invoice by submitting an HTLC transaction.

### GET /_bitfs/buy/{txid}

Retrieve invoice details for a pending content purchase.

**URL Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `txid`    | string | Invoice ID returned by the 402 response |

**Query Parameters:** None

**Request Body:** None

**Response Body (200):**

```json
{
  "invoice_id":   "inv-abc123",
  "total_price":  1024,
  "capsule_hash": "f0e1d2c3...a5b6",
  "price_per_kb": 10,
  "file_size":    102400,
  "payment_addr": "1BitFS02a1b2c3d4e5f6",
  "paid":         false
}
```

| Field          | Type   | Description |
|----------------|--------|-------------|
| `invoice_id`   | string | Unique invoice identifier |
| `total_price`  | uint64 | Total price in satoshis |
| `capsule_hash` | string | SHA-256 of the content key hash (hex) |
| `price_per_kb` | uint64 | Price per kilobyte in satoshis |
| `file_size`    | uint64 | File size in bytes |
| `payment_addr` | string | BSV payment address |
| `paid`         | bool   | Whether the invoice has been fulfilled |

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Invoice found |
| 400  | Missing invoice ID (`MISSING_TXID`) |
| 404  | Invoice not found (`NOT_FOUND`) or expired (`EXPIRED`) |
| 429  | Rate limited |

**Example:**

```bash
curl http://localhost:8080/_bitfs/buy/inv-abc123
```

### POST /_bitfs/buy/{txid}

Submit an HTLC transaction to pay for content. On successful payment
verification, the server returns the encrypted content capsule (hex-encoded).

**URL Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `txid`    | string | Invoice ID to pay |

**Query Parameters:** None

**Request Body:** Raw HTLC transaction bytes (binary). Maximum size: 1 MB.

**Response Body (200):**

```json
{
  "invoice_id": "inv-abc123",
  "capsule":    "0a1b2c3d4e5f...encrypted-hex-content",
  "paid":       true
}
```

| Field        | Type   | Description |
|--------------|--------|-------------|
| `invoice_id` | string | The fulfilled invoice ID |
| `capsule`    | string | Hex-encoded encrypted content |
| `paid`       | bool   | Always `true` on success |

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Payment accepted, capsule returned |
| 400  | Missing invoice ID (`MISSING_TXID`), empty transaction (`EMPTY_TX`), unreadable body (`INVALID_REQUEST`), or payment verification failure (`PAYMENT_INVALID`) |
| 404  | Invoice not found (`NOT_FOUND`), expired (`EXPIRED`), or no content key hash (`NO_CONTENT`), or content missing from store (`CONTENT_NOT_FOUND`) |
| 409  | Invoice already paid (`ALREADY_PAID`) |
| 429  | Rate limited |
| 500  | Storage error (`STORAGE_ERROR`) |

**Example:**

```bash
curl -X POST http://localhost:8080/_bitfs/buy/inv-abc123 \
  -H "Content-Type: application/octet-stream" \
  --data-binary @htlc_tx.bin
```

---

## Paymail / BSV Alias

### GET /.well-known/bsvalias

Return the BSV Alias (Paymail) capability discovery document. Clients use this to
discover the PKI and public-profile endpoint URL templates.

**URL Parameters:** None

**Query Parameters:** None

**Request Body:** None

**Response Body (200):**

```json
{
  "bsvalias": "1.0",
  "capabilities": {
    "pki":          "https://example.com/api/v1/pki/{alias}@{domain.tld}",
    "f12f968c92d6": "https://example.com/api/v1/public-profile/{alias}@{domain.tld}"
  }
}
```

| Field            | Type   | Description |
|------------------|--------|-------------|
| `bsvalias`       | string | Protocol version (always `"1.0"`) |
| `capabilities`   | object | Map of capability IDs to URL templates |
| `capabilities.pki` | string | PKI endpoint URL template |
| `capabilities.f12f968c92d6` | string | Public profile endpoint URL template |

The base URL is constructed from the request's `Host` header and the daemon's TLS
configuration (`https://` when TLS is enabled, `http://` otherwise).

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Capabilities document returned |
| 429  | Rate limited |

**Example:**

```bash
curl http://localhost:8080/.well-known/bsvalias
```

### GET /api/v1/pki/{handle}

Resolve a Paymail handle to its vault's compressed public key.

**URL Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `handle`  | string | Paymail handle in `alias@domain` format |

**Query Parameters:** None

**Request Body:** None

**Response Body (200):**

```json
{
  "bsvalias": "1.0",
  "handle":   "alice@bitfs.org",
  "pubkey":   "02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2"
}
```

| Field      | Type   | Description |
|------------|--------|-------------|
| `bsvalias` | string | Protocol version (always `"1.0"`) |
| `handle`   | string | The requested handle |
| `pubkey`   | string | Compressed public key (hex, 66 characters) |

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Public key resolved |
| 400  | Missing or malformed handle (`INVALID_HANDLE`); must be `alias@domain` format |
| 404  | Unknown alias (`NOT_FOUND`) |
| 429  | Rate limited |

**Example:**

```bash
curl http://localhost:8080/api/v1/pki/alice@bitfs.org
```

---

## Content Negotiation

### GET /{path}

Serve content at the given filesystem path with content negotiation based on the
`Accept` header. This is the primary endpoint for browsing the BitFS filesystem
over HTTP.

If the resolved node has `access: "paid"` and x402 is enabled, the daemon returns
an HTTP 402 response with an invoice (see [Payment](#payment)).

**URL Parameters:**

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `path`    | string | Filesystem path (e.g., `/docs/readme.txt`). The root path `/` returns basic daemon info or a root directory listing. |

**Query Parameters:** None

**Request Body:** None

**Content Negotiation:**

The response format is determined by the `Accept` header:

| Accept Header        | Response Content-Type           | Description |
|----------------------|---------------------------------|-------------|
| `text/html`          | `text/html; charset=utf-8`      | HTML page with directory listing or file info |
| `text/markdown`      | `text/markdown; charset=utf-8`  | Markdown document (designed for CLI agents) |
| `application/json`   | `application/json`              | JSON metadata object |
| (default/other)      | `application/json`              | Falls back to JSON |

**Response Body (200) -- JSON, file node:**

```json
{
  "pnode":       "02abc123...def",
  "type":        "file",
  "mime_type":   "text/plain",
  "file_size":   4096,
  "key_hash":    "e3b0c442...b855",
  "access":      "free",
  "price_per_kb": 0,
  "domain":      "bitfs.org",
  "children":    null
}
```

**Response Body (200) -- JSON, root fallback (no Metanet service):**

```json
{
  "node": "BitFS LFCP",
  "path": "/"
}
```

**Response Body (200) -- HTML, directory node:**

```html
<!DOCTYPE html>
<html><head><title>BitFS Directory</title></head>
<body>
  <h1>Directory</h1>
  <ul>
    <li><a href="readme.txt">readme.txt</a> (file)</li>
    <li><a href="images">images</a> (dir)</li>
  </ul>
</body></html>
```

**Response Body (200) -- Markdown, directory node:**

```markdown
# Directory Listing

- readme.txt (file)
- images (dir)
```

**Response Body (402) -- Paid content (x402 enabled):**

The response includes x402 HTTP headers and a JSON invoice body:

```json
{
  "error":        "payment required",
  "invoice_id":   "inv-abc123",
  "total_price":  1024,
  "price_per_kb": 10,
  "file_size":    102400,
  "payment_addr": "1BitFS02a1b2c3d4e5f6"
}
```

**Status Codes:**

| Code | Meaning |
|------|---------|
| 200  | Content served successfully |
| 400  | Path traversal attempt (`INVALID_PATH`) |
| 402  | Payment required (paid content with x402 enabled) |
| 404  | Path not found (`NOT_FOUND`) |
| 429  | Rate limited |

**Examples:**

```bash
# JSON (default)
curl http://localhost:8080/docs/readme.txt

# HTML
curl -H "Accept: text/html" http://localhost:8080/docs/

# Markdown (agent-friendly)
curl -H "Accept: text/markdown" http://localhost:8080/docs/

# Root path
curl http://localhost:8080/
```

---

## CORS

All endpoints include CORS headers. Preflight `OPTIONS` requests are supported on
`/_bitfs/health`, `/_bitfs/handshake`, and `/_bitfs/buy/{txid}`.

Default CORS configuration:

| Header                         | Default Value |
|--------------------------------|---------------|
| `Access-Control-Allow-Origin`  | `*` (configurable) |
| `Access-Control-Allow-Methods` | `GET, POST, OPTIONS` |
| `Access-Control-Allow-Headers` | `Content-Type, Authorization, X-Session-Id` |

---

## Rate Limiting

All endpoints are subject to per-IP token-bucket rate limiting. When the limit is
exceeded, the daemon returns:

```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json

{
  "error": {
    "code": "RATE_LIMITED",
    "message": "Too many requests",
    "retry": true,
    "cached": false
  }
}
```

Default limits:

| Parameter | Default |
|-----------|---------|
| RPM       | 60      |
| Burst     | 20      |
