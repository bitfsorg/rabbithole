# Den Explorer v2 Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Feature-complete Den Explorer with DAG visualization, full protocol decoding, and BitFS VI styling.

**Architecture:** Incremental enhancement of existing Go+htmx app. D3.js added only for DAG page. Static assets (CSS/JS) embedded via `//go:embed static`. New Go files for each feature domain (dag.go, x402.go, raw.go, history.go). All new routes registered in handlers.go.

**Tech Stack:** Go 1.25, html/template, htmx (CDN), D3.js v7 (CDN), libbitfs-go (metanet, x402, network, spv packages)

**Design doc:** `docs/plans/2026-03-02-den-explorer-v2-design.md`

---

## Phase 1: UI — BitFS VI Alignment

### Task 1: Extract CSS to static file and apply BitFS VI color palette

**Files:**
- Create: `den-explorer/static/style.css`
- Modify: `den-explorer/templates/base.html`
- Modify: `den-explorer/main.go`
- Modify: `den-explorer/templates.go`

**Step 1: Create `static/style.css` with BitFS VI variables**

Replace the entire inline `<style>` block from `base.html` with a standalone CSS file using CSS variables. Map colors:

```css
:root {
  --b-bg: #110f0d;
  --b-bg2: #1a1714;
  --b-bg3: #231f1b;
  --b-border: #2e2924;
  --b-text: #d4cdc4;
  --b-text-dim: #8a8078;
  --b-text-bright: #f0ebe5;
  --b-gold: #c9956b;
  --b-gold-light: #dbb08a;
  --b-gold-dark: #a07548;
  --b-valid: #6d9e6d;
  --b-error: #c96b6b;
}
```

All existing CSS rules are preserved but colors remapped:
- `#1a1a2e` → `var(--b-bg)`
- `#16213e` → `var(--b-bg2)`
- `#0f3460` → `var(--b-border)`
- `#e0e0e0` → `var(--b-text)`
- `#e94560` → `var(--b-gold)`
- `#64b5f6` → `var(--b-gold-light)`
- `#999` → `var(--b-text-dim)`
- `#4caf50` (valid) → `var(--b-valid)`

Font stack update:
```css
body {
  font-family: 'JetBrains Mono', 'SF Mono', 'Menlo', 'Monaco', monospace;
}
```

Tag colors adjusted for Botanical palette:
```css
.tag-file { background: #1a2e1a; color: #6d9e6d; }
.tag-dir { background: #1a2024; color: #6d8a9e; }
.tag-link { background: #241a2e; color: #9e6d9e; }
.tag-root { background: #2e241a; color: var(--b-gold); }
.tag-private { background: #2e1a1a; color: #c96b6b; }
.tag-free { background: #1a2e1a; color: #6d9e6d; }
.tag-paid { background: #2e2a1a; color: var(--b-gold-light); }
.tag-metanet { background: var(--b-gold-dark); color: var(--b-text-bright); }
.tag-anchor { background: #1a242e; color: #6d9ec9; }
```

**Step 2: Add static embed to `templates.go`**

Add after the existing `templateFS` embed (line 14):

```go
//go:embed static
var staticFS embed.FS
```

Export a function to get the static FS:

```go
// StaticFS returns the embedded static file system.
func StaticFS() http.FileSystem {
	sub, _ := fs.Sub(staticFS, "static")
	return http.FS(sub)
}
```

Import `io/fs` and `net/http`.

**Step 3: Update `base.html`**

Replace the entire `<style>...</style>` block with:

```html
<link rel="stylesheet" href="/static/style.css">
```

Add Google Fonts (JetBrains Mono) link in `<head>`:

```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;700&display=swap" rel="stylesheet">
```

**Step 4: Register static route in `main.go`**

In `Routes()` method in `handlers.go`, add before the first route:

```go
mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(staticFS)))
```

This requires passing `staticFS` to Server or using the exported `StaticFS()` function. Simplest: add a field to Server or use the package-level embed directly in handlers.go.

Actually, cleaner approach: register the static handler in `Routes()` using the package-level `staticFS` variable. Since `staticFS` is in templates.go (same package), it's directly accessible:

```go
sub, _ := fs.Sub(staticFS, "static")
mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(sub)))
```

Add import `io/fs` to handlers.go.

**Step 5: Verify**

Run: `cd den-explorer && go build .`
Expected: compiles without error.

Visual check: start the server, open browser, verify Dark Botanical colors render.

**Step 6: Commit**

```bash
git add den-explorer/static/style.css den-explorer/templates/base.html den-explorer/templates.go den-explorer/handlers.go
git commit -m "feat(den): extract CSS to static file, apply BitFS VI palette"
```

---

## Phase 2: TLV Complete Decoding

### Task 2: Extend TLV tag names and value interpretation

**Files:**
- Modify: `den-explorer/decode.go`
- Create: `den-explorer/decode_test.go` (extend existing)

**Step 1: Extend `tlvTagNames` map (decode.go:33-60)**

Add the 23 missing tags after `0x1A: "MerkleRoot"`:

```go
0x1B: "EncPayload",
0x1E: "Metadata",
0x1F: "VersionLog",
0x20: "TreeRootPNode",
0x21: "TreeRootTxID",
0x22: "ParentAnchorTxID",
0x23: "Author",
0x24: "CommitMessage",
0x25: "GitCommitSHA",
0x26: "FileMode",
0x27: "ShareList",
0x28: "ChunkIndex",
0x29: "TotalChunks",
0x2A: "RecombinationHash",
0x2B: "RabinSignature",
0x2C: "RabinPubKey",
0x2D: "RegistryTxID",
0x2E: "RegistryVout",
0x2F: "ISOConfig",
0x30: "ACLRef",
```

**Step 2: Extend `interpretTLVValue` (decode.go:144-192)**

Add cases for all new tags. Key logic per type:

