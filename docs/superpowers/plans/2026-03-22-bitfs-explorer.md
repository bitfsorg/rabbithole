# BitFS Explorer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Chrome MV3 content-script-only extension that decodes BitFS/Metanet OP_RETURN data on WhatsOnChain tx pages.

**Architecture:** Vanilla TypeScript + Vite. Content script scans WoC tx pages for OP_RETURN outputs, detects MetaFlag, parses via libbitfs-ts, injects styled decoded panels. No popup, no background worker, no wallet.

**Tech Stack:** TypeScript, Vite (IIFE output), vitest, `@bitfs/libbitfs-ts` (metanet parser subset)

**Spec:** `docs/superpowers/specs/2026-03-22-bitfs-explorer-design.md`

---

### Task 1: Project Scaffold

Create the bitfs-explorer project with Vite, TypeScript, and extension manifest.

**Files:**
- Create: `bitfs-explorer/package.json`
- Create: `bitfs-explorer/tsconfig.json`
- Create: `bitfs-explorer/vite.config.ts`
- Create: `bitfs-explorer/manifest.json`
- Create: `bitfs-explorer/src/content.ts` (empty entry point)
- Create: `bitfs-explorer/src/style.css` (empty)

- [ ] **Step 1: Create package.json**

```json
{
  "name": "bitfs-explorer",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite build --watch",
    "build": "vite build",
    "test": "vitest run"
  },
  "dependencies": {
    "@bitfs/libbitfs": "file:../libbitfs-ts"
  },
  "devDependencies": {
    "typescript": "^5.7.0",
    "vite": "^6.0.0",
    "vitest": "^3.0.0",
    "vite-plugin-static-copy": "^2.2.0"
  }
}
```

- [ ] **Step 2: Create tsconfig.json**

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ESNext",
    "moduleResolution": "bundler",
    "strict": true,
    "esModuleInterop": true,
    "skipLibCheck": true,
    "outDir": "dist",
    "rootDir": "src",
    "types": ["chrome"]
  },
  "include": ["src"]
}
```

- [ ] **Step 3: Create vite.config.ts**

Content script must be IIFE format, CSS emitted separately.

```typescript
import { defineConfig } from 'vite'
import { viteStaticCopy } from 'vite-plugin-static-copy'

export default defineConfig({
  build: {
    outDir: 'dist',
    emptyOutDir: true,
    lib: {
      entry: 'src/content.ts',
      name: 'BitFSExplorer',
      formats: ['iife'],
      fileName: () => 'content.js',
    },
    rollupOptions: {
      output: {
        assetFileNames: 'style.css',
      },
    },
  },
  plugins: [
    viteStaticCopy({
      targets: [
        { src: 'manifest.json', dest: '.' },
        { src: 'icons/*', dest: 'icons' },
      ],
    }),
  ],
})
```

- [ ] **Step 4: Create manifest.json**

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

- [ ] **Step 5: Create placeholder source files**

`src/content.ts`:
```typescript
// BitFS Explorer — content script entry point
console.log('[BitFS Explorer] loaded')
```

`src/style.css`:
```css
/* BitFS Explorer — injected panel styles */
```

- [ ] **Step 6: Create placeholder icons**

Generate simple 16x16, 48x48, 128x128 PNG icons. Can be solid gold (`#c9956b`) squares with "B" text, or use the BitFS logo if available. Create `icons/` directory with placeholders.

- [ ] **Step 7: Install dependencies and verify build**

Run:
```bash
cd bitfs-explorer && bun install && bun run build
```
Expected: `dist/` directory with `content.js`, `style.css`, `manifest.json`, `icons/`

- [ ] **Step 8: Commit**

```bash
git add -A && git commit -m "feat: scaffold bitfs-explorer Chrome extension project"
```

---

### Task 2: Hex Decoder (TDD)

Pure logic — parse raw OP_RETURN script hex into push data arrays, detect MetaFlag, call libbitfs-ts parser.

**Files:**
- Create: `bitfs-explorer/src/decoder.ts`
- Create: `bitfs-explorer/test/decoder.test.ts`

