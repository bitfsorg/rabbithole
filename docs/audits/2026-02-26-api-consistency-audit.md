# BitFS Daemon API Consistency Audit Report

**Date**: 2026-02-26
**Scope**: Daemon HTTP API (`bitfs/internal/daemon/`) — endpoint consistency, client-server contract, JSON serialization, error handling, spec conformance
**Auditor**: Claude Opus 4.6 (5 parallel audit agents)
**Status**: Audit findings only — no fixes applied

---

## Executive Summary

Comprehensive audit of the BitFS daemon HTTP API across 5 dimensions: endpoint catalog, client-server contract mismatches, spec vs implementation deviations, JSON serialization, and error handling patterns. The API is generally well-structured with a consistent `writeJSONError()` helper and snake_case naming, but contains **4 HIGH**, **9 MEDIUM**, and **10 LOW** severity findings.

The most impactful issues are: client-server field name mismatches that break paid content purchase flow, a `ChildInfo` struct missing json tags causing PascalCase output in one code path, and error messages leaking internal details.

---

## Endpoint Catalog

14 endpoints across the daemon HTTP API:

| # | Method | Path | Handler | Response Type |
|---|--------|------|---------|---------------|
| 1 | GET | `/_bitfs/health` | `handleHealth` | `application/json` |
| 2 | OPTIONS | `/_bitfs/health` | `handleOptions` | (empty) |
| 3 | POST | `/_bitfs/handshake` | `handleHandshake` | `application/json` |
| 4 | OPTIONS | `/_bitfs/handshake` | `handleOptions` | (empty) |
| 5 | GET | `/_bitfs/data/{hash}` | `handleData` | `application/octet-stream` |
| 6 | GET | `/_bitfs/meta/{pnode}/{path...}` | `handleMeta` | `application/json` |
| 7 | GET | `/_bitfs/buy/{txid}` | `handleGetBuyInfo` | `application/json` |
| 8 | POST | `/_bitfs/buy/{txid}` | `handleSubmitHTLC` | `application/json` |
| 9 | OPTIONS | `/_bitfs/buy/{txid}` | `handleOptions` | (empty) |
| 10 | GET | `/_bitfs/spv/proof/{txid}` | `handleSPVProof` | `application/json` |
| 11 | OPTIONS | `/_bitfs/spv/proof/{txid}` | `handleOptions` | (empty) |
| 12 | GET | `/.well-known/bsvalias` | `handleBSVAlias` | `application/json` |
| 13 | GET | `/api/v1/pki/{handle}` | `handlePKI` | `application/json` |
| 14 | GET | `/` (catch-all) | `handleRootOrPath` | Content-negotiated |

All endpoints set `Content-Type` explicitly. CORS headers applied uniformly via `withMiddleware`.

---

## Findings

### HIGH Severity

#### API-H-1: Client `BuyInfo.Price` field name mismatch with daemon response

**Location**: `client/client.go:62` vs `daemon/payment.go:187`

Client struct expects `"price"`:
```go
// client/client.go:60-65
type BuyInfo struct {
    CapsuleHash  string `json:"capsule_hash"`
    Price        uint64 `json:"price"`          // ← expects "price"
    PaymentAddr  string `json:"payment_addr"`
    SellerPubKey string `json:"seller_pubkey"`
}
```

Daemon sends `"total_price"`:
```go
// daemon/payment.go:185-194
json.NewEncoder(w).Encode(map[string]interface{}{
    "total_price":   invoice.TotalPrice,    // ← sends "total_price"
    ...
})
```

**Impact**: `BuyInfo.Price` always deserializes as 0. Client b-tools cannot determine the correct payment amount for paid content.

---

#### API-H-2: Client `MetaResponse.TxID` expected but never sent by daemon

**Location**: `client/client.go:49` vs `daemon/content.go:121-143`

Client struct declares `TxID`:
```go
// client/client.go:49
TxID string `json:"txid,omitempty"`
```

Daemon's `metaNodeResponse` (content.go:147-157) has no `TxID` field. The `NodeInfo` struct (daemon.go:73-83) also lacks `TxID`.