```go
case 0x08: // PricePerKB
    if len(value) == 8 {
        v := binary.LittleEndian.Uint64(value)
        return fmt.Sprintf("%d sat/KB", v)
    }
case 0x09: // LinkTarget
    return hex.EncodeToString(value) + " (P_node)"
case 0x0A: // LinkType
    if len(value) == 4 {
        v := int32(binary.LittleEndian.Uint32(value))
        switch metanet.LinkType(v) {
        case metanet.LinkTypeSoft:
            return "SOFT"
        case metanet.LinkTypeSoftRemote:
            return "SOFT_REMOTE"
        }
    }
case 0x0B: // Timestamp
    if len(value) == 8 {
        v := binary.LittleEndian.Uint64(value)
        return time.Unix(int64(v), 0).Format("2006-01-02 15:04:05")
    }
case 0x0C: // Parent P_node
    return hex.EncodeToString(value) + " (P_node)"
case 0x0D: // Index
    if len(value) == 4 {
        return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
    }
case 0x0E: // ChildEntry — just show count/length
    return fmt.Sprintf("%d bytes (child entry)", len(value))
case 0x0F: // NextChildIndex
    if len(value) == 4 {
        return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
    }
case 0x13: // Encrypted
    if len(value) == 4 {
        v := binary.LittleEndian.Uint32(value)
        if v != 0 { return "Yes" }
        return "No"
    }
case 0x14: // OnChain
    if len(value) == 4 {
        v := binary.LittleEndian.Uint32(value)
        if v != 0 { return "Yes" }
        return "No"
    }
case 0x15: // ContentTxID
    return hex.EncodeToString(value) + " (TxID)"
case 0x16: // Compression
    if len(value) == 4 {
        v := binary.LittleEndian.Uint32(value)
        switch v {
        case 0: return "NONE"
        case 1: return "LZW"
        case 2: return "GZIP"
        default: return fmt.Sprintf("UNKNOWN(%d)", v)
        }
    }
case 0x17: // CltvHeight
    if len(value) == 4 {
        return fmt.Sprintf("block %d", binary.LittleEndian.Uint32(value))
    }
case 0x18: // RevenueShare
    if len(value) == 4 {
        return fmt.Sprintf("%d shares", binary.LittleEndian.Uint32(value))
    }
case 0x1B: // EncPayload
    if len(value) >= 44 { // salt(16)+nonce(12)+tag(16) minimum
        cLen := len(value) - 44
        return fmt.Sprintf("salt=%s nonce=%s ciphertext=%d bytes",
            hex.EncodeToString(value[:16]),
            hex.EncodeToString(value[16:28]),
            cLen)
    }
    return fmt.Sprintf("%d bytes (encrypted)", len(value))
case 0x1E: // Metadata
    return interpretMetadata(value)
case 0x1F: // VersionLog
    return hex.EncodeToString(value) + " (P_node)"
case 0x20: // TreeRootPNode
    return hex.EncodeToString(value) + " (P_node)"
case 0x21: // TreeRootTxID
    return hex.EncodeToString(value) + " (TxID)"
case 0x22: // ParentAnchorTxID
    return hex.EncodeToString(value) + " (TxID)"
case 0x23: // Author
    return string(value)
case 0x24: // CommitMessage
    return string(value)
case 0x25: // GitCommitSHA
    return hex.EncodeToString(value)
case 0x26: // FileMode
    if len(value) == 4 {
        return fmt.Sprintf("0%o", binary.LittleEndian.Uint32(value))
    }
case 0x27: // ShareList
    return hex.EncodeToString(value) + " (P_node)"
case 0x28: // ChunkIndex
    if len(value) == 4 {
        return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
    }
case 0x29: // TotalChunks
    if len(value) == 4 {
        return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
    }
case 0x2A: // RecombinationHash
    return hex.EncodeToString(value)
case 0x2B: // RabinSignature
    return fmt.Sprintf("%d bytes", len(value))
case 0x2C: // RabinPubKey
    return fmt.Sprintf("%d bytes", len(value))
case 0x2D: // RegistryTxID
    return hex.EncodeToString(value) + " (TxID)"
case 0x2E: // RegistryVout
    if len(value) == 4 {
        return fmt.Sprintf("%d", binary.LittleEndian.Uint32(value))
    }
case 0x2F: // ISOConfig
    return interpretISOConfig(value)
case 0x30: // ACLRef
    return fmt.Sprintf("%d bytes", len(value))
```

**Step 3: Add helper functions**

Add `interpretMetadata` and `interpretISOConfig` to decode.go:

```go
// interpretMetadata decodes sub-TLV metadata map for display.
func interpretMetadata(data []byte) string {
    var pairs []string
    offset := 0
    for offset+2 <= len(data) {
        kLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
        offset += 2
        if offset+kLen > len(data) { break }
        key := string(data[offset : offset+kLen])
        offset += kLen
        if offset+2 > len(data) { break }
        vLen := int(binary.LittleEndian.Uint16(data[offset : offset+2]))
        offset += 2
        if offset+vLen > len(data) { break }
        val := string(data[offset : offset+vLen])
        offset += vLen
        pairs = append(pairs, key+"="+val)
    }
    if len(pairs) == 0 { return fmt.Sprintf("%d bytes", len(data)) }
    return strings.Join(pairs, ", ")
}

// interpretISOConfig decodes ISO configuration bytes for display.
func interpretISOConfig(data []byte) string {
    if len(data) < 37 { return fmt.Sprintf("%d bytes", len(data)) }
    totalShares := binary.BigEndian.Uint64(data[0:8])
    pricePerShare := binary.BigEndian.Uint64(data[8:16])
    creatorAddr := hex.EncodeToString(data[16:36])
    status := data[36]
    return fmt.Sprintf("shares=%d price=%d sat creator=%s status=%d",
        totalShares, pricePerShare, creatorAddr, status)
}
```

Add imports: `encoding/binary`, `strings`, `time` (if not already present).

**Step 4: Add TLV link generation to template**

Add a template function `tlvLink` that generates `<a>` for TxID-type TLV values. In `templates.go` add to `tmplFuncs`:

```go
"tlvLink": func(tag byte, valueHex string) template.HTML {
    switch tag {
    case 0x15, 0x21, 0x22, 0x2D: // ContentTxID, TreeRootTxID, ParentAnchorTxID, RegistryTxID
        txid := strings.TrimSuffix(valueHex, " (TxID)")
        if len(txid) == 64 {
            return template.HTML(fmt.Sprintf(`<a href="/tx/%s" class="mono">%s</a>`, txid, truncHash(txid)))
        }
    }
    return template.HTML(valueHex)
},
```

Update `metanet.html` TLV table to use `tlvLink` for the Decoded column:

```html
<td>{{if .ValueStr}}{{tlvLink .Tag .ValueStr}}{{end}}</td>
```

**Step 5: Write tests for new TLV interpretation**

Add test cases to `decode_test.go` for the new tags — at minimum test interpretTLVValue for each new tag, interpretMetadata, and interpretISOConfig.

**Step 6: Verify**

Run: `cd den-explorer && go test ./... -v -count=1`
Expected: all tests pass.

Run: `cd den-explorer && go build .`
Expected: compiles.

**Step 7: Commit**

```bash
git add den-explorer/decode.go den-explorer/decode_test.go den-explorer/templates.go den-explorer/templates/metanet.html
git commit -m "feat(den): complete TLV decoding for all 49 tags"
```

---

## Phase 3: Hex Dump (Raw Data View)

### Task 3: Add raw data hex dump page

**Files:**
- Create: `den-explorer/raw.go`
- Create: `den-explorer/templates/raw.html`
- Modify: `den-explorer/handlers.go` (add routes + handlers)
- Modify: `den-explorer/templates.go` (register template)
- Modify: `den-explorer/templates/metanet.html` (add link)
- Modify: `den-explorer/templates/tx.html` (add link)

**Step 1: Create `raw.go`**