**Reference:** The Metanet OP_RETURN format is `OP_FALSE (0x00) OP_RETURN (0x6a)` followed by push data items: `[MetaFlag, P_node, ParentTxID, Payload]`. MetaFlag is `0x6d657461` ("meta").

**Reference:** `parseNodeFromPushes(pushes: Uint8Array[]): Node` is exported from `@bitfs/libbitfs/metanet`. It takes a 4-element array of Uint8Array push items.

- [ ] **Step 1: Write failing test — hex to bytes**

`test/decoder.test.ts`:
```typescript
import { describe, it, expect } from 'vitest'
import { hexToBytes, parseScriptPushes, isMetanet, decodeMetanetOutput } from '../src/decoder'

describe('hexToBytes', () => {
  it('converts hex string to Uint8Array', () => {
    expect(hexToBytes('6d657461')).toEqual(new Uint8Array([0x6d, 0x65, 0x74, 0x61]))
  })

  it('returns empty array for empty string', () => {
    expect(hexToBytes('')).toEqual(new Uint8Array(0))
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd bitfs-explorer && bun test`
Expected: FAIL — module not found

- [ ] **Step 3: Implement hexToBytes**

`src/decoder.ts`:
```typescript
/** Convert hex string to Uint8Array. */
export function hexToBytes(hex: string): Uint8Array {
  if (hex.length === 0) return new Uint8Array(0)
  const bytes = new Uint8Array(hex.length / 2)
  for (let i = 0; i < bytes.length; i++) {
    bytes[i] = parseInt(hex.substring(i * 2, i * 2 + 2), 16)
  }
  return bytes
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `bun test`
Expected: PASS

- [ ] **Step 5: Write failing test — parseScriptPushes**

Add to `test/decoder.test.ts`:
```typescript
describe('parseScriptPushes', () => {
  it('parses OP_FALSE OP_RETURN with push data items', () => {
    // 00 6a 04 6d657461 21 <33 bytes pubkey> 20 <32 bytes parent> 4c <len> <payload>
    // Simplified: just MetaFlag push
    const script = '006a046d657461'
    const pushes = parseScriptPushes(script)
    expect(pushes).toHaveLength(1)
    expect(pushes[0]).toEqual(new Uint8Array([0x6d, 0x65, 0x74, 0x61]))
  })

  it('returns null for non OP_FALSE OP_RETURN script', () => {
    const script = '76a91489abcdef' // P2PKH
    expect(parseScriptPushes(script)).toBeNull()
  })

  it('handles OP_PUSHDATA1 (0x4c)', () => {
    // 00 6a 4c 04 6d657461 — PUSHDATA1 with 4-byte payload
    const script = '006a4c046d657461'
    const pushes = parseScriptPushes(script)
    expect(pushes).toHaveLength(1)
    expect(pushes[0]).toEqual(new Uint8Array([0x6d, 0x65, 0x74, 0x61]))
  })

  it('handles OP_PUSHDATA2 (0x4d)', () => {
    // 00 6a 4d 0400 6d657461 — PUSHDATA2 with 4-byte (LE) payload
    const script = '006a4d04006d657461'
    const pushes = parseScriptPushes(script)
    expect(pushes).toHaveLength(1)
    expect(pushes[0]).toEqual(new Uint8Array([0x6d, 0x65, 0x74, 0x61]))
  })

  it('returns null for truncated script', () => {
    expect(parseScriptPushes('006a04')).toBeNull()
  })
})
```

- [ ] **Step 6: Implement parseScriptPushes**

Add to `src/decoder.ts`:
```typescript
/**
 * Parse a raw script hex into push data items.
 * Returns null if the script is not OP_FALSE OP_RETURN format.
 */