**Impact**: `bcat` and `bget` call `c.VerifySPV(meta.TxID)` which silently skips when TxID is empty. SPV verification of Metanet transactions never actually runs. The paid content purchase flow depends on `meta.TxID` to call `GetBuyInfo(meta.TxID)`, creating a broken chain.

---

#### API-H-3: `ChildInfo` struct missing json tags — PascalCase in `serveJSON` path

**Location**: `daemon/daemon.go:86-89`, serialized at `daemon/routes.go:269`

```go
// daemon.go:86-89 — NO json tags
type ChildInfo struct {
    Name string
    Type string
}
```

At `routes.go:269`, `node.Children` (type `[]ChildInfo`) is serialized directly into the JSON response map without conversion:
```go
resp["children"] = node.Children  // routes.go:269
```

This produces `{"Name":"foo","Type":"file"}` (PascalCase) instead of `{"name":"foo","type":"file"}`.

Note: `handleMeta` in `content.go:136-139` correctly converts to `metaChildResponse` (which has proper json tags), so the `/_bitfs/meta/` endpoint is fine. Only the `GET /` content-negotiation JSON path is affected.

**Impact**: Two different JSON formats for the same directory children data depending on which endpoint is queried. Client `ChildEntry` expects lowercase field names.

---

#### API-H-4: Error response format mismatch — daemon sends JSON, client reads as plain text

**Location**: `daemon/daemon.go:485-490` vs `client/client.go:246-272`

Daemon `writeJSONError()` sends structured JSON:
```json
{"error":{"code":"NOT_FOUND","message":"Path not found","retry":false,"cached":false}}
```

Client `checkStatus()` reads the body as a raw string:
```go
body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
msg := strings.TrimSpace(string(body))
return fmt.Errorf("%w: %s", ErrNotFound, msg)
```

**Impact**: Error messages to users contain unprocessed JSON blobs: `"client: not found: {"error":{"code":"NOT_FOUND",...}}"`. Functionally correct (sentinel error detection uses status codes), but produces poor user experience and makes error messages difficult to read.

---

### MEDIUM Severity

#### API-M-1: SPV error message leaks internal details

**Location**: `daemon/spv.go:32`

```go
writeJSONError(w, http.StatusBadGateway, "SPV_ERROR", err.Error())
```

Passes upstream SPV service error directly to client. Could expose internal URLs, connection details, or stack traces.

**Recommendation**: Use generic message `"SPV verification failed"`, log actual error server-side.

---

#### API-M-2: HTLC verification error leaks crypto details

**Location**: `daemon/payment.go:254-255`

```go
writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID",
    fmt.Sprintf("HTLC verification failed: %v", err))
```

Exposes internal verification error details to clients.

**Recommendation**: Use generic message `"HTLC verification failed"`.

---

#### API-M-3: Broadcast error leaks blockchain node details

**Location**: `daemon/payment.go:306`

```go
writeJSONError(w, http.StatusBadRequest, "BROADCAST_FAILED",
    fmt.Sprintf("Payment tx not accepted: %v", broadcastErr))
```

Exposes blockchain node error messages.

**Recommendation**: Use generic message `"Payment transaction rejected"`.

---

#### API-M-4: Silent failure in capsule/HTLC script computation

**Location**: `daemon/payment.go:158-170`

```go
sellerPriv2, _, kpErr := d.wallet.GetSellerKeyPair()
var htlcScript []byte
if kpErr == nil {
    htlcScript, _ = x402.BuildHTLC(...)  // ← error ignored
    if len(htlcScript) > 0 {
        invoice.HTLCScript = htlcScript
    }
}
```

If `BuildHTLC()` fails, HTLC script is empty and payment silently falls back to P2PKH verification. Client has no way to know this happened.

**Impact**: Undocumented behavior change. If capsule computation also fails silently, `CapsuleHash` will be empty, and `hex.DecodeString("")` succeeds with `[]byte{}`, leading to an invalid HTLC with all-zeros hash.

---

#### API-M-5: Invalid `buyer_pubkey` query param silently ignored