```go
package main

import (
	"fmt"
	"strings"
)

// HexDumpLine represents one line of hex dump output.
type HexDumpLine struct {
	Offset string
	Hex    string
	ASCII  string
}

// FormatHexDump formats raw bytes as classic hex dump lines (16 bytes per line).
func FormatHexDump(data []byte) []HexDumpLine {
	var lines []HexDumpLine
	for i := 0; i < len(data); i += 16 {
		end := i + 16
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]

		// Hex part
		var hexParts []string
		for j, b := range chunk {
			hexParts = append(hexParts, fmt.Sprintf("%02x", b))
			if j == 7 {
				hexParts = append(hexParts, "")
			}
		}
		// Pad if short
		for j := len(chunk); j < 16; j++ {
			hexParts = append(hexParts, "  ")
			if j == 7 {
				hexParts = append(hexParts, "")
			}
		}

		// ASCII part
		var ascii strings.Builder
		for _, b := range chunk {
			if b >= 0x20 && b <= 0x7e {
				ascii.WriteByte(b)
			} else {
				ascii.WriteByte('.')
			}
		}

		lines = append(lines, HexDumpLine{
			Offset: fmt.Sprintf("%06x", i),
			Hex:    strings.Join(hexParts, " "),
			ASCII:  ascii.String(),
		})
	}
	return lines
}

// RawData holds the raw view data for a transaction.
type RawData struct {
	TxID       string
	IsMetanet  bool
	PushData   []PushDataItem // OP_RETURN push data elements
	RawTxHex   string         // full raw tx hex
}

// PushDataItem is one push data element from OP_RETURN.
type PushDataItem struct {
	Index   int
	Label   string // e.g. "MetaFlag", "P_node", "ParentTxID", "TLV Payload"
	Hex     string
	Length  int
	Lines   []HexDumpLine
}

// BuildRawData builds the raw data view for a Metanet transaction.
func BuildRawData(txid string, rawBytes []byte) *RawData {
	rd := &RawData{TxID: txid}

	decoded := DecodeMetanetTx(rawBytes)
	if !decoded.IsMetanet {
		rd.IsMetanet = false
		return rd
	}
	rd.IsMetanet = true

	// Re-extract push data for display
	tx, err := transaction.NewTransactionFromBytes(rawBytes)
	if err != nil {
		return rd
	}
	pushes := extractOPReturnPushes(tx)

	labels := []string{"MetaFlag", "P_node", "ParentTxID", "TLV Payload"}
	for i, push := range pushes {
		label := "Data"
		if i < len(labels) {
			label = labels[i]
		}
		item := PushDataItem{
			Index:  i,
			Label:  label,
			Hex:    hex.EncodeToString(push),
			Length: len(push),
			Lines:  FormatHexDump(push),
		}
		rd.PushData = append(rd.PushData, item)
	}
	return rd
}
```

Import `"encoding/hex"` and `"github.com/bsv-blockchain/go-sdk/transaction"`.

**Step 2: Create `templates/raw.html`**

```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / Raw</div>

<div class="card">
  <h2>Raw Transaction Data</h2>
  <div class="field-row"><span class="label">TxID</span><span class="value mono">{{.TxID}}</span></div>
  <div class="field-row"><span class="label">Metanet</span><span class="value">{{if .Raw.IsMetanet}}Yes{{else}}No{{end}}</span></div>
</div>

{{if .Raw.IsMetanet}}
{{range .Raw.PushData}}
<div class="card">
  <h2>Push #{{.Index}}: {{.Label}} ({{.Length}} bytes)</h2>
  <pre class="hexdump">{{range .Lines}}
{{.Offset}}   {{.Hex}}   {{.ASCII}}{{end}}</pre>
</div>
{{end}}
{{end}}

<div class="card">
  <h2>Full Transaction Hex</h2>
  <div style="margin-bottom:8px"><a href="/raw/{{.TxID}}?format=hex">Download as text</a></div>
  <pre class="hexdump" style="max-height:400px;overflow-y:auto;font-size:11px">{{.RawHex}}</pre>
</div>
{{end}}
```

Add `.hexdump` style to `static/style.css`:

```css
.hexdump {
  font-family: var(--font-mono, 'JetBrains Mono', monospace);
  font-size: 12px;
  line-height: 1.6;
  color: var(--b-text);
  background: var(--b-bg);
  padding: 12px;
  border: 1px solid var(--b-border);
  border-radius: 4px;
  overflow-x: auto;
  white-space: pre;
}
```

**Step 3: Add handlers in `handlers.go`**

Add two handlers:

```go
func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	rawBytes, err := s.explorer.rpc.GetRawTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	// Plain hex download
	if r.URL.Query().Get("format") == "hex" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s.hex", txid[:16]))
		fmt.Fprintf(w, "%x", rawBytes)
		return
	}

	raw := BuildRawData(txid, rawBytes)

	data := map[string]interface{}{
		"Title":  fmt.Sprintf("Raw %s", truncHash(txid)),
		"TxID":   txid,
		"Raw":    raw,
		"RawHex": fmt.Sprintf("%x", rawBytes),
	}
	s.render(w, "raw.html", data)
}
```

Register in `Routes()`:

```go
mux.HandleFunc("GET /raw/{txid}", s.handleRaw)
```

Register template in `templates.go` — add `"raw.html"` to the `pages` slice (line 60).

**Step 4: Add links from existing pages**

In `templates/tx.html`, add after the Metanet links line (line 16):

```html
| <a href="/raw/{{.Tx.TxID}}">Raw Data</a>
```

In `templates/metanet.html`, add to the bottom card (line 87):

```html
<a href="/raw/{{.TxID}}">View Raw Data</a> |
```

**Step 5: Verify**

Run: `cd den-explorer && go build . && go test ./... -v -count=1`

**Step 6: Commit**

```bash
git add den-explorer/raw.go den-explorer/templates/raw.html den-explorer/static/style.css \
        den-explorer/handlers.go den-explorer/templates.go \
        den-explorer/templates/tx.html den-explorer/templates/metanet.html
git commit -m "feat(den): add hex dump raw data view"
```

---

## Phase 4: Transaction Chain History

### Task 4: Add version history tracking page

**Files:**
- Create: `den-explorer/history.go`
- Create: `den-explorer/templates/history.html`
- Modify: `den-explorer/handlers.go`
- Modify: `den-explorer/templates.go`
- Modify: `den-explorer/templates/metanet.html`

**Step 1: Create `history.go`**