export function parseScriptPushes(scriptHex: string): Uint8Array[] | null {
  const bytes = hexToBytes(scriptHex)
  if (bytes.length < 2 || bytes[0] !== 0x00 || bytes[1] !== 0x6a) return null

  const pushes: Uint8Array[] = []
  let i = 2
  while (i < bytes.length) {
    const op = bytes[i++]
    let len: number

    if (op >= 0x01 && op <= 0x4b) {
      len = op
    } else if (op === 0x4c) { // OP_PUSHDATA1
      if (i >= bytes.length) return null
      len = bytes[i++]
    } else if (op === 0x4d) { // OP_PUSHDATA2
      if (i + 1 >= bytes.length) return null
      len = bytes[i] | (bytes[i + 1] << 8)
      i += 2
    } else {
      return null // unexpected opcode
    }

    if (i + len > bytes.length) return null
    pushes.push(bytes.slice(i, i + len))
    i += len
  }
  return pushes
}
```

- [ ] **Step 7: Run tests**

Run: `bun test`
Expected: all parseScriptPushes tests PASS

- [ ] **Step 8: Write failing test — isMetanet**

Add to `test/decoder.test.ts`:
```typescript
describe('isMetanet', () => {
  it('returns true for MetaFlag push', () => {
    const pushes = [new Uint8Array([0x6d, 0x65, 0x74, 0x61])]
    expect(isMetanet(pushes)).toBe(true)
  })

  it('returns false for empty pushes', () => {
    expect(isMetanet([])).toBe(false)
  })

  it('returns false for non-MetaFlag first push', () => {
    const pushes = [new Uint8Array([0x00, 0x01, 0x02, 0x03])]
    expect(isMetanet(pushes)).toBe(false)
  })
})
```

- [ ] **Step 9: Implement isMetanet**

Add to `src/decoder.ts`:
```typescript
const META_FLAG = new Uint8Array([0x6d, 0x65, 0x74, 0x61]) // "meta"

/** Check if the first push data item is the Metanet flag. */
export function isMetanet(pushes: Uint8Array[]): boolean {
  if (pushes.length === 0) return false
  const first = pushes[0]
  return first.length === 4 &&
    first[0] === META_FLAG[0] && first[1] === META_FLAG[1] &&
    first[2] === META_FLAG[2] && first[3] === META_FLAG[3]
}
```

- [ ] **Step 10: Run tests**

Run: `bun test`
Expected: PASS

- [ ] **Step 11: Write failing test — decodeMetanetOutput**

This test uses a real Metanet tx output hex. Get one from the smoke test tx `56f25d08...`. The test calls the full decode pipeline: hex → pushes → libbitfs-ts Node.

Add to `test/decoder.test.ts`:
```typescript
describe('decodeMetanetOutput', () => {
  it('decodes a real Metanet OP_RETURN output', () => {
    // Use the OP_RETURN output (vout 0) from tx 56f25d08...
    // This is the hex of the first output's scriptPubKey
    // Extract this during implementation from the raw tx hex
    const scriptHex = '<<REAL_HEX_FROM_TX>>'
    const result = decodeMetanetOutput(scriptHex)
    expect(result).not.toBeNull()
    if (result) {
      expect(result.node.type).toBeDefined()
      expect(result.pNodeHex).toBeTruthy()
      expect(result.parentTxIDHex).toBeTruthy()
    }
  })

  it('returns null for non-Metanet output', () => {
    const p2pkh = '76a91489abcdefabbaabbaabbaabbaabbaabbaabbaabba88ac'
    expect(decodeMetanetOutput(p2pkh)).toBeNull()
  })

  it('returns null for malformed Metanet output', () => {
    // Has MetaFlag but truncated — not enough pushes
    expect(decodeMetanetOutput('006a046d657461')).toBeNull()
  })
})
```

Note: The `<<REAL_HEX_FROM_TX>>` placeholder must be filled during implementation. Extract vout 0 scriptPubKey hex from the raw tx `56f25d0881592657cd845816f9381f0d31fa267712402d678d631a0aa3766dac` (already broadcast on mainnet — can get from WoC API: `GET https://api.whatsonchain.com/v1/bsv/main/tx/56f25d0881592657cd845816f9381f0d31fa267712402d678d631a0aa3766dac/hex`, then parse the raw tx to extract vout 0 script).

- [ ] **Step 12: Implement decodeMetanetOutput**