**Location**: `daemon/payment.go:144`

```go
buyerPubHex := r.URL.Query().Get("buyer_pubkey")
if buyerPubHex != "" && len(buyerPubBytes) == 33 {
    // use it
}
```

If `buyer_pubkey` is present but malformed (not 33 bytes), it's silently ignored. Client gets a response with empty `capsule_hash` and no indication of what went wrong.

**Recommendation**: Return `400 INVALID_PUBKEY` if `buyer_pubkey` is present but invalid.

---

#### API-M-6: Metanet error detection uses fragile string matching

**Location**: `daemon/content.go:106`

```go
if strings.Contains(strings.ToLower(err.Error()), "not found") {
    writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Path not found")
} else {
    writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", ...)
}
```

Error classification depends on whether the error message contains "not found". Fragile — any change in the metanet package's error wording would change the HTTP status code.

**Recommendation**: Use sentinel errors (`errors.Is`) or typed errors for classification.

---

#### API-M-7: Payment endpoint URL parameter semantic mismatch

**Location**: `daemon/routes.go:30-33`, `daemon/payment.go:107-122`

URL parameter is named `{txid}`:
```go
mux.HandleFunc("GET /_bitfs/buy/{txid}", ...)
mux.HandleFunc("POST /_bitfs/buy/{txid}", ...)
```

But semantically it's an `invoice_id` (used to look up `d.invoices[txid]`). Error codes also say `MISSING_TXID`.

**Impact**: API documentation confusion. The `txid` suggests a blockchain transaction ID, but it's actually an invoice identifier generated by `x402.NewInvoice()`.

---

#### API-M-8: x402 402 response uses non-standard error format

**Location**: `daemon/payment.go:97-104`

```go
json.NewEncoder(w).Encode(map[string]interface{}{
    "error":        "payment required",    // flat string
    "invoice_id":   inv.ID,
    "total_price":  inv.Price,
    ...
})
```

All other errors use nested format `{"error":{"code":"...","message":"...",...}}` via `writeJSONError()`. The 402 response uses a flat format with `"error"` as a plain string and additional top-level fields.

**Impact**: Clients cannot use a single error parsing strategy for all non-2xx responses.

---

#### API-M-9: Inconsistent field inclusion strategy across endpoints

`handleMeta` (content.go) and `serveJSON` (routes.go) omit zero-value fields selectively:
```go
if node.MimeType != "" { resp["mime_type"] = node.MimeType }
if node.FileSize > 0 { resp["file_size"] = node.FileSize }
```

`handleGetBuyInfo` (payment.go:185-194) always includes all fields regardless of value.

**Impact**: Inconsistent JSON shape — some responses have nullable fields, others always-present fields.

---

### LOW Severity

#### API-L-1: TX_REUSED error leaks other invoice ID

**Location**: `daemon/payment.go:289`

```go
fmt.Sprintf("Transaction already used for invoice %s", existingInvoice)
```

Reveals which other invoice a transaction was used for — minor privacy leak.

---

#### API-L-2: Paymail PKI echoes user input in error message

**Location**: `daemon/paymail.go:36`

```go
writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "unknown alias: "+alias)
```

Echoes user-supplied alias back in error message.

---

#### API-L-3: No server-side error logging

No `log.Error()` or equivalent calls in any daemon handler. All error information is only sent to the client. Server-side debugging requires reproducing the issue.

---

#### API-L-4: Inconsistent error code naming for missing parameters

| Error Code | Used In |
|------------|---------|
| `MISSING_HASH` | content.go (handleData) |
| `MISSING_PNODE` | content.go (handleMeta) |
| `MISSING_TXID` | payment.go |
| `MISSING_FIELD` | handshake.go (for buyer_pub, nonce_b) |
| `INVALID_PARAM` | spv.go (for txid) |

Same semantic (missing required parameter) uses 3 different naming patterns.

---

#### API-L-5: No `Cache-Control` headers on content-addressed endpoints

`/_bitfs/data/{hash}` serves immutable, content-addressed encrypted data — ideal for `Cache-Control: public, immutable`. No caching headers are set on any endpoint.