```go
package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// HistoryEntry is one version in a Metanet node's history.
type HistoryEntry struct {
	Version   int
	TxID      string
	PNode     string
	Op        string
	Timestamp uint64
	IsCurrent bool
}

// BuildHistory traces a Metanet node's version chain backwards via vin[0].
// maxDepth prevents infinite loops.
func (e *Explorer) BuildHistory(ctx context.Context, txid string, maxDepth int) ([]HistoryEntry, error) {
	var entries []HistoryEntry
	currentTxID := txid
	visited := make(map[string]bool)

	for i := 0; i < maxDepth; i++ {
		if visited[currentTxID] {
			break
		}
		visited[currentTxID] = true

		rawBytes, err := e.rpc.GetRawTx(ctx, currentTxID)
		if err != nil {
			break
		}
		decoded := DecodeMetanetTx(rawBytes)
		if !decoded.IsMetanet {
			break
		}

		entry := HistoryEntry{
			TxID:      currentTxID,
			PNode:     decoded.PNode,
			IsCurrent: i == 0,
		}
		if decoded.Node != nil {
			entry.Op = decoded.Node.Op.String()
			entry.Timestamp = decoded.Node.Timestamp
		}
		entries = append(entries, entry)

		// Follow vin[0] to previous version
		sdkTx, err := transaction.NewTransactionFromBytes(rawBytes)
		if err != nil {
			break
		}
		if len(sdkTx.Inputs) == 0 || sdkTx.Inputs[0].SourceTXID == nil {
			break
		}
		prevTxID := hex.EncodeToString(sdkTx.Inputs[0].SourceTXID)
		if prevTxID == "" || prevTxID == currentTxID {
			break
		}

		// Check if prev is same P_node Metanet tx
		prevRaw, err := e.rpc.GetRawTx(ctx, prevTxID)
		if err != nil {
			break
		}
		prevDecoded := DecodeMetanetTx(prevRaw)
		if !prevDecoded.IsMetanet || prevDecoded.PNode != decoded.PNode {
			break // not a version update — we've reached the creation point
		}
		currentTxID = prevTxID
	}

	// Reverse so oldest first, then assign version numbers
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	for i := range entries {
		entries[i].Version = i + 1
	}
	return entries, nil
}
```

Note: `sdkTx.Inputs[0].SourceTXID` is a `[]byte` in go-sdk. It's the raw txid bytes (internal byte order). Need to reverse for display-order hex. Check the exact field name — may be `SourceTXID` or `PreviousTxID`. Verify against go-sdk's `transaction.TransactionInput` struct at implementation time. If it's display-order already, no reversal needed. The key idea: follow vin[0]'s prevout txid.

**Step 2: Create `templates/history.html`**

```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / <a href="/metanet/{{.TxID}}">Metanet</a> / History</div>

<div class="card">
  <h2>Version History ({{len .History}} versions)</h2>
</div>

<div class="timeline">
  {{range .History}}
  <div class="timeline-entry{{if .IsCurrent}} timeline-current{{end}}">
    <div class="timeline-marker"></div>
    <div class="timeline-content card">
      <div class="field-row">
        <span class="label">v{{.Version}}{{if .IsCurrent}} (current){{end}}</span>
        <span class="value"><span class="tag tag-metanet">{{.Op}}</span></span>
      </div>
      <div class="field-row">
        <span class="label">TxID</span>
        <span class="value"><a href="/metanet/{{.TxID}}" class="mono">{{truncHash .TxID}}</a></span>
      </div>
      {{if .Timestamp}}
      <div class="field-row">
        <span class="label">Time</span>
        <span class="value">{{formatTime (int64 .Timestamp)}}</span>
      </div>
      {{end}}
    </div>
  </div>
  {{end}}
</div>
{{end}}
```

Add timeline CSS to `static/style.css`:

```css
.timeline { position: relative; padding-left: 32px; }
.timeline::before {
  content: '';
  position: absolute;
  left: 8px;
  top: 0;
  bottom: 0;
  width: 2px;
  background: var(--b-border);
}
.timeline-entry { position: relative; margin-bottom: 16px; }
.timeline-marker {
  position: absolute;
  left: -28px;
  top: 16px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--b-gold-dark);
  border: 2px solid var(--b-border);
}
.timeline-current .timeline-marker { background: var(--b-gold); }
.timeline-current .card { border-color: var(--b-gold-dark); }
```

**Step 3: Add handler and route**

In `handlers.go`:

```go
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	history, err := s.explorer.BuildHistory(r.Context(), txid, 50)
	if err != nil {
		http.Error(w, fmt.Sprintf("history: %v", err), 500)
		return
	}

	data := map[string]interface{}{
		"Title":   fmt.Sprintf("History %s", truncHash(txid)),
		"TxID":    txid,
		"History": history,
	}
	s.render(w, "history.html", data)
}
```

Register: `mux.HandleFunc("GET /history/{txid}", s.handleHistory)`

Register template: add `"history.html"` to `pages` slice in templates.go.

Add `int64` to template funcs if not already present (for Timestamp conversion):

```go
"int64": func(v uint64) int64 { return int64(v) },
```

**Step 4: Add link from metanet.html**

In the bottom card of `metanet.html` (line 87 area):

```html
<a href="/history/{{.TxID}}">View History</a> |
```

**Step 5: Verify**

Run: `cd den-explorer && go build . && go test ./... -v -count=1`

**Step 6: Commit**

```bash
git add den-explorer/history.go den-explorer/templates/history.html \
        den-explorer/static/style.css den-explorer/handlers.go \
        den-explorer/templates.go den-explorer/templates/metanet.html
git commit -m "feat(den): add transaction chain version history"
```

---

## Phase 5: x402 HTLC Analysis

### Task 5: Add x402 payment protocol analysis page

**Files:**
- Create: `den-explorer/x402.go`
- Create: `den-explorer/templates/x402.html`
- Modify: `den-explorer/handlers.go`
- Modify: `den-explorer/templates.go`
- Modify: `den-explorer/templates/metanet.html`
- Modify: `den-explorer/templates/tx.html`

**Step 1: Create `x402.go`**