Add to `src/decoder.ts`:
```typescript
import { parseNodeFromPushes } from '@bitfs/libbitfs/metanet'
import type { Node } from '@bitfs/libbitfs/metanet'

export interface DecodedOutput {
  node: Node
  pNodeHex: string
  parentTxIDHex: string
}

/** Decode a raw script hex into a Metanet Node. Returns null if not Metanet. */
export function decodeMetanetOutput(scriptHex: string): DecodedOutput | null {
  const pushes = parseScriptPushes(scriptHex)
  if (!pushes || !isMetanet(pushes)) return null
  if (pushes.length < 4) return null // need MetaFlag, P_node, ParentTxID, Payload

  try {
    const node = parseNodeFromPushes(pushes)
    return {
      node,
      pNodeHex: bytesToHex(pushes[1]),
      parentTxIDHex: bytesToHex(pushes[2]),
    }
  } catch {
    return null
  }
}

/** Convert Uint8Array to hex string. */
export function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes).map(b => b.toString(16).padStart(2, '0')).join('')
}
```

- [ ] **Step 13: Run all tests**

Run: `bun test`
Expected: PASS

- [ ] **Step 14: Commit**

```bash
git add src/decoder.ts test/decoder.test.ts
git commit -m "feat: add OP_RETURN hex decoder with MetaFlag detection and libbitfs-ts integration"
```

---

### Task 3: UI Panel Builder (TDD)

Build the DOM panel that displays decoded Metanet node data. Pure DOM generation — no DOM scraping.

**Files:**
- Create: `bitfs-explorer/src/ui.ts`
- Create: `bitfs-explorer/test/ui.test.ts`

- [ ] **Step 1: Write failing test — panel renders for file node**

`test/ui.test.ts`:
```typescript
import { describe, it, expect, beforeEach } from 'vitest'
import { buildPanel } from '../src/ui'
import type { DecodedOutput } from '../src/decoder'

// Mock a decoded file node
function mockFileOutput(): DecodedOutput {
  return {
    pNodeHex: '02d1c8985fe9fcde363c7b5723ff5952fdfca6f43b6e35fd90bd36ec6b4556ccbb',
    parentTxIDHex: '0ed6cefe627d01c9bd5a3da8f9a5c37bd0f328a5cba30b29bcd59e13a85f43f2',
    node: {
      type: 0,  // File
      op: 0,    // Create
      mimeType: 'text/plain',
      fileSize: 57n,
      access: 1, // Free
      pricePerKB: 0n,
      timestamp: 1742605203n,
      children: [],
    } as any,
  }
}

describe('buildPanel', () => {
  it('renders a panel with correct fields for a file node', () => {
    const panel = buildPanel(mockFileOutput(), 'mainnet')
    expect(panel.classList.contains('bfx-panel')).toBe(true)
    expect(panel.innerHTML).toContain('FILE')
    expect(panel.innerHTML).toContain('CREATE')
    expect(panel.innerHTML).toContain('text/plain')
    expect(panel.innerHTML).toContain('57 B')
    expect(panel.innerHTML).toContain('FREE')
    expect(panel.innerHTML).toContain('02d1c8')
  })

  it('renders parent TX as clickable WoC link', () => {
    const panel = buildPanel(mockFileOutput(), 'mainnet')
    const link = panel.querySelector('a[href*="whatsonchain.com/tx/"]')
    expect(link).not.toBeNull()
    expect(link?.getAttribute('href')).toContain('0ed6cefe')
  })

  it('uses test.whatsonchain.com for testnet links', () => {
    const panel = buildPanel(mockFileOutput(), 'testnet')
    const link = panel.querySelector('a[href*="whatsonchain.com/tx/"]')
    expect(link?.getAttribute('href')).toContain('test.whatsonchain.com')
  })
})
```

- [ ] **Step 2: Run test to verify it fails**

Run: `bun test`
Expected: FAIL — buildPanel not found

- [ ] **Step 3: Implement buildPanel**