---

#### API-L-6: No `X-Request-ID` or request tracing headers

No request correlation mechanism across API calls. Makes debugging multi-step flows (handshake → meta → buy → pay) difficult in production.

---

#### API-L-7: `X-Session-Id` in CORS allow list but unused

**Location**: `daemon/routes.go:91`

```go
w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-Id")
```

`X-Session-Id` is declared in CORS headers but no endpoint validates or uses session-based authentication. Sessions are created during handshake but never enforced.

---

#### API-L-8: Dashboard routes not implemented

**Location**: `daemon/webserve.go:28-29`

```go
// TODO: implement registerDashboardRoutes(mux *http.ServeMux)
// TODO: implement serveSPA(w http.ResponseWriter, r *http.Request)
```

`GET /_dashboard/*` endpoints specified in design docs are not registered.

---

#### API-L-9: Handshake response has extra fields not in spec

**Location**: `daemon/handshake.go:22-28`

Spec defines: `{ P_seller, nonce_s, timestamp }`
Implementation adds: `session_id`, `expires_at`

Non-breaking (extra fields), but undocumented in spec.

---

#### API-L-10: `MaxRequestSize` config field defined but never enforced

**Location**: `daemon/daemon.go:121`, default `"10MB"` at line 187

Config field exists with a default value but is never read or applied. Individual endpoints hardcode their own limits (handshake: 4KB, HTLC: 1MB).

---

## Spec Conformance Summary

| Spec Requirement | Status | Notes |
|------------------|--------|-------|
| Content negotiation (HTML/Markdown/JSON) | **Implemented** | Via Accept header |
| x402 Payment Required (HTTP 402) | **Implemented** | `SetPaymentHeaders` sets 402 correctly |
| Error response format `{"error":{...}}` | **Mostly** | 402 response uses different format (API-M-8) |
| CORS configurable origins | **Implemented** | Default `*` wildcard |
| Rate limiting (429) | **Implemented** | Per-IP token bucket |
| Paymail BSV Alias 1.0 | **Implemented** | `/.well-known/bsvalias` + `/api/v1/pki/` |
| SPV proof endpoint | **Implemented** | Optional (503 if unconfigured) |
| Method 42 handshake | **Implemented** | Extra session fields returned |
| Session authentication on subsequent requests | **Not Implemented** | Sessions created but never validated |
| WebMCP declarations in HTML | **Not Implemented** | HTML responses lack WebMCP metadata |
| Dashboard SPA | **Not Implemented** | TODO stubs only |
| DNSLink P_seller validation | **Not Implemented** | Spec requires P_seller match published P_node |
| Handshake timestamp validation | **Not Implemented** | Timestamp accepted but not checked |

---

## Summary Statistics

| Severity | Count | Category Breakdown |
|----------|-------|--------------------|
| **HIGH** | 4 | Client-server mismatch (3), JSON serialization (1) |
| **MEDIUM** | 9 | Error leakage (3), silent failures (2), API design (4) |
| **LOW** | 10 | Naming (2), missing features (3), hardening (5) |
| **Total** | **23** | |

### Priority Recommendations

1. **P0 — Fix client-server contract** (API-H-1, API-H-2, API-H-4): Align field names between daemon responses and client structs. The paid content purchase flow is broken without TxID and correct price field.
2. **P0 — Fix ChildInfo json tags** (API-H-3): Add `json:"name"` and `json:"type"` tags, or convert to `metaChildResponse` in `serveJSON()`.
3. **P1 — Suppress error details** (API-M-1, API-M-2, API-M-3, API-L-1, API-L-2): Replace `err.Error()` with generic messages in client-facing responses, add server-side logging.
4. **P1 — Fail explicitly** (API-M-4, API-M-5): Return errors instead of silently ignoring capsule/HTLC failures and invalid query params.
5. **P2 — Standardize API patterns** (API-M-6, API-M-7, API-M-8, API-M-9): Use sentinel errors, rename URL params, unify error/response formats.
6. **P3 — Hardening** (API-L-3 through API-L-10): Logging, caching headers, request tracing, enforce config values.