```go
package main

import (
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// HTLCAnalysis holds parsed HTLC details from a transaction output.
type HTLCAnalysis struct {
	Found       bool
	OutputIndex int
	Amount      float64 // BSV
	InvoiceID   string  // hex, empty if absent
	CapsuleHash string  // hex 32 bytes
	SellerAddr  string  // hex 20 bytes (P2PKH hash)
	BuyerPubKey string  // hex 33 bytes
	SellerPubKey string // hex 33 bytes
	Status      string  // "Pending" / "Claimed" / "Refunded"
	Error       string
}

// AnalyzeX402 scans a transaction's outputs for HTLC scripts.
func AnalyzeX402(rawBytes []byte) *HTLCAnalysis {
	result := &HTLCAnalysis{}

	sdkTx, err := transaction.NewTransactionFromBytes(rawBytes)
	if err != nil {
		result.Error = fmt.Sprintf("parse tx: %v", err)
		return result
	}

	for i, out := range sdkTx.Outputs {
		script := []byte(*out.LockingScript)
		htlc := parseHTLCScript(script)
		if htlc == nil {
			continue
		}
		result.Found = true
		result.OutputIndex = i
		result.Amount = float64(out.Satoshis) / 1e8
		result.CapsuleHash = htlc.capsuleHash
		result.SellerAddr = htlc.sellerAddr
		result.BuyerPubKey = htlc.buyerPubKey
		result.SellerPubKey = htlc.sellerPubKey
		result.InvoiceID = htlc.invoiceID
		result.Status = "Pending" // Default; would need UTXO check for accuracy
		break
	}
	return result
}

type htlcParsed struct {
	invoiceID   string
	capsuleHash string
	sellerAddr  string
	buyerPubKey string
	sellerPubKey string
}

// parseHTLCScript attempts to match the x402 HTLC pattern in a locking script.
// Pattern: [<16> OP_DROP] OP_IF OP_SHA256 <32> OP_EQUALVERIFY OP_DUP OP_HASH160 <20> OP_EQUALVERIFY OP_CHECKSIG OP_ELSE OP_2 <33> <33> OP_2 OP_CHECKMULTISIG OP_ENDIF
func parseHTLCScript(script []byte) *htlcParsed {
	if len(script) < 100 { // minimum HTLC script length
		return nil
	}

	pos := 0
	result := &htlcParsed{}

	// Optional: InvoiceID prefix (<16 bytes> OP_DROP)
	if pos < len(script) && script[pos] == 0x10 { // push 16 bytes
		pos++
		if pos+16 >= len(script) { return nil }
		result.invoiceID = hex.EncodeToString(script[pos : pos+16])
		pos += 16
		if pos >= len(script) || script[pos] != 0x75 { return nil } // OP_DROP
		pos++
	}

	// OP_IF (0x63)
	if pos >= len(script) || script[pos] != 0x63 { return nil }
	pos++

	// OP_SHA256 (0xa8)
	if pos >= len(script) || script[pos] != 0xa8 { return nil }
	pos++

	// Push 32 bytes (capsule hash)
	if pos >= len(script) || script[pos] != 0x20 { return nil }
	pos++
	if pos+32 > len(script) { return nil }
	result.capsuleHash = hex.EncodeToString(script[pos : pos+32])
	pos += 32

	// OP_EQUALVERIFY (0x88)
	if pos >= len(script) || script[pos] != 0x88 { return nil }
	pos++

	// OP_DUP (0x76) OP_HASH160 (0xa9)
	if pos+1 >= len(script) || script[pos] != 0x76 || script[pos+1] != 0xa9 { return nil }
	pos += 2

	// Push 20 bytes (seller address hash)
	if pos >= len(script) || script[pos] != 0x14 { return nil }
	pos++
	if pos+20 > len(script) { return nil }
	result.sellerAddr = hex.EncodeToString(script[pos : pos+20])
	pos += 20

	// OP_EQUALVERIFY (0x88) OP_CHECKSIG (0xac) OP_ELSE (0x67)
	if pos+2 >= len(script) || script[pos] != 0x88 || script[pos+1] != 0xac || script[pos+2] != 0x67 {
		return nil
	}
	pos += 3

	// OP_2 (0x52)
	if pos >= len(script) || script[pos] != 0x52 { return nil }
	pos++

	// Push 33 bytes (buyer pubkey)
	if pos >= len(script) || script[pos] != 0x21 { return nil }
	pos++
	if pos+33 > len(script) { return nil }
	result.buyerPubKey = hex.EncodeToString(script[pos : pos+33])
	pos += 33

	// Push 33 bytes (seller pubkey)
	if pos >= len(script) || script[pos] != 0x21 { return nil }
	pos++
	if pos+33 > len(script) { return nil }
	result.sellerPubKey = hex.EncodeToString(script[pos : pos+33])
	pos += 33

	// OP_2 (0x52) OP_CHECKMULTISIG (0xae) OP_ENDIF (0x68)
	if pos+2 >= len(script) || script[pos] != 0x52 || script[pos+1] != 0xae || script[pos+2] != 0x68 {
		return nil
	}

	return result
}
```

**Step 2: Create `templates/x402.html`**

```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / x402 Payment</div>

<div class="card">
  <h2>x402 HTLC Payment Analysis</h2>
  <div class="field-row"><span class="label">TxID</span><span class="value mono">{{.TxID}}</span></div>
  {{if not .X402.Found}}
  <div class="field-row"><span class="label">Status</span><span class="value" style="color:var(--b-text-dim)">No HTLC output found in this transaction</span></div>
  {{end}}
</div>

{{if .X402.Found}}
<div class="card">
  <h2>HTLC Details</h2>
  <div class="field-row"><span class="label">Output</span><span class="value">#{{.X402.OutputIndex}}</span></div>
  <div class="field-row"><span class="label">Amount</span><span class="value">{{printf "%.8f" .X402.Amount}} BSV</span></div>
  <div class="field-row"><span class="label">Status</span><span class="value"><span class="tag tag-paid">{{.X402.Status}}</span></span></div>
  {{if .X402.InvoiceID}}
  <div class="field-row"><span class="label">Invoice ID</span><span class="value mono" style="font-size:12px">{{.X402.InvoiceID}}</span></div>
  {{end}}
  <div class="field-row"><span class="label">Capsule Hash</span><span class="value mono" style="font-size:11px">{{.X402.CapsuleHash}}</span></div>
  <div class="field-row"><span class="label">Seller Addr</span><span class="value mono" style="font-size:12px">{{.X402.SellerAddr}}</span></div>
  <div class="field-row"><span class="label">Buyer PubKey</span><span class="value mono" style="font-size:11px">{{.X402.BuyerPubKey}}</span></div>
  <div class="field-row"><span class="label">Seller PubKey</span><span class="value mono" style="font-size:11px">{{.X402.SellerPubKey}}</span></div>
</div>

<div class="card">
  <h2>HTLC Script Structure</h2>
  <pre class="hexdump" style="line-height:1.8">
{{if .X402.InvoiceID}}&lt;invoice_id&gt; OP_DROP      // Replay protection
{{end}}OP_IF
  OP_SHA256 &lt;capsule_hash&gt; OP_EQUALVERIFY
  OP_DUP OP_HASH160 &lt;seller_addr&gt; OP_EQUALVERIFY OP_CHECKSIG
OP_ELSE
  OP_2 &lt;buyer_pubkey&gt; &lt;seller_pubkey&gt; OP_2 OP_CHECKMULTISIG
OP_ENDIF

Claim path: Seller reveals capsule (preimage of capsule_hash)
Refund path: 2-of-2 multisig (buyer + seller cooperative refund)
  </pre>
</div>
{{end}}

{{if .X402.Error}}
<div class="card"><div class="error">{{.X402.Error}}</div></div>
{{end}}
{{end}}
```

**Step 3: Add handler and route**

```go
func (s *Server) handleX402(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	rawBytes, err := s.explorer.rpc.GetRawTx(r.Context(), txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	analysis := AnalyzeX402(rawBytes)
	data := map[string]interface{}{
		"Title": fmt.Sprintf("x402 %s", truncHash(txid)),
		"TxID":  txid,
		"X402":  analysis,
	}
	s.render(w, "x402.html", data)
}
```

Register: `mux.HandleFunc("GET /x402/{txid}", s.handleX402)`

Register template: add `"x402.html"` to pages slice.

**Step 4: Add links from existing pages**

In `metanet.html` bottom card, add: `<a href="/x402/{{.TxID}}">View x402 Payment</a> |`