`src/ui.ts`:
```typescript
import type { DecodedOutput } from './decoder'

const NODE_TYPES = ['FILE', 'DIR', 'LINK', 'ANCHOR']
const OP_TYPES = ['CREATE', 'UPDATE', 'DELETE']
const ACCESS_TYPES = ['PRIVATE', 'FREE', 'PAID']

function formatSize(bytes: bigint): string {
  const n = Number(bytes)
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}

function formatTimestamp(ts: bigint): string {
  if (ts === 0n) return ''
  return new Date(Number(ts) * 1000).toISOString()
}

function truncateHex(hex: string, chars = 6): string {
  if (hex.length <= chars * 2 + 3) return hex
  return hex.substring(0, chars) + '...' + hex.substring(hex.length - chars)
}

function wocTxURL(txid: string, network: string): string {
  const host = network === 'testnet' ? 'test.whatsonchain.com' : 'whatsonchain.com'
  return `https://${host}/tx/${txid}`
}

export function buildPanel(decoded: DecodedOutput, network: string): HTMLDivElement {
  const { node, pNodeHex, parentTxIDHex } = decoded
  const panel = document.createElement('div')
  panel.className = 'bfx-panel'

  const typeBadge = NODE_TYPES[node.type] ?? 'UNKNOWN'
  const opBadge = OP_TYPES[node.op] ?? '?'
  const accessLabel = ACCESS_TYPES[node.access] ?? '?'
  const accessFull = node.access === 2 && node.pricePerKB > 0n
    ? `PAID (${node.pricePerKB} sat/KB)` : accessLabel

  let html = `
    <div class="bfx-header">
      <span class="bfx-diamond">&#9670;</span> BitFS Metanet Node
      <span class="bfx-toggle" title="Collapse">&#9662;</span>
    </div>
    <div class="bfx-body">
      <div class="bfx-badges">
        <span class="bfx-badge">${typeBadge}</span>
        <span class="bfx-badge">${opBadge}</span>
        ${node.mimeType ? `<span class="bfx-meta">${node.mimeType}</span>` : ''}
        ${node.fileSize > 0n ? `<span class="bfx-meta">${formatSize(node.fileSize)}</span>` : ''}
        <span class="bfx-badge bfx-access">${accessFull}</span>
      </div>
      <div class="bfx-row">
        <span class="bfx-label">P_node:</span>
        <code class="bfx-hex">${truncateHex(pNodeHex)}</code>
        <button class="bfx-copy" data-copy="${pNodeHex}" title="Copy full pubkey">copy</button>
      </div>
  `

  if (parentTxIDHex && parentTxIDHex !== '0'.repeat(64)) {
    html += `
      <div class="bfx-row">
        <span class="bfx-label">Parent TX:</span>
        <a class="bfx-link" href="${wocTxURL(parentTxIDHex, network)}" target="_blank">${truncateHex(parentTxIDHex)}</a>
      </div>
    `
  }

  const ts = formatTimestamp(node.timestamp)
  if (ts) {
    html += `
      <div class="bfx-row">
        <span class="bfx-label">Timestamp:</span>
        <span>${ts}</span>
      </div>
    `
  }

  if (node.children && node.children.length > 0) {
    html += `<div class="bfx-children"><span class="bfx-label">Children:</span><ul>`
    for (const child of node.children) {
      const icon = child.type === 1 ? '&#128193;' : '&#128196;' // folder or file
      const name = child.name || '(unnamed)'
      html += `<li>${icon} ${name}</li>`
    }
    html += '</ul></div>'
  }

  html += '</div>' // close bfx-body
  panel.innerHTML = html

  // Wire up collapse toggle
  const toggle = panel.querySelector('.bfx-toggle')
  const body = panel.querySelector('.bfx-body')
  if (toggle && body) {
    toggle.addEventListener('click', () => {
      const collapsed = body.classList.toggle('bfx-collapsed')
      toggle.innerHTML = collapsed ? '&#9656;' : '&#9662;'
    })
  }

  // Wire up copy buttons
  panel.querySelectorAll('.bfx-copy').forEach(btn => {
    btn.addEventListener('click', () => {
      const text = (btn as HTMLElement).dataset.copy ?? ''
      navigator.clipboard.writeText(text).catch(() => {
        // Fallback for restricted contexts
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
      })
    })
  })

  return panel
}
```

- [ ] **Step 4: Run tests**

Run: `bun test`
Expected: PASS (vitest uses jsdom/happy-dom for DOM)

Note: May need to add `environment: 'happy-dom'` to vitest config. If so, install `happy-dom` and add to `vite.config.ts`:
```typescript
test: { environment: 'happy-dom' }
```

- [ ] **Step 5: Write test — directory node with children**

Add to `test/ui.test.ts`:
```typescript
it('renders children list for directory node', () => {
  const decoded = mockFileOutput()
  decoded.node.type = 1 // Dir
  decoded.node.children = [
    { name: 'readme.txt', type: 0 },
    { name: 'docs', type: 1 },
  ] as any
  const panel = buildPanel(decoded, 'mainnet')
  expect(panel.innerHTML).toContain('DIR')
  expect(panel.innerHTML).toContain('readme.txt')
  expect(panel.innerHTML).toContain('docs')
})
```

- [ ] **Step 6: Run tests**

Run: `bun test`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add src/ui.ts test/ui.test.ts
git commit -m "feat: add decoded panel UI builder with collapse toggle and copy buttons"
```

