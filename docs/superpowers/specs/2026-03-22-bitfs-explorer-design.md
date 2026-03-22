# BitFS Explorer — Chrome Extension Design

**Date**: 2026-03-22
**Status**: Approved

## Goal

Developer debug tool Chrome extension that automatically decodes BitFS/Metanet OP_RETURN data on WhatsOnChain transaction pages. Injects a styled panel showing decoded metadata (node type, file name, access mode, DAG relationships) next to the raw hex output.

## Non-Goals

- No wallet functionality
- No popup UI or background service worker
- No file content fetching/decryption
- No multi-output batch parsing (one panel per OP_RETURN output)
- Not part of bitfs-extension (separate lightweight extension)

## Architecture

Content-script-only Chrome MV3 extension. No popup, no background worker, no storage permissions.

### Manifest

```json
{
  "manifest_version": 3,
  "name": "BitFS Explorer",
  "version": "0.1.0",
  "description": "Decode BitFS/Metanet transactions on WhatsOnChain",
  "content_scripts": [{
    "matches": [
      "https://whatsonchain.com/tx/*",
      "https://test.whatsonchain.com/tx/*"
    ],
    "js": ["content.js"],
    "css": ["style.css"],
    "run_at": "document_idle"
  }],
  "icons": {
    "16": "icons/icon-16.png",
    "48": "icons/icon-48.png",
    "128": "icons/icon-128.png"
  }
}
```

Minimal permissions: only runs on WoC tx pages. No `<all_urls>`, no `storage`, no `activeTab`.

### Content Script Flow

1. **Scan**: On page load (`document_idle`), find all OP_RETURN script elements in the WoC tx output table.
2. **Detect**: For each OP_RETURN, extract the hex push data. Check if the first push is MetaFlag (`0x6d657461`).
3. **Parse**: If Metanet, convert hex pushes to `Uint8Array[]` and call `parseNodeFromPushes(pushes)` from `@bitfs/libbitfs/metanet`. This function takes the 4-item push array (MetaFlag, P_node, ParentTxID, Payload) and returns a decoded `Node` object.
4. **Render**: Build a decoded panel DOM element and inject it below the raw hex output.
5. **Observe**: Use MutationObserver to handle WoC's SPA navigation (page content may change without full reload).

### DOM Scraping Strategy

WoC tx pages render OP_RETURN outputs in a table/card layout. The content script must:

1. Locate the outputs section (look for "Outputs" heading or output cards).
2. For each output, check if it contains "OP_RETURN" text.
3. Extract the raw hex from the script display element.
4. Parse the hex into push data items (split by OP_RETURN prefix, handle OP_PUSHDATA opcodes).

Since WoC's DOM structure may change, the scraping logic should be isolated in `scraper.ts` for easy updates. If the expected DOM structure isn't found, fail silently (no errors, no broken pages).

The scraper should look for output script hex in WoC's output cards. WoC typically renders each output as a card with the script hex displayed. The implementation should inspect the live DOM at development time and adapt selectors accordingly. WoC may display scripts as raw hex or ASM notation — handle both.

### Hex Parsing

OP_RETURN scripts on WoC are displayed as hex strings. The decoder must:

1. Strip the leading `OP_FALSE OP_RETURN` (`00 6a`) prefix. Metanet uses the 2-byte `OP_FALSE OP_RETURN` format, not bare `OP_RETURN`.
2. Parse push data items following Bitcoin script push conventions:
   - `01`-`4b`: next N bytes are data
   - `4c` (OP_PUSHDATA1): next 1 byte is length, then data
   - `4d` (OP_PUSHDATA2): next 2 bytes (LE) is length, then data
3. Return an array of `Uint8Array` push items.
4. Validate: first push must be `6d657461` (MetaFlag "meta").

This parsing happens in `decoder.ts`, independent of DOM scraping.

## Decoded Panel

### Fields Displayed

| Field | Source | Format |
|-------|--------|--------|
| Node type | `node.type` (NodeType enum) | Badge: "FILE" / "DIR" / "LINK" / "ANCHOR" |
| Operation | `node.op` (OpType enum) | Badge: "CREATE" / "UPDATE" / "DELETE" |
| MIME type | `node.mimeType` | `text/plain`, `image/png`, etc. |
| File size | `node.fileSize` (bigint) | Human-readable: "1.2 KB", "3.4 MB" — convert from bigint |
| Access mode | `node.access` + `node.pricePerKB` | "FREE" / "PRIVATE" / "PAID (100 sat/KB)" |
| P_node | Compressed pubkey | Truncated `02abc...def` with copy button |
| Parent TxID | From push data | Clickable link to WoC tx page |
| Timestamp | `node.timestamp` (bigint, Unix seconds) | ISO 8601 formatted — convert from bigint |
| Children | `node.children[]` | List of ChildEntry (name, type, pubkey) for dirs |

### Layout