In `tx.html`, after the Metanet link (line 16): `| <a href="/x402/{{.Tx.TxID}}">x402 Payment</a>`

**Step 5: Verify**

Run: `cd den-explorer && go build . && go test ./... -v -count=1`

**Step 6: Commit**

```bash
git add den-explorer/x402.go den-explorer/templates/x402.html \
        den-explorer/handlers.go den-explorer/templates.go \
        den-explorer/templates/metanet.html den-explorer/templates/tx.html
git commit -m "feat(den): add x402 HTLC payment analysis page"
```

---

## Phase 6: DAG Visualization

### Task 6: Create DAG JSON API and tree builder

**Files:**
- Create: `den-explorer/dag.go`
- Modify: `den-explorer/handlers.go`

**Step 1: Create `dag.go`**

```go
package main

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bitfsorg/libbitfs-go/metanet"
)

// DAGNode is one node in the DAG JSON response.
type DAGNode struct {
	TxID     string   `json:"txid"`
	PNode    string   `json:"pnode"`
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Access   string   `json:"access"`
	Parent   string   `json:"parent,omitempty"`
	Children []string `json:"children,omitempty"`
}

// DAGResponse is the JSON structure returned by /api/dag/:txid.
type DAGResponse struct {
	Root  string    `json:"root"`
	Nodes []DAGNode `json:"nodes"`
	Error string    `json:"error,omitempty"`
}

// BuildDAG constructs the Metanet DAG from a root transaction.
// It recursively resolves child nodes by scanning for transactions
// whose P_node matches a child entry's PubKey.
func (e *Explorer) BuildDAG(ctx context.Context, rootTxID string, maxDepth int) *DAGResponse {
	resp := &DAGResponse{Root: rootTxID}
	visited := make(map[string]bool)
	e.buildDAGRecursive(ctx, rootTxID, "", maxDepth, 0, visited, resp)
	return resp
}

func (e *Explorer) buildDAGRecursive(ctx context.Context, txid, parentTxID string, maxDepth, depth int, visited map[string]bool, resp *DAGResponse) {
	if depth > maxDepth || visited[txid] {
		return
	}
	visited[txid] = true

	rawBytes, err := e.rpc.GetRawTx(ctx, txid)
	if err != nil {
		return
	}
	decoded := DecodeMetanetTx(rawBytes)
	if !decoded.IsMetanet || decoded.Node == nil {
		return
	}

	node := DAGNode{
		TxID:   txid,
		PNode:  decoded.PNode,
		Type:   decoded.Node.Type.String(),
		Access: accessStr(decoded.Node.Access),
		Parent: parentTxID,
	}

	// Resolve children
	for _, child := range decoded.Node.Children {
		childPNode := hex.EncodeToString(child.PubKey)
		childTxID := e.findTxByPNode(ctx, childPNode)
		if childTxID != "" {
			node.Children = append(node.Children, childTxID)
			// Use child name
			e.buildDAGRecursive(ctx, childTxID, txid, maxDepth, depth+1, visited, resp)
		}
	}

	// Try to find name from parent's child entries
	if parentTxID != "" {
		node.Name = e.findChildName(ctx, parentTxID, decoded.PNode)
	}
	if node.Name == "" && decoded.Node.IsDir() && decoded.IsRoot {
		node.Name = "/" // root directory
	}

	resp.Nodes = append(resp.Nodes, node)
}

// findTxByPNode searches recent blocks for a transaction with a given P_node.
// This is a brute-force approach suitable for regtest with small data.
func (e *Explorer) findTxByPNode(ctx context.Context, targetPNode string) string {
	// Get recent blocks and scan transactions
	blocks, err := e.GetRecentBlocks(ctx, 50)
	if err != nil {
		return ""
	}
	for _, block := range blocks {
		for _, txid := range block.Tx {
			rawBytes, err := e.rpc.GetRawTx(ctx, txid)
			if err != nil {
				continue
			}
			decoded := DecodeMetanetTx(rawBytes)
			if decoded.IsMetanet && decoded.PNode == targetPNode {
				return txid
			}
		}
	}
	return ""
}

// findChildName looks up a child's name from its parent's ChildEntry list.
func (e *Explorer) findChildName(ctx context.Context, parentTxID, childPNode string) string {
	rawBytes, err := e.rpc.GetRawTx(ctx, parentTxID)
	if err != nil {
		return ""
	}
	decoded := DecodeMetanetTx(rawBytes)
	if decoded.Node == nil {
		return ""
	}
	childPNodeBytes, _ := hex.DecodeString(childPNode)
	for _, child := range decoded.Node.Children {
		if bytesEqual(child.PubKey, childPNodeBytes) {
			return child.Name
		}
	}
	return ""
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) { return false }
	for i := range a {
		if a[i] != b[i] { return false }
	}
	return true
}

func accessStr(a metanet.AccessLevel) string {
	switch a {
	case metanet.AccessPrivate: return "PRIVATE"
	case metanet.AccessFree: return "FREE"
	case metanet.AccessPaid: return "PAID"
	default: return "UNKNOWN"
	}
}
```

Note: `findTxByPNode` is brute-force — scans recent 50 blocks. Fine for regtest. In production you'd need an index, but that's out of scope.

**Step 2: Add JSON API handler**

In `handlers.go`, add:

```go
import "encoding/json"

func (s *Server) handleDAGAPI(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	dag := s.explorer.BuildDAG(r.Context(), txid, 10)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(dag)
}
```

Register: `mux.HandleFunc("GET /api/dag/{txid}", s.handleDAGAPI)`

**Step 3: Verify**

Run: `cd den-explorer && go build .`

**Step 4: Commit**

```bash
git add den-explorer/dag.go den-explorer/handlers.go
git commit -m "feat(den): add DAG builder and JSON API endpoint"
```

---

### Task 7: Create DAG visualization frontend (D3.js)

**Files:**
- Create: `den-explorer/static/dag.js`
- Create: `den-explorer/templates/dag.html`
- Modify: `den-explorer/handlers.go`
- Modify: `den-explorer/templates.go`
- Modify: `den-explorer/templates/metanet.html`

**Step 1: Create `templates/dag.html`**

```html
{{define "content"}}
<div class="breadcrumb"><a href="/">Home</a> / <a href="/tx/{{.TxID}}">Tx</a> / <a href="/metanet/{{.TxID}}">Metanet</a> / DAG</div>

<div class="card">
  <h2>Metanet DAG Visualization</h2>
  <div class="field-row"><span class="label">Root TxID</span><span class="value mono">{{.TxID}}</span></div>
</div>

<div class="card" style="padding:0;overflow:hidden">
  <div id="dag-container" style="width:100%;height:600px;position:relative">
    <div id="dag-loading" style="text-align:center;padding:40px;color:var(--b-text-dim)">Loading DAG...</div>
  </div>
</div>

<script src="https://d3js.org/d3.v7.min.js"></script>
<script src="/static/dag.js"></script>
<script>renderDAG("{{.TxID}}");</script>
{{end}}
```