---

### Task 4: CSS Styling

**Files:**
- Modify: `bitfs-explorer/src/style.css`

- [ ] **Step 1: Write panel styles**

`src/style.css`:
```css
.bfx-panel {
  background: #1a1a2e;
  border: 1px solid #2a2a4a;
  border-radius: 8px;
  margin: 8px 0;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
  font-size: 13px;
  color: #e0e0e0;
  overflow: hidden;
}

.bfx-header {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  background: #16162a;
  border-bottom: 1px solid #2a2a4a;
  font-weight: 600;
  font-size: 13px;
  cursor: default;
}

.bfx-diamond {
  color: #c9956b;
  margin-right: 8px;
  font-size: 14px;
}

.bfx-toggle {
  margin-left: auto;
  cursor: pointer;
  color: #888;
  font-size: 16px;
  user-select: none;
}

.bfx-toggle:hover { color: #c9956b; }

.bfx-body { padding: 10px 12px; }
.bfx-body.bfx-collapsed { display: none; }

.bfx-badges {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 8px;
}

.bfx-badge {
  background: #2a2a4a;
  color: #c9956b;
  padding: 2px 8px;
  border-radius: 4px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
}

.bfx-meta {
  color: #999;
  font-size: 12px;
  padding: 2px 0;
}

.bfx-access { color: #7ec8a0; }

.bfx-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 0;
}

.bfx-label {
  color: #888;
  min-width: 80px;
  font-size: 12px;
}

.bfx-hex {
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
  color: #b0b0d0;
}

.bfx-copy {
  background: none;
  border: 1px solid #444;
  color: #888;
  font-size: 10px;
  padding: 1px 6px;
  border-radius: 3px;
  cursor: pointer;
}

.bfx-copy:hover { color: #c9956b; border-color: #c9956b; }

.bfx-link {
  color: #7ea8c8;
  text-decoration: none;
  font-family: 'JetBrains Mono', monospace;
  font-size: 12px;
}

.bfx-link:hover { text-decoration: underline; }

.bfx-children {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #2a2a4a;
}

.bfx-children ul {
  list-style: none;
  padding: 0;
  margin: 4px 0 0 0;
}

.bfx-children li {
  padding: 2px 0;
  font-size: 12px;
}
```

- [ ] **Step 2: Import CSS in content.ts**

Add to top of `src/content.ts`:
```typescript
import './style.css'
```

- [ ] **Step 3: Build and verify CSS is emitted**

Run: `bun run build`
Expected: `dist/style.css` exists and contains `.bfx-panel` rules

- [ ] **Step 4: Commit**

```bash
git add src/style.css src/content.ts
git commit -m "feat: add dark-themed panel CSS matching WoC aesthetic"
```

---

### Task 5: WoC DOM Scraper

Scrape OP_RETURN script hex from WoC tx pages. This module is isolated for easy updates if WoC changes their DOM.

**Files:**
- Create: `bitfs-explorer/src/scraper.ts`

**Important:** WoC is a React SPA with minified class names. The scraper must use stable signals (text content, data attributes, element structure) rather than class names. The implementer MUST open a real WoC tx page in Chrome DevTools to identify the correct selectors. The plan provides a skeleton; the selectors must be filled in during implementation.