```
┌─────────────────────────────────────────────────┐
│ ◆ BitFS Metanet Node                        [▾] │
├─────────────────────────────────────────────────┤
│ FILE CREATE        text/plain  57 B    FREE     │
│                                                 │
│ P_node:     02d1c8...ccbb  [copy]               │
│ Parent TX:  0ed6ce...43f2  (link)               │
│ Timestamp:  2026-03-22T01:40:03Z                │
│                                                 │
│ Children:                                       │
│   📄 smoke-test.txt  024ce5...9dd8              │
│   📁 docs/           03e4ad...ec52              │
└─────────────────────────────────────────────────┘
```

- Header with BitFS gold diamond (`#c9956b`) and collapse toggle
- First row: type badge, operation badge, MIME, size, access — all inline
- Detail rows below for pubkeys, links, timestamps
- Children section only shown for directory nodes
- Collapsed state shows only the header line

### Styling

- Dark background `#1a1a2e` to match WoC's dark theme
- BitFS gold `#c9956b` for header accent and badges
- Monospace `JetBrains Mono` / fallback `monospace` for hex values
- Regular sans-serif for labels
- Border radius and padding consistent with WoC cards
- All styles scoped via a unique class prefix (`bfx-`) to avoid conflicts

## Project Structure

```
bitfs-explorer/
├── manifest.json
├── vite.config.ts
├── tsconfig.json
├── package.json
├── src/
│   ├── content.ts          ← Entry: DOM scan, MutationObserver, injection
│   ├── decoder.ts          ← Hex parsing, MetaFlag detection, libbitfs-ts wrapper
│   ├── ui.ts               ← Panel DOM builder, collapse toggle, copy button
│   ├── scraper.ts          ← WoC DOM scraping (isolated for maintainability)
│   └── style.css           ← Panel styles (bfx- prefixed)
├── icons/
│   ├── icon-16.png
│   ├── icon-48.png
│   └── icon-128.png
└── test/
    ├── decoder.test.ts     ← Unit tests for hex parsing + MetaFlag detection
    └── ui.test.ts          ← Unit tests for panel DOM generation
```

### Dependencies

- `@bitfs/libbitfs-ts` — metanet parser (`parseNodeFromPushes`, `parsePayload` from `metanet/`). Only the metanet parser subset is used; `@bsv/sdk` is NOT required at runtime and will be tree-shaken by Vite.
- `vite` — build tool (bundle content script + CSS into single files)
- `vitest` — test runner
- `vite-plugin-static-copy` — copy manifest.json and icons to dist

No React, no Tailwind, no framework. Vanilla TS + DOM API.

### Vite Configuration

Key requirements for content-script builds:
- **Output format**: IIFE (not ESM) — content scripts cannot use `import`/`export`
- **CSS**: Emit as separate file (loaded via manifest `css` array, not injected via JS)
- **Tree-shaking**: Exclude unused libbitfs-ts modules (wallet, method42, spv, etc.)
- **Static assets**: Copy `manifest.json` and `icons/` to `dist/`

### Build Output

Vite bundles to:
```
dist/
├── content.js    ← single IIFE-bundled content script
├── style.css     ← single CSS file
├── manifest.json ← copied from root
└── icons/        ← copied from root
```

Load via `chrome://extensions` → "Load unpacked" → select `dist/`.

## Error Handling

- **DOM structure changed**: If WoC updates their page layout, the scraper fails silently. No errors shown to user. Extension simply doesn't render panels.
- **Parse failure**: If hex data looks like Metanet (has MetaFlag) but fails to parse, show a minimal error panel: "BitFS: decode error" with the raw hex preserved.
- **SPA navigation**: MutationObserver watches for DOM changes. On navigation to a new tx page, re-scan and inject.

## Testing

### Unit Tests (vitest)

- `decoder.test.ts`:
  - Parse valid Metanet OP_RETURN hex → correct Node fields
  - Reject non-Metanet OP_RETURN (no MetaFlag)
  - Handle OP_PUSHDATA1 and OP_PUSHDATA2 correctly
  - Handle malformed hex gracefully (no throw)
- `ui.test.ts`:
  - Panel renders all fields for a file node
  - Panel renders children list for a directory node
  - Collapse toggle works
  - Copy button copies pubkey to clipboard (use `navigator.clipboard.writeText()` with fallback to `document.execCommand('copy')` + temporary textarea for broader compatibility)

### Manual Testing

- Load extension in Chrome
- Navigate to a known BitFS tx on WoC mainnet
- Verify decoded panel appears with correct metadata
- Navigate to a non-BitFS tx — verify no panel injected
- Test on WoC testnet URL

## Network Detection

Determine network from URL:
- `whatsonchain.com/tx/*` → mainnet
- `test.whatsonchain.com/tx/*` → testnet

Used for generating correct parent TX links (link to same network's WoC).

## Future Extensions (Out of Scope for v0.1)

- Multi-output batch transaction decoding
- DAG visualization (parent → child tree)
- Link to bitfs-extension for file decryption
- Support for other block explorers