**Step 2: Create `static/dag.js`**

```javascript
// DAG visualization using D3.js tree layout
function renderDAG(rootTxID) {
  const container = document.getElementById('dag-container');
  const loading = document.getElementById('dag-loading');
  const width = container.clientWidth;
  const height = container.clientHeight;

  fetch('/api/dag/' + rootTxID)
    .then(r => r.json())
    .then(data => {
      loading.remove();
      if (!data.nodes || data.nodes.length === 0) {
        container.innerHTML = '<div style="text-align:center;padding:40px;color:var(--b-text-dim)">No DAG data found</div>';
        return;
      }
      drawTree(container, data, width, height);
    })
    .catch(err => {
      loading.textContent = 'Error loading DAG: ' + err.message;
    });
}

function drawTree(container, data, width, height) {
  // Build hierarchy from flat node list
  const nodeMap = {};
  data.nodes.forEach(n => { nodeMap[n.txid] = n; });

  const root = nodeMap[data.root];
  if (!root) return;

  function buildHierarchy(node) {
    const result = {
      name: node.name || truncHash(node.txid),
      data: node,
      children: []
    };
    if (node.children) {
      node.children.forEach(childTxID => {
        const child = nodeMap[childTxID];
        if (child) {
          result.children.push(buildHierarchy(child));
        }
      });
    }
    return result;
  }

  const hierarchy = d3.hierarchy(buildHierarchy(root));
  const treeLayout = d3.tree().size([width - 120, height - 120]);
  treeLayout(hierarchy);

  const svg = d3.select(container)
    .append('svg')
    .attr('width', width)
    .attr('height', height);

  const g = svg.append('g')
    .attr('transform', 'translate(60, 40)');

  // Zoom + pan
  const zoom = d3.zoom()
    .scaleExtent([0.3, 3])
    .on('zoom', (event) => g.attr('transform', event.transform));
  svg.call(zoom);
  svg.call(zoom.transform, d3.zoomIdentity.translate(60, 40));

  // Colors by type
  const typeColors = {
    'DIR': '#6d8a9e',
    'FILE': '#6d9e6d',
    'LINK': '#9e6d9e',
    'ANCHOR': '#6d9ec9'
  };

  const accessColors = {
    'PRIVATE': '#c96b6b',
    'FREE': '#6d9e6d',
    'PAID': '#c9956b'
  };

  // Links
  g.selectAll('.link')
    .data(hierarchy.links())
    .enter()
    .append('path')
    .attr('class', 'link')
    .attr('fill', 'none')
    .attr('stroke', '#2e2924')
    .attr('stroke-width', 2)
    .attr('d', d3.linkVertical()
      .x(d => d.x)
      .y(d => d.y));

  // Nodes
  const node = g.selectAll('.node')
    .data(hierarchy.descendants())
    .enter()
    .append('g')
    .attr('class', 'node')
    .attr('transform', d => `translate(${d.x},${d.y})`)
    .style('cursor', 'pointer')
    .on('click', (event, d) => {
      window.location.href = '/metanet/' + d.data.data.txid;
    });

  // Node rectangles
  node.append('rect')
    .attr('x', -60)
    .attr('y', -16)
    .attr('width', 120)
    .attr('height', 32)
    .attr('rx', 6)
    .attr('fill', d => {
      const c = typeColors[d.data.data.type] || '#8a8078';
      return c + '33'; // with alpha
    })
    .attr('stroke', d => typeColors[d.data.data.type] || '#8a8078')
    .attr('stroke-width', 1.5);

  // Type icon (text)
  node.append('text')
    .attr('x', -52)
    .attr('y', 4)
    .attr('font-size', '10px')
    .attr('fill', d => typeColors[d.data.data.type] || '#8a8078')
    .text(d => {
      switch(d.data.data.type) {
        case 'DIR': return '\u{1F4C1}';
        case 'FILE': return '\u{1F4C4}';
        case 'LINK': return '\u{1F517}';
        default: return '\u{2B21}';
      }
    });

  // Node label
  node.append('text')
    .attr('x', -38)
    .attr('y', 4)
    .attr('font-size', '11px')
    .attr('font-family', "'JetBrains Mono', monospace")
    .attr('fill', '#d4cdc4')
    .text(d => {
      const name = d.data.name;
      return name.length > 12 ? name.slice(0, 12) + '...' : name;
    });

  // Access dot
  node.append('circle')
    .attr('cx', 50)
    .attr('cy', 0)
    .attr('r', 4)
    .attr('fill', d => accessColors[d.data.data.access] || '#8a8078');

  // Tooltip
  const tooltip = d3.select(container)
    .append('div')
    .style('position', 'absolute')
    .style('background', '#1a1714')
    .style('border', '1px solid #2e2924')
    .style('border-radius', '4px')
    .style('padding', '8px 12px')
    .style('font-size', '11px')
    .style('font-family', "'JetBrains Mono', monospace")
    .style('color', '#d4cdc4')
    .style('pointer-events', 'none')
    .style('display', 'none')
    .style('z-index', '10');

  node.on('mouseover', (event, d) => {
    const nd = d.data.data;
    tooltip.html(
      `<strong>${d.data.name}</strong><br>` +
      `Type: ${nd.type}<br>` +
      `Access: ${nd.access}<br>` +
      `TxID: ${nd.txid}<br>` +
      `P_node: ${truncHash(nd.pnode)}`
    ).style('display', 'block');
  })
  .on('mousemove', (event) => {
    tooltip
      .style('left', (event.offsetX + 12) + 'px')
      .style('top', (event.offsetY - 12) + 'px');
  })
  .on('mouseout', () => {
    tooltip.style('display', 'none');
  });
}

function truncHash(h) {
  if (!h || h.length <= 16) return h || '';
  return h.slice(0, 8) + '...' + h.slice(-8);
}
```

**Step 3: Add DAG page handler**

In `handlers.go`:

```go
func (s *Server) handleDAG(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	data := map[string]interface{}{
		"Title": fmt.Sprintf("DAG %s", truncHash(txid)),
		"TxID":  txid,
	}
	s.render(w, "dag.html", data)
}
```

Register: `mux.HandleFunc("GET /dag/{txid}", s.handleDAG)`

Register template: add `"dag.html"` to pages slice.

**Step 4: Add link from metanet.html**

In the bottom card of `metanet.html`, add:

```html
<a href="/dag/{{.TxID}}">View DAG</a> |
```

**Step 5: Verify**

Run: `cd den-explorer && go build .`
Start server and test with seed data — navigate to `/dag/<root_txid>`.

**Step 6: Commit**

```bash
git add den-explorer/static/dag.js den-explorer/templates/dag.html \
        den-explorer/handlers.go den-explorer/templates.go \
        den-explorer/templates/metanet.html
git commit -m "feat(den): add D3.js DAG visualization page"
```

---

## Phase 7: Final Integration

### Task 8: Update block.html template for consistent styling