- [ ] **Step 1: Inspect WoC DOM**

Open `https://whatsonchain.com/tx/56f25d0881592657cd845816f9381f0d31fa267712402d678d631a0aa3766dac` in Chrome DevTools. Identify:
1. The container for each transaction output
2. How OP_RETURN scripts are displayed (raw hex? ASM? Both?)
3. Where the script hex lives in the DOM
4. A stable way to identify OP_RETURN outputs vs P2PKH outputs

Document findings as comments in `scraper.ts`.

- [ ] **Step 2: Implement scraper**

`src/scraper.ts`:
```typescript
export interface ScrapedOutput {
  scriptHex: string
  element: Element  // the DOM element to inject the panel after
}

/**
 * Scrape OP_RETURN outputs from the current WoC tx page.
 * Returns an array of {scriptHex, element} pairs.
 *
 * NOTE: Selectors must be updated if WoC changes their DOM structure.
 * Inspect the page at https://whatsonchain.com/tx/<txid> to find the
 * correct selectors.
 */
export function scrapeOutputs(): ScrapedOutput[] {
  const results: ScrapedOutput[] = []

  // IMPLEMENTATION NOTE: The exact selectors below are placeholders.
  // During implementation, inspect the WoC page DOM and replace with
  // the actual selectors. The general strategy:
  //
  // 1. Find the outputs section (usually contains "Outputs" text)
  // 2. For each output card/row, look for script content
  // 3. If script content starts with "OP_FALSE OP_RETURN" (ASM) or
  //    the hex starts with "006a", extract the full hex
  //
  // WoC may show scripts in ASM format. If so, we need to find the
  // raw hex view. Some WoC pages have a "Hex" toggle or show both.
  // Alternatively, we can reconstruct hex from the ASM if needed,
  // but this is fragile. Prefer finding raw hex.

  // Strategy: Find all elements that contain script hex or ASM data
  // Look for elements with OP_RETURN-like content
  const allTextElements = document.querySelectorAll('code, pre, [class*="script"], td')
  for (const el of allTextElements) {
    const text = el.textContent?.trim() ?? ''

    // Check for raw hex starting with 006a (OP_FALSE OP_RETURN)
    if (/^006a[0-9a-f]+$/i.test(text)) {
      results.push({ scriptHex: text.toLowerCase(), element: el.closest('tr, div, section') ?? el })
      continue
    }

    // Check for ASM format: "0 OP_RETURN ..."
    if (text.startsWith('0 OP_RETURN') || text.startsWith('OP_FALSE OP_RETURN')) {
      // ASM format — need to find the hex version nearby
      // Look for a sibling or child element with raw hex
      const parent = el.closest('tr, div, section')
      if (parent) {
        const hexEl = parent.querySelector('[class*="hex"], code')
        const hexText = hexEl?.textContent?.trim() ?? ''
        if (/^006a[0-9a-f]+$/i.test(hexText)) {
          results.push({ scriptHex: hexText.toLowerCase(), element: parent })
        }
      }
    }
  }

  return results
}

/** Detect network from current URL. */
export function detectNetwork(): string {
  return window.location.hostname.startsWith('test.') ? 'testnet' : 'mainnet'
}
```

- [ ] **Step 3: Test manually in Chrome**

Load the extension in Chrome (`chrome://extensions` → Load unpacked → `dist/`). Navigate to a BitFS tx on WoC. Open console and check if scraper finds outputs. Iterate on selectors if needed.

- [ ] **Step 4: Commit**

```bash
git add src/scraper.ts
git commit -m "feat: add WoC DOM scraper for OP_RETURN outputs"
```

---

### Task 6: Content Script Integration

Wire everything together: scraper → decoder → UI → inject.

**Files:**
- Modify: `bitfs-explorer/src/content.ts`

- [ ] **Step 1: Implement content script**

`src/content.ts`:
```typescript
import './style.css'
import { scrapeOutputs, detectNetwork } from './scraper'
import { decodeMetanetOutput } from './decoder'
import { buildPanel } from './ui'

const MARKER = 'data-bfx-decoded'

function scan(): void {
  const network = detectNetwork()
  const outputs = scrapeOutputs()

  for (const { scriptHex, element } of outputs) {
    // Skip if already decoded
    if (element.hasAttribute(MARKER)) continue

    const decoded = decodeMetanetOutput(scriptHex)
    if (!decoded) continue

    const panel = buildPanel(decoded, network)
    element.setAttribute(MARKER, 'true')
    element.insertAdjacentElement('afterend', panel)
  }
}

// Initial scan
scan()

// Re-scan on SPA navigation (WoC uses client-side routing)
const observer = new MutationObserver(() => {
  // Debounce: wait for DOM to settle
  clearTimeout((observer as any)._timer)
  ;(observer as any)._timer = setTimeout(scan, 500)
})

observer.observe(document.body, {
  childList: true,
  subtree: true,
})
```

- [ ] **Step 2: Build**

Run: `bun run build`
Expected: `dist/content.js` contains bundled IIFE with all modules

- [ ] **Step 3: Manual test in Chrome**

1. Load extension: `chrome://extensions` → Load unpacked → select `bitfs-explorer/dist/`
2. Navigate to: `https://whatsonchain.com/tx/56f25d0881592657cd845816f9381f0d31fa267712402d678d631a0aa3766dac`
3. Expected: Decoded BitFS panel appears below the OP_RETURN output showing FILE CREATE, text/plain, 57 B, FREE, pubkey, parent link
4. Navigate to a non-BitFS tx — no panel should appear
5. Test collapse toggle and copy button

If the scraper doesn't find outputs, inspect the DOM and update `scraper.ts` selectors. Iterate until it works.

- [ ] **Step 4: Commit**

```bash
git add src/content.ts
git commit -m "feat: integrate scraper + decoder + UI into content script with SPA observer"
```

---

### Task 7: Generate Extension Icons

**Files:**
- Create: `bitfs-explorer/icons/icon-16.png`
- Create: `bitfs-explorer/icons/icon-48.png`
- Create: `bitfs-explorer/icons/icon-128.png`

- [ ] **Step 1: Generate icons**

Create simple PNG icons with the BitFS gold color (`#c9956b`). Options:
- Use an existing BitFS logo from `docs/vi/` if available
- Or generate minimal icons: gold diamond shape on transparent background
- Use any image tool (Figma, ImageMagick, or a simple script)

Example with ImageMagick (if available):
```bash
for size in 16 48 128; do
  convert -size ${size}x${size} xc:transparent -fill '#c9956b' \
    -draw "polygon $((size/2)),0 $((size)),$(($size/2)) $(($size/2)),$size 0,$(($size/2))" \
    icons/icon-${size}.png
done
```

- [ ] **Step 2: Build and verify icons copied**

Run: `bun run build`
Expected: `dist/icons/` contains all three PNGs

- [ ] **Step 3: Commit**

```bash
git add icons/
git commit -m "feat: add extension icons"
```

---

### Task 8: Final Build and Verification

- [ ] **Step 1: Run all tests**

Run: `cd bitfs-explorer && bun test`
Expected: ALL PASS

- [ ] **Step 2: Clean build**

Run: `rm -rf dist && bun run build`
Expected: `dist/` contains: `content.js`, `style.css`, `manifest.json`, `icons/`

- [ ] **Step 3: Load in Chrome and verify on mainnet**

1. `chrome://extensions` → Load unpacked → `dist/`
2. Visit `https://whatsonchain.com/tx/56f25d0881592657cd845816f9381f0d31fa267712402d678d631a0aa3766dac`
3. Verify decoded panel shows correct data
4. Visit `https://whatsonchain.com/tx/3524921055186174fdd9c5f900e9d9ccd282b66bfc6d8c275bbac68d7ed5a4e5` (directory node)
5. Verify directory children are listed

- [ ] **Step 4: Test on testnet URL**

Visit a known testnet BitFS tx (if available) at `test.whatsonchain.com/tx/...`. Verify panel links use testnet WoC URLs.

- [ ] **Step 5: Final commit**

```bash
git add -A && git commit -m "chore: final build verification for bitfs-explorer v0.1.0"
```