**Files:**
- Modify: `den-explorer/templates/block.html`

The block template should already work with the new CSS variables from Task 1. Review and verify it renders correctly. No code changes expected unless color-specific inline styles need updating (e.g. `style="color:#e94560"` → remove, let CSS handle it).

**Step 1: Review all templates for inline style remnants**

Check each template for hardcoded colors that should use CSS variables instead. Specifically:
- `spv.html:14` — `color:#4caf50` and `color:#e94560` → use `.valid` / `.error` classes
- `method42.html:32` — `color:#a5d6a7` → use `var(--b-valid)` or a new `.formula` class

**Step 2: Fix inline styles**

Add to `static/style.css`:

```css
.valid { color: var(--b-valid); font-weight: bold; }
.invalid { color: var(--b-error); font-weight: bold; }
.formula {
  color: var(--b-valid);
  font-size: 12px;
  line-height: 1.8;
  padding: 8px 0;
}
```

Update templates to use these classes.

**Step 3: Verify all pages**

Run: `cd den-explorer && go build . && go test ./... -v -count=1`
Start server and visually check: Home, Block, Tx, Address, Metanet, SPV, Method42, DAG, x402, Raw, History.

**Step 4: Commit**

```bash
git add den-explorer/templates/ den-explorer/static/style.css
git commit -m "feat(den): clean up inline styles, use CSS classes consistently"
```

---

### Task 9: Add tests for new functionality

**Files:**
- Modify: `den-explorer/decode_test.go`
- Create: `den-explorer/raw_test.go`
- Create: `den-explorer/x402_test.go`

**Step 1: Test FormatHexDump**

In `raw_test.go`:

```go
func TestFormatHexDump(t *testing.T) {
	data := []byte("Hello, World!!! Extra bytes here")
	lines := FormatHexDump(data)
	assert.Equal(t, 2, len(lines))
	assert.Equal(t, "000000", lines[0].Offset)
	assert.Contains(t, lines[0].ASCII, "Hello")
	assert.Equal(t, "000010", lines[1].Offset)
}

func TestFormatHexDump_Empty(t *testing.T) {
	lines := FormatHexDump(nil)
	assert.Empty(t, lines)
}
```

**Step 2: Test interpretMetadata**

In `decode_test.go`:

```go
func TestInterpretMetadata(t *testing.T) {
	// Encode "key"="val"
	var buf []byte
	k := []byte("key")
	v := []byte("val")
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(k)))
	buf = append(buf, k...)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(v)))
	buf = append(buf, v...)
	result := interpretMetadata(buf)
	assert.Contains(t, result, "key=val")
}
```

**Step 3: Test interpretISOConfig**

```go
func TestInterpretISOConfig(t *testing.T) {
	data := make([]byte, 37)
	binary.BigEndian.PutUint64(data[0:8], 1000)   // totalShares
	binary.BigEndian.PutUint64(data[8:16], 500)    // pricePerShare
	// creatorAddr is 20 zero bytes
	data[36] = 1 // status
	result := interpretISOConfig(data)
	assert.Contains(t, result, "shares=1000")
	assert.Contains(t, result, "price=500")
}
```

**Step 4: Test parseHTLCScript**

In `x402_test.go`, build a minimal HTLC script and verify parsing:

```go
func TestParseHTLCScript_Basic(t *testing.T) {
	// Build a minimal HTLC script without InvoiceID
	var script []byte
	script = append(script, 0x63) // OP_IF
	script = append(script, 0xa8) // OP_SHA256
	script = append(script, 0x20) // push 32
	capsule := bytes.Repeat([]byte{0xab}, 32)
	script = append(script, capsule...)
	script = append(script, 0x88) // OP_EQUALVERIFY
	script = append(script, 0x76) // OP_DUP
	script = append(script, 0xa9) // OP_HASH160
	script = append(script, 0x14) // push 20
	addr := bytes.Repeat([]byte{0xcd}, 20)
	script = append(script, addr...)
	script = append(script, 0x88, 0xac, 0x67) // OP_EQUALVERIFY OP_CHECKSIG OP_ELSE
	script = append(script, 0x52) // OP_2
	script = append(script, 0x21) // push 33
	buyer := bytes.Repeat([]byte{0x02}, 33)
	script = append(script, buyer...)
	script = append(script, 0x21) // push 33
	seller := bytes.Repeat([]byte{0x03}, 33)
	script = append(script, seller...)
	script = append(script, 0x52, 0xae, 0x68) // OP_2 OP_CHECKMULTISIG OP_ENDIF

	result := parseHTLCScript(script)
	assert.NotNil(t, result)
	assert.Equal(t, hex.EncodeToString(capsule), result.capsuleHash)
	assert.Equal(t, hex.EncodeToString(addr), result.sellerAddr)
	assert.Empty(t, result.invoiceID)
}
```

**Step 5: Run all tests**

Run: `cd den-explorer && go test ./... -v -count=1`
Expected: all pass.

**Step 6: Commit**

```bash
git add den-explorer/raw_test.go den-explorer/x402_test.go den-explorer/decode_test.go
git commit -m "test(den): add tests for hex dump, TLV decode, HTLC parsing"
```

---

### Task 10: Update seed tool and verify end-to-end

**Files:**
- Possibly modify: `den-explorer/cmd/seed/main.go`

**Step 1: Verify seed tool still works**

Run: `cd den-explorer && go build ./cmd/seed/ && echo "OK"`

If it compiles, the API hasn't broken.

**Step 2: End-to-end verification**

If Docker Desktop is running:

```bash
cd bitfs/e2e && docker compose up -d && cd ../../den-explorer
go build . && go build ./cmd/seed/
./seed  # generates test data
./den   # starts explorer
```

Open browser and navigate through all pages:
1. Home — check BitFS VI colors
2. Block — click through
3. Tx — verify Metanet detection
4. Metanet — check all 49 TLV tags display, child entries
5. DAG — D3.js tree renders from root
6. x402 — HTLC analysis (if HTLC tx exists in seed data)
7. Raw — hex dump
8. History — version timeline
9. SPV — proof verification
10. Method42 — encryption analysis

**Step 3: Final commit**

If any fixes are needed, commit them.

```bash
git add -A && git commit -m "fix(den): end-to-end verification fixes"
```

---

## Summary

| Task | Phase | Description | Est. Files |
|------|-------|-------------|-----------|
| 1 | UI | CSS extraction + BitFS VI palette | 4 |
| 2 | TLV | Complete 49-tag TLV decoding | 3 |
| 3 | Raw | Hex dump page | 6 |
| 4 | History | Version chain tracking | 6 |
| 5 | x402 | HTLC payment analysis | 6 |
| 6 | DAG | DAG builder + JSON API | 2 |
| 7 | DAG | D3.js visualization frontend | 5 |
| 8 | Polish | Clean up inline styles | 3 |
| 9 | Tests | Unit tests for new code | 3 |
| 10 | E2E | End-to-end verification | 0-1 |
