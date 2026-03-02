# BitFS Chrome Extension Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Complete the BitFS Chrome Extension — a fully independent MetaMask-model wallet that connects directly to BSV blockchain via public APIs.

**Architecture:** MV3 extension with 3 layers: Popup (React 19 + Tailwind + Context), Service Worker (wallet/DAG/tx engine), Content Script (link detection + 402 payment + window.bitfs). All crypto via `@bitfs/libbitfs` (linked from `../libbitfs-ts`). Blockchain access via WhatsOnChain REST API. DAG cached in IndexedDB.

**Tech Stack:** React 19, TypeScript 5.7, Vite 6, Tailwind CSS, `@bitfs/libbitfs` (local link), Vitest, Chrome MV3 APIs.

**Design Doc:** `docs/plans/2026-03-02-bitfs-extension-design.md`

**Working Directory:** `/Users/alex/Codes/RabbitHole/bitfs-extension/`

---

## Phase 0: Project Restructure

### Task 1: Update dependencies and remove bitfs-core stubs

**Files:**
- Modify: `package.json`
- Delete: `src/bitfs-core/types.ts`
- Delete: `src/bitfs-core/wallet.ts`
- Delete: `src/bitfs-core/method42.ts`
- Delete: `src/bitfs-core/metanet.ts`
- Delete: `src/bitfs-core/x402.ts`
- Delete: `test/method42.test.ts`

**Step 1: Update package.json**

Replace `package.json` with:

```json
{
  "name": "@bitfs/extension",
  "private": true,
  "type": "module",
  "version": "0.1.0",
  "scripts": {
    "dev": "vite build --watch",
    "build": "tsc && vite build",
    "test": "vitest run"
  },
  "dependencies": {
    "@bitfs/libbitfs": "file:../libbitfs-ts",
    "react": "^19",
    "react-dom": "^19",
    "react-router-dom": "^7"
  },
  "devDependencies": {
    "@tailwindcss/vite": "^4",
    "@types/chrome": "^0.0.304",
    "@types/react": "^19",
    "@types/react-dom": "^19",
    "@vitejs/plugin-react": "^4",
    "tailwindcss": "^4",
    "typescript": "^5.7",
    "vite": "^6",
    "vitest": "^3"
  }
}
```

Key changes:
- Added `@bitfs/libbitfs` via file link (replaces `@noble/*` and `@scure/*`)
- Added `tailwindcss` v4 + `@tailwindcss/vite` plugin
- Removed `@noble/secp256k1`, `@noble/hashes`, `@scure/bip32`, `@scure/bip39`

**Step 2: Delete bitfs-core stubs**

```bash
rm -rf src/bitfs-core/
rm test/method42.test.ts
```

**Step 3: Install dependencies**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs-extension
npm install
```

**Step 4: Update vite.config.ts**

Replace `vite.config.ts`:

```typescript
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { resolve } from "path";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": resolve(__dirname, "src"),
    },
  },
  build: {
    outDir: "dist",
    rollupOptions: {
      input: {
        popup: resolve(__dirname, "src/popup/index.html"),
        "service-worker": resolve(
          __dirname,
          "src/background/service-worker.ts"
        ),
        "content-script": resolve(
          __dirname,
          "src/content-script/index.ts"
        ),
      },
      output: {
        entryFileNames: (chunkInfo) => {
          if (chunkInfo.name === "service-worker")
            return "background/service-worker.js";
          if (chunkInfo.name === "content-script")
            return "content-script/index.js";
          return "[name]/[name]-[hash].js";
        },
        chunkFileNames: "chunks/[name]-[hash].js",
        assetFileNames: "assets/[name]-[hash].[ext]",
      },
    },
  },
});
```

**Step 5: Add Tailwind CSS entry**

Create `src/popup/index.css`:

```css
@import "tailwindcss";

@theme {
  --color-bg: #1a1a1a;
  --color-bg-secondary: #252525;
  --color-bg-tertiary: #2f2f2f;
  --color-border: #3a3a3a;
  --color-accent: #c9956b;
  --color-accent-hover: #d4a57d;
  --color-text: #e5e5e5;
  --color-text-secondary: #999999;
  --color-success: #4caf50;
  --color-warning: #e8983e;
  --color-error: #ef4444;
  --font-mono: "JetBrains Mono", "Fira Code", monospace;
}
```

Update `src/popup/index.html` to import the CSS:

```html
<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>BitFS</title>
    <link
      href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap"
      rel="stylesheet"
    />
  </head>
  <body class="w-[360px] h-[500px] m-0 p-0 bg-bg text-text font-sans">
    <div id="root" class="h-full"></div>
    <script type="module" src="./main.tsx"></script>
  </body>
</html>
```

Update `src/popup/main.tsx`:

```typescript
import "./index.css";
import { createRoot } from "react-dom/client";
import { App } from "./App";

const root = document.getElementById("root");
if (!root) throw new Error("Root element not found");

createRoot(root).render(<App />);
```

**Step 6: Verify build**

```bash
npm run build
```

Expected: Build succeeds, dist/ contains popup/index.html, background/service-worker.js, content-script/index.js.

**Step 7: Commit**

```bash
git add -A
git commit -m "refactor: restructure project, link libbitfs-ts, add Tailwind v4

- Remove src/bitfs-core/ stubs (replaced by @bitfs/libbitfs)
- Add @bitfs/libbitfs via file:../libbitfs-ts local link
- Add Tailwind CSS v4 with BitFS dark theme
- Update vite.config.ts with tailwindcss plugin
- Remove @noble/* and @scure/* direct deps"
```

---

## Phase 1: Core Infrastructure

### Task 2: Message protocol and types

**Files:**
- Create: `src/lib/messages.ts`

**Step 1: Create message protocol**

```typescript
// Message types for chrome.runtime.sendMessage communication
// between Popup ↔ Service Worker ↔ Content Script

export type WalletStatus = "none" | "locked" | "unlocked";

export type MessageType =
  | "WALLET_CREATE"
  | "WALLET_IMPORT"
  | "WALLET_UNLOCK"
  | "WALLET_LOCK"
  | "GET_STATUS"
  | "GET_ADDRESS"
  | "GET_BALANCE"
  | "SCAN_DAG"
  | "LIST_FILES"
  | "READ_FILE"
  | "SIGN_TX"
  | "BROADCAST_TX"
  | "PAY_INVOICE"
  | "GET_SETTINGS"
  | "SET_SETTINGS";

export interface Message {
  type: MessageType;
  payload?: unknown;
}

export interface Response<T = unknown> {
  ok: boolean;
  data?: T;
  error?: string;
}

// Wallet messages
export interface WalletCreatePayload {
  password: string;
}

export interface WalletCreateResult {
  mnemonic: string;
  address: string;
}

export interface WalletImportPayload {
  mnemonic: string;
  password: string;
}

export interface WalletUnlockPayload {
  password: string;
}

export interface WalletStatusResult {
  status: WalletStatus;
  address?: string;
  network?: string;
}

// Balance
export interface BalanceResult {
  confirmed: bigint;
  unconfirmed: bigint;
}

// DAG / Files
export interface ScanDAGPayload {
  force?: boolean; // force full rescan
}

export interface ListFilesPayload {
  path: string; // "/" for root
}

export interface FileEntry {
  name: string;
  type: "dir" | "file" | "link";
  size?: number;
  access?: number; // AccessLevel
  txid?: string;
}

export interface ReadFilePayload {
  path: string;
}

export interface ReadFileResult {
  content: Uint8Array;
  mimeType?: string;
  size: number;
  access: number;
}

// Transaction
export interface BroadcastTxPayload {
  rawTxHex: string;
}

// x402 Payment
export interface PayInvoicePayload {
  invoiceUrl: string;
  price: number; // satoshis
  invoiceId: string;
}

// Settings
export interface ExtensionSettings {
  apiProviderUrl: string;
  network: "mainnet" | "testnet";
  autoLockMinutes: number;
}

// Helper to send messages from Popup/ContentScript → ServiceWorker
export async function sendMessage<T>(msg: Message): Promise<T> {
  const response = await chrome.runtime.sendMessage(msg);
  const typed = response as Response<T>;
  if (!typed.ok) throw new Error(typed.error ?? "Unknown error");
  return typed.data as T;
}
```

**Step 2: Commit**

```bash
git add src/lib/messages.ts
git commit -m "feat: add message protocol types for extension IPC"
```

---

### Task 3: Chrome storage wrapper

**Files:**
- Modify: `src/lib/storage.ts`
- Create: `test/storage.test.ts`

**Step 1: Write tests**

```typescript
import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock chrome.storage API for testing
const mockStore: Record<string, unknown> = {};

vi.stubGlobal("chrome", {
  storage: {
    local: {
      get: vi.fn((keys: string[]) =>
        Promise.resolve(
          Object.fromEntries(keys.map((k) => [k, mockStore[k]]))
        )
      ),
      set: vi.fn((items: Record<string, unknown>) => {
        Object.assign(mockStore, items);
        return Promise.resolve();
      }),
      remove: vi.fn((keys: string[]) => {
        for (const k of keys) delete mockStore[k];
        return Promise.resolve();
      }),
    },
    session: {
      get: vi.fn((keys: string[]) =>
        Promise.resolve(
          Object.fromEntries(keys.map((k) => [k, mockStore[`session:${k}`]]))
        )
      ),
      set: vi.fn((items: Record<string, unknown>) => {
        for (const [k, v] of Object.entries(items))
          mockStore[`session:${k}`] = v;
        return Promise.resolve();
      }),
      remove: vi.fn((keys: string[]) => {
        for (const k of keys) delete mockStore[`session:${k}`];
        return Promise.resolve();
      }),
    },
  },
});

import { localGet, localSet, localRemove, sessionGet, sessionSet } from "@/lib/storage";

describe("Chrome Storage Wrapper", () => {
  beforeEach(() => {
    for (const k of Object.keys(mockStore)) delete mockStore[k];
  });

  it("should set and get from local storage", async () => {
    await localSet("testKey", { foo: "bar" });
    const result = await localGet<{ foo: string }>("testKey");
    expect(result).toEqual({ foo: "bar" });
  });

  it("should return null for missing keys", async () => {
    const result = await localGet("nonexistent");
    expect(result).toBeNull();
  });

  it("should remove from local storage", async () => {
    await localSet("toDelete", "value");
    await localRemove("toDelete");
    const result = await localGet("toDelete");
    expect(result).toBeNull();
  });

  it("should set and get from session storage", async () => {
    await sessionSet("sessionKey", 42);
    const result = await sessionGet<number>("sessionKey");
    expect(result).toBe(42);
  });
});
```

**Step 2: Run test to verify it fails**

```bash
npm test -- test/storage.test.ts
```

Expected: FAIL — functions not exported.

**Step 3: Implement storage wrapper**

Replace `src/lib/storage.ts`:

```typescript
// Chrome storage wrapper
// local: persisted (encrypted wallet data)
// session: MV3 session-only (unlocked state)

export async function localGet<T>(key: string): Promise<T | null> {
  const result = await chrome.storage.local.get([key]);
  return (result[key] as T) ?? null;
}

export async function localSet<T>(key: string, value: T): Promise<void> {
  await chrome.storage.local.set({ [key]: value });
}

export async function localRemove(key: string): Promise<void> {
  await chrome.storage.local.remove([key]);
}

export async function sessionGet<T>(key: string): Promise<T | null> {
  const result = await chrome.storage.session.get([key]);
  return (result[key] as T) ?? null;
}

export async function sessionSet<T>(key: string, value: T): Promise<void> {
  await chrome.storage.session.set({ [key]: value });
}

export async function sessionRemove(key: string): Promise<void> {
  await chrome.storage.session.remove([key]);
}
```

**Step 4: Run tests**

```bash
npm test -- test/storage.test.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add src/lib/storage.ts test/storage.test.ts
git commit -m "feat: implement Chrome storage wrapper with local/session APIs"
```

---

### Task 4: WhatsOnChain blockchain provider

This implements the `BlockchainService` interface from libbitfs-ts using WhatsOnChain REST API.

**Files:**
- Create: `src/lib/provider.ts`
- Create: `test/provider.test.ts`

**Step 1: Write tests**

```typescript
import { describe, it, expect, vi, beforeEach } from "vitest";
import { WhatsOnChainProvider } from "@/lib/provider";

// Mock fetch
const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

describe("WhatsOnChainProvider", () => {
  let provider: WhatsOnChainProvider;

  beforeEach(() => {
    provider = new WhatsOnChainProvider("main");
    mockFetch.mockReset();
  });

  it("should list unspent UTXOs for address", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () =>
        Promise.resolve([
          {
            tx_hash: "abc123",
            tx_pos: 0,
            value: 50000,
            height: 800000,
          },
        ]),
    });

    const utxos = await provider.listUnspent("1A1zP1...");
    expect(utxos).toHaveLength(1);
    expect(utxos[0].txid).toBe("abc123");
    expect(utxos[0].vout).toBe(0);
    expect(utxos[0].amount).toBe(BigInt(50000));
  });

  it("should broadcast transaction", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      text: () => Promise.resolve("txid_result_hex"),
    });

    const txid = await provider.broadcastTx("0100000001...");
    expect(txid).toBe("txid_result_hex");
  });

  it("should get raw transaction hex", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      text: () => Promise.resolve("0100000001abcdef..."),
    });

    const raw = await provider.getRawTx("abc123");
    expect(raw).toBeInstanceOf(Uint8Array);
    expect(raw.length).toBeGreaterThan(0);
  });

  it("should throw on HTTP error", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: false,
      status: 404,
      statusText: "Not Found",
    });

    await expect(provider.listUnspent("invalid")).rejects.toThrow();
  });

  it("should get best block height", async () => {
    mockFetch.mockResolvedValueOnce({
      ok: true,
      json: () =>
        Promise.resolve({
          blocks: 850000,
        }),
    });

    const height = await provider.getBestBlockHeight();
    expect(height).toBe(850000);
  });
});
```

**Step 2: Run test to verify it fails**

```bash
npm test -- test/provider.test.ts
```

Expected: FAIL.

**Step 3: Implement WhatsOnChain provider**

```typescript
// WhatsOnChain REST API implementation of BlockchainService
// API docs: https://developers.whatsonchain.com/

import type { BlockchainService, UTXO, TxStatus, MerkleProofData } from "@bitfs/libbitfs/network";

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substring(i, i + 2), 16);
  }
  return bytes;
}

function bytesToHex(bytes: Uint8Array): string {
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}

export class WhatsOnChainProvider implements BlockchainService {
  private baseUrl: string;

  constructor(network: string = "main") {
    this.baseUrl = `https://api.whatsonchain.com/v1/bsv/${network}`;
  }

  private async fetchJSON<T>(path: string): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`);
    if (!res.ok) throw new Error(`WoC API error: ${res.status} ${res.statusText}`);
    return res.json() as Promise<T>;
  }

  private async fetchText(path: string): Promise<string> {
    const res = await fetch(`${this.baseUrl}${path}`);
    if (!res.ok) throw new Error(`WoC API error: ${res.status} ${res.statusText}`);
    return res.text();
  }

  async listUnspent(address: string): Promise<UTXO[]> {
    interface WocUTXO {
      tx_hash: string;
      tx_pos: number;
      value: number;
      height: number;
    }
    const data = await this.fetchJSON<WocUTXO[]>(
      `/address/${address}/unspent`
    );
    return data.map((u) => ({
      txid: u.tx_hash,
      vout: u.tx_pos,
      amount: BigInt(u.value),
      scriptPubKey: "",
      address,
      confirmations: u.height > 0 ? 1 : 0,
    }));
  }

  async getUTXO(txid: string, vout: number): Promise<UTXO | null> {
    const utxos = await this.listUnspent(txid); // WoC doesn't have direct UTXO lookup
    return utxos.find((u) => u.txid === txid && u.vout === vout) ?? null;
  }

  async broadcastTx(rawTxHex: string): Promise<string> {
    const res = await fetch(`${this.baseUrl}/tx/raw`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ txhex: rawTxHex }),
    });
    if (!res.ok) throw new Error(`Broadcast failed: ${res.status}`);
    return res.text();
  }

  async getRawTx(txid: string): Promise<Uint8Array> {
    const hex = await this.fetchText(`/tx/${txid}/hex`);
    return hexToBytes(hex.trim());
  }

  async getTxStatus(txid: string): Promise<TxStatus> {
    interface WocTxInfo {
      blockhash?: string;
      blockheight?: number;
      blocktime?: number;
      confirmations?: number;
    }
    const data = await this.fetchJSON<WocTxInfo>(`/tx/hash/${txid}`);
    return {
      confirmed: (data.confirmations ?? 0) > 0,
      blockHash: data.blockhash ?? "",
      blockHeight: data.blockheight ?? 0,
      txIndex: 0,
    };
  }

  async getBlockHeader(blockHash: string): Promise<Uint8Array> {
    const hex = await this.fetchText(`/block/${blockHash}/header`);
    return hexToBytes(hex.trim());
  }

  async getMerkleProof(txid: string): Promise<MerkleProofData> {
    interface WocMerkle {
      blockHash: string;
      branches: { hash: string; pos: string }[];
      index: number;
    }
    const data = await this.fetchJSON<WocMerkle>(
      `/tx/${txid}/merkleproof`
    );
    return {
      txid,
      blockHash: data.blockHash,
      branches: data.branches.map((b) => hexToBytes(b.hash)),
      index: data.index,
    };
  }

  async getBestBlockHeight(): Promise<number> {
    interface WocChainInfo {
      blocks: number;
    }
    const data = await this.fetchJSON<WocChainInfo>("/chain/info");
    return data.blocks;
  }

  async importAddress(_address: string): Promise<void> {
    // No-op for WoC — addresses are automatically indexed
  }
}
```

**Step 4: Run tests**

```bash
npm test -- test/provider.test.ts
```

Expected: PASS.

**Step 5: Commit**

```bash
git add src/lib/provider.ts test/provider.test.ts
git commit -m "feat: add WhatsOnChain blockchain provider"
```

---

### Task 5: IndexedDB DAG cache

**Files:**
- Create: `src/lib/dag-cache.ts`
- Create: `test/dag-cache.test.ts`

**Step 1: Write tests**

Note: IndexedDB is not available in Node/Vitest by default. Use `fake-indexeddb` or test manually. For vitest, mock with a simple Map-based store.

```typescript
import { describe, it, expect, beforeEach } from "vitest";
import { MemoryDAGCache, type CachedNode } from "@/lib/dag-cache";

describe("DAG Cache", () => {
  let cache: MemoryDAGCache;

  beforeEach(() => {
    cache = new MemoryDAGCache();
  });

  it("should store and retrieve a node", async () => {
    const node: CachedNode = {
      txid: "abc123",
      parentTxid: null,
      name: "/",
      type: "dir",
      children: ["child1"],
      access: 1,
      dataTxid: null,
      lastSeen: Date.now(),
    };
    await cache.put(node);
    const result = await cache.get("abc123");
    expect(result).toEqual(node);
  });

  it("should list children of a directory", async () => {
    await cache.put({
      txid: "root",
      parentTxid: null,
      name: "/",
      type: "dir",
      children: ["child1", "child2"],
      access: 1,
      dataTxid: null,
      lastSeen: Date.now(),
    });
    await cache.put({
      txid: "child1",
      parentTxid: "root",
      name: "hello.txt",
      type: "file",
      children: [],
      access: 1,
      dataTxid: "data1",
      lastSeen: Date.now(),
    });
    await cache.put({
      txid: "child2",
      parentTxid: "root",
      name: "docs",
      type: "dir",
      children: [],
      access: 1,
      dataTxid: null,
      lastSeen: Date.now(),
    });

    const children = await cache.getChildren("root");
    expect(children).toHaveLength(2);
    expect(children.map((c) => c.name).sort()).toEqual(["docs", "hello.txt"]);
  });

  it("should return null for missing nodes", async () => {
    const result = await cache.get("nonexistent");
    expect(result).toBeNull();
  });

  it("should clear all nodes", async () => {
    await cache.put({
      txid: "a",
      parentTxid: null,
      name: "/",
      type: "dir",
      children: [],
      access: 1,
      dataTxid: null,
      lastSeen: Date.now(),
    });
    await cache.clear();
    expect(await cache.get("a")).toBeNull();
  });

  it("should store and retrieve scan state", async () => {
    await cache.setScanHeight(12345);
    expect(await cache.getScanHeight()).toBe(12345);
  });
});
```

**Step 2: Implement DAG cache**

```typescript
// DAG cache — stores reconstructed Metanet DAG nodes
// Uses IndexedDB in Chrome, MemoryDAGCache for testing

export interface CachedNode {
  txid: string;
  parentTxid: string | null;
  name: string;
  type: "dir" | "file" | "link";
  children: string[]; // child txids
  access: number; // AccessLevel
  dataTxid: string | null; // points to DataTx with content
  mimeType?: string;
  fileSize?: number;
  pricePerKB?: number;
  lastSeen: number; // timestamp
}

export interface DAGCache {
  get(txid: string): Promise<CachedNode | null>;
  put(node: CachedNode): Promise<void>;
  getChildren(parentTxid: string): Promise<CachedNode[]>;
  clear(): Promise<void>;
  getScanHeight(): Promise<number>;
  setScanHeight(height: number): Promise<void>;
}

// Memory implementation for testing
export class MemoryDAGCache implements DAGCache {
  private nodes = new Map<string, CachedNode>();
  private scanHeight = 0;

  async get(txid: string): Promise<CachedNode | null> {
    return this.nodes.get(txid) ?? null;
  }

  async put(node: CachedNode): Promise<void> {
    this.nodes.set(node.txid, node);
  }

  async getChildren(parentTxid: string): Promise<CachedNode[]> {
    const parent = this.nodes.get(parentTxid);
    if (!parent) return [];
    return parent.children
      .map((txid) => this.nodes.get(txid))
      .filter((n): n is CachedNode => n !== undefined);
  }

  async clear(): Promise<void> {
    this.nodes.clear();
    this.scanHeight = 0;
  }

  async getScanHeight(): Promise<number> {
    return this.scanHeight;
  }

  async setScanHeight(height: number): Promise<void> {
    this.scanHeight = height;
  }
}

// IndexedDB implementation for Chrome extension
const DB_NAME = "bitfs-dag";
const DB_VERSION = 1;
const NODES_STORE = "nodes";
const META_STORE = "meta";

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(NODES_STORE)) {
        const store = db.createObjectStore(NODES_STORE, { keyPath: "txid" });
        store.createIndex("parentTxid", "parentTxid", { unique: false });
      }
      if (!db.objectStoreNames.contains(META_STORE)) {
        db.createObjectStore(META_STORE);
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

export class IndexedDBDAGCache implements DAGCache {
  private dbPromise: Promise<IDBDatabase>;

  constructor() {
    this.dbPromise = openDB();
  }

  async get(txid: string): Promise<CachedNode | null> {
    const db = await this.dbPromise;
    return new Promise((resolve, reject) => {
      const tx = db.transaction(NODES_STORE, "readonly");
      const req = tx.objectStore(NODES_STORE).get(txid);
      req.onsuccess = () => resolve((req.result as CachedNode) ?? null);
      req.onerror = () => reject(req.error);
    });
  }

  async put(node: CachedNode): Promise<void> {
    const db = await this.dbPromise;
    return new Promise((resolve, reject) => {
      const tx = db.transaction(NODES_STORE, "readwrite");
      tx.objectStore(NODES_STORE).put(node);
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }

  async getChildren(parentTxid: string): Promise<CachedNode[]> {
    const db = await this.dbPromise;
    return new Promise((resolve, reject) => {
      const tx = db.transaction(NODES_STORE, "readonly");
      const index = tx.objectStore(NODES_STORE).index("parentTxid");
      const req = index.getAll(parentTxid);
      req.onsuccess = () => resolve(req.result as CachedNode[]);
      req.onerror = () => reject(req.error);
    });
  }

  async clear(): Promise<void> {
    const db = await this.dbPromise;
    return new Promise((resolve, reject) => {
      const tx = db.transaction([NODES_STORE, META_STORE], "readwrite");
      tx.objectStore(NODES_STORE).clear();
      tx.objectStore(META_STORE).clear();
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }

  async getScanHeight(): Promise<number> {
    const db = await this.dbPromise;
    return new Promise((resolve, reject) => {
      const tx = db.transaction(META_STORE, "readonly");
      const req = tx.objectStore(META_STORE).get("scanHeight");
      req.onsuccess = () => resolve((req.result as number) ?? 0);
      req.onerror = () => reject(req.error);
    });
  }

  async setScanHeight(height: number): Promise<void> {
    const db = await this.dbPromise;
    return new Promise((resolve, reject) => {
      const tx = db.transaction(META_STORE, "readwrite");
      tx.objectStore(META_STORE).put(height, "scanHeight");
      tx.oncomplete = () => resolve();
      tx.onerror = () => reject(tx.error);
    });
  }
}
```

**Step 3: Run tests**

```bash
npm test -- test/dag-cache.test.ts
```

Expected: PASS (using MemoryDAGCache).

**Step 4: Commit**

```bash
git add src/lib/dag-cache.ts test/dag-cache.test.ts
git commit -m "feat: add IndexedDB DAG cache with memory impl for testing"
```

---

## Phase 2: Service Worker

### Task 6: Wallet manager

Handles wallet creation, import, lock/unlock lifecycle. Stores encrypted seed in chrome.storage.local, keeps decrypted HD key in memory.

**Files:**
- Create: `src/background/wallet-manager.ts`
- Create: `test/wallet-manager.test.ts`

**Step 1: Write tests**

```typescript
import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock chrome.storage
const store: Record<string, unknown> = {};
vi.stubGlobal("chrome", {
  storage: {
    local: {
      get: vi.fn((keys: string[]) =>
        Promise.resolve(Object.fromEntries(keys.map((k) => [k, store[k]])))
      ),
      set: vi.fn((items: Record<string, unknown>) => {
        Object.assign(store, items);
        return Promise.resolve();
      }),
      remove: vi.fn((keys: string[]) => {
        for (const k of keys) delete store[k];
        return Promise.resolve();
      }),
    },
    session: {
      get: vi.fn(() => Promise.resolve({})),
      set: vi.fn(() => Promise.resolve()),
      remove: vi.fn(() => Promise.resolve()),
    },
  },
  alarms: {
    create: vi.fn(),
    clear: vi.fn(),
  },
});

import { WalletManager } from "@/background/wallet-manager";

describe("WalletManager", () => {
  let wm: WalletManager;

  beforeEach(() => {
    for (const k of Object.keys(store)) delete store[k];
    wm = new WalletManager();
  });

  it("should report status 'none' initially", async () => {
    const status = await wm.getStatus();
    expect(status.status).toBe("none");
  });

  it("should create wallet and return mnemonic", async () => {
    const result = await wm.create("testpassword123");
    expect(result.mnemonic).toBeTruthy();
    expect(result.mnemonic.split(" ").length).toBe(12);
    expect(result.address).toBeTruthy();
  });

  it("should be unlocked after creation", async () => {
    await wm.create("testpassword123");
    const status = await wm.getStatus();
    expect(status.status).toBe("unlocked");
  });

  it("should lock and unlock", async () => {
    await wm.create("testpassword123");
    await wm.lock();
    expect((await wm.getStatus()).status).toBe("locked");

    await wm.unlock("testpassword123");
    expect((await wm.getStatus()).status).toBe("unlocked");
  });

  it("should reject wrong password on unlock", async () => {
    await wm.create("testpassword123");
    await wm.lock();
    await expect(wm.unlock("wrongpassword")).rejects.toThrow();
  });

  it("should import wallet from mnemonic", async () => {
    const { mnemonic } = await wm.create("pass1");
    const address1 = (await wm.getStatus()).address;

    // Create new manager, import same mnemonic
    for (const k of Object.keys(store)) delete store[k];
    const wm2 = new WalletManager();
    await wm2.import(mnemonic, "pass2");
    const address2 = (await wm2.getStatus()).address;

    expect(address2).toBe(address1);
  });
});
```

**Step 2: Implement wallet manager**

```typescript
import {
  generateMnemonic,
  validateMnemonic,
  seedFromMnemonic,
  encryptSeed,
  decryptSeed,
  Wallet,
} from "@bitfs/libbitfs/wallet";
import { localGet, localSet, localRemove } from "@/lib/storage";
import type { WalletStatus, WalletStatusResult, WalletCreateResult } from "@/lib/messages";

const STORAGE_KEY_ENCRYPTED_SEED = "bitfs:encryptedSeed";
const STORAGE_KEY_ADDRESS = "bitfs:address";

export class WalletManager {
  private wallet: Wallet | null = null;
  private autoLockMinutes = 15;

  async getStatus(): Promise<WalletStatusResult> {
    if (this.wallet) {
      const address = await localGet<string>(STORAGE_KEY_ADDRESS);
      return { status: "unlocked", address: address ?? undefined };
    }
    const encrypted = await localGet<number[]>(STORAGE_KEY_ENCRYPTED_SEED);
    if (encrypted) {
      const address = await localGet<string>(STORAGE_KEY_ADDRESS);
      return { status: "locked", address: address ?? undefined };
    }
    return { status: "none" };
  }

  async create(password: string): Promise<WalletCreateResult> {
    const mnemonic = generateMnemonic();
    const seed = await seedFromMnemonic(mnemonic);
    const encrypted = await encryptSeed(seed, password);

    this.wallet = new Wallet(seed);
    const rootKey = this.wallet.deriveVaultRootKey(0);
    const address = rootKey.publicKey.toAddress().toString();

    await localSet(STORAGE_KEY_ENCRYPTED_SEED, Array.from(encrypted));
    await localSet(STORAGE_KEY_ADDRESS, address);
    this.scheduleAutoLock();

    return { mnemonic, address };
  }

  async import(mnemonic: string, password: string): Promise<string> {
    if (!validateMnemonic(mnemonic)) {
      throw new Error("Invalid mnemonic phrase");
    }
    const seed = await seedFromMnemonic(mnemonic);
    const encrypted = await encryptSeed(seed, password);

    this.wallet = new Wallet(seed);
    const rootKey = this.wallet.deriveVaultRootKey(0);
    const address = rootKey.publicKey.toAddress().toString();

    await localSet(STORAGE_KEY_ENCRYPTED_SEED, Array.from(encrypted));
    await localSet(STORAGE_KEY_ADDRESS, address);
    this.scheduleAutoLock();

    return address;
  }

  async unlock(password: string): Promise<void> {
    const encryptedArr = await localGet<number[]>(STORAGE_KEY_ENCRYPTED_SEED);
    if (!encryptedArr) throw new Error("No wallet found");

    const encrypted = new Uint8Array(encryptedArr);
    const seed = await decryptSeed(encrypted, password);
    this.wallet = new Wallet(seed);
    this.scheduleAutoLock();
  }

  async lock(): Promise<void> {
    this.wallet = null;
  }

  getWallet(): Wallet {
    if (!this.wallet) throw new Error("Wallet is locked");
    return this.wallet;
  }

  setAutoLockMinutes(minutes: number): void {
    this.autoLockMinutes = minutes;
  }

  private scheduleAutoLock(): void {
    if (typeof chrome !== "undefined" && chrome.alarms) {
      chrome.alarms.clear("autoLock");
      chrome.alarms.create("autoLock", {
        delayInMinutes: this.autoLockMinutes,
      });
    }
  }
}
```

**Step 3: Run tests**

```bash
npm test -- test/wallet-manager.test.ts
```

Expected: PASS.

**Step 4: Commit**

```bash
git add src/background/wallet-manager.ts test/wallet-manager.test.ts
git commit -m "feat: implement wallet manager with create/import/lock/unlock"
```

---

### Task 7: DAG manager

Scans blockchain to reconstruct the Metanet DAG from the wallet's root address, stores in DAG cache.

**Files:**
- Create: `src/background/dag-manager.ts`

**Step 1: Implement DAG manager**

```typescript
import type { BlockchainService } from "@bitfs/libbitfs/network";
import { deserializePayload, NodeType } from "@bitfs/libbitfs/metanet";
import type { DAGCache, CachedNode } from "@/lib/dag-cache";
import type { FileEntry } from "@/lib/messages";

const META_FLAG = 0x6d657461;

// Parse raw transaction to find Metanet OP_RETURN data
function parseMetanetTx(rawTx: Uint8Array): {
  parentTxid: Uint8Array | null;
  payload: Uint8Array | null;
} | null {
  // Simple OP_RETURN parser: find OP_RETURN (0x6a) followed by META_FLAG
  // Full parsing would use @bsv/sdk Transaction class
  // For now, scan outputs for OP_RETURN pattern
  try {
    const hex = Array.from(rawTx, (b) => b.toString(16).padStart(2, "0")).join("");
    const opReturnMarker = "6a"; // OP_RETURN
    const metaFlagHex = "6d657461"; // "meta"

    const idx = hex.indexOf(opReturnMarker);
    if (idx === -1) return null;

    // Check for META_FLAG after OP_RETURN
    const afterOpReturn = hex.substring(idx + 2);
    if (!afterOpReturn.includes(metaFlagHex)) return null;

    // Extract payload after META_FLAG
    const payloadStart = afterOpReturn.indexOf(metaFlagHex) + metaFlagHex.length;
    const payloadHex = afterOpReturn.substring(payloadStart);
    if (payloadHex.length < 2) return null;

    const payload = new Uint8Array(payloadHex.length / 2);
    for (let i = 0; i < payloadHex.length; i += 2) {
      payload[i / 2] = parseInt(payloadHex.substring(i, i + 2), 16);
    }

    return { parentTxid: null, payload };
  } catch {
    return null;
  }
}

export class DAGManager {
  constructor(
    private chain: BlockchainService,
    private cache: DAGCache
  ) {}

  async scan(rootAddress: string, force = false): Promise<void> {
    if (force) await this.cache.clear();

    const lastHeight = await this.cache.getScanHeight();
    const currentHeight = await this.chain.getBestBlockHeight();

    // Get all transactions for the root address
    await this.scanAddress(rootAddress, null, "/");

    await this.cache.setScanHeight(currentHeight);
  }

  private async scanAddress(
    address: string,
    parentTxid: string | null,
    name: string
  ): Promise<void> {
    try {
      // Get UTXOs to find transaction history
      // Note: WoC listUnspent only shows unspent, we need full history
      // For MVP, we use UTXOs to find the latest state
      const utxos = await this.chain.listUnspent(address);

      for (const utxo of utxos) {
        const rawTx = await this.chain.getRawTx(utxo.txid);
        const parsed = parseMetanetTx(rawTx);
        if (!parsed?.payload) continue;

        try {
          const node = deserializePayload(parsed.payload);
          const nodeType =
            node.type === NodeType.DIR
              ? "dir"
              : node.type === NodeType.FILE
                ? "file"
                : node.type === NodeType.LINK
                  ? "link"
                  : "file";

          const cached: CachedNode = {
            txid: utxo.txid,
            parentTxid,
            name,
            type: nodeType,
            children: [],
            access: node.access ?? 1,
            dataTxid: null, // TODO: extract from DataTx reference
            mimeType: node.mimeType,
            fileSize: node.fileSize ? Number(node.fileSize) : undefined,
            pricePerKB: node.pricePerKB,
            lastSeen: Date.now(),
          };

          await this.cache.put(cached);
        } catch {
          // Skip unparseable transactions
        }
      }
    } catch {
      // Address may have no transactions
    }
  }

  async listFiles(path: string): Promise<FileEntry[]> {
    // Resolve path to find the directory node
    // For root, get the root node from cache
    if (path === "/" || path === "") {
      // Find root node (parentTxid === null)
      // For MVP, return children of first root-like node
      // TODO: proper path resolution using metanet resolvePath
    }

    return [];
  }

  async getNode(txid: string): Promise<CachedNode | null> {
    return this.cache.get(txid);
  }
}
```

**Step 2: Commit**

```bash
git add src/background/dag-manager.ts
git commit -m "feat: add DAG manager for blockchain scanning and DAG reconstruction"
```

---

### Task 8: File reader

Fetches and decrypts file content from blockchain transactions.

**Files:**
- Create: `src/background/file-reader.ts`

**Step 1: Implement file reader**

```typescript
import type { BlockchainService } from "@bitfs/libbitfs/network";
import { decrypt, deriveAESKey, ecdh } from "@bitfs/libbitfs/method42";
import type { Wallet } from "@bitfs/libbitfs/wallet";
import type { DAGCache } from "@/lib/dag-cache";
import type { ReadFileResult } from "@/lib/messages";

function hexToBytes(hex: string): Uint8Array {
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < hex.length; i += 2) {
    bytes[i / 2] = parseInt(hex.substring(i, i + 2), 16);
  }
  return bytes;
}

export class FileReader {
  constructor(
    private chain: BlockchainService,
    private cache: DAGCache,
    private wallet: Wallet
  ) {}

  async readFile(txid: string): Promise<ReadFileResult> {
    const node = await this.cache.get(txid);
    if (!node) throw new Error("File not found in cache");

    // Fetch the DataTx that contains the actual content
    const dataTxid = node.dataTxid ?? txid;
    const rawTx = await this.chain.getRawTx(dataTxid);

    // Extract content from OP_RETURN
    const content = this.extractOPReturnData(rawTx);
    if (!content) throw new Error("No content found in transaction");

    // Check access level
    const access = node.access ?? 1; // default FREE

    if (access === 0) {
      // PRIVATE — decrypt with owner's key
      // Derive node key from wallet
      const nodeKey = this.wallet.deriveVaultRootKey(0);
      const sharedSecret = ecdh(nodeKey.privateKey, nodeKey.publicKey);

      // Need keyHash from the cached node metadata
      // For MVP: assume content is already available
      // Full implementation would need keyHash from TLV metadata
      return {
        content,
        mimeType: node.mimeType,
        size: content.length,
        access,
      };
    }

    // FREE or content already decrypted
    return {
      content,
      mimeType: node.mimeType,
      size: content.length,
      access,
    };
  }

  private extractOPReturnData(rawTx: Uint8Array): Uint8Array | null {
    // Parse transaction outputs to find OP_RETURN data
    // OP_RETURN = 0x6a, followed by pushdata
    const hex = Array.from(rawTx, (b) => b.toString(16).padStart(2, "0")).join("");

    // Find OP_RETURN output (simplified parser)
    const opReturnIdx = hex.indexOf("6a");
    if (opReturnIdx === -1) return null;

    // Skip OP_RETURN byte and extract the rest as data
    // This is simplified — a full implementation would properly parse
    // the Bitcoin script pushdata opcodes
    const dataHex = hex.substring(opReturnIdx + 2);
    if (dataHex.length < 2) return null;

    return hexToBytes(dataHex);
  }
}
```

**Step 2: Commit**

```bash
git add src/background/file-reader.ts
git commit -m "feat: add file reader for fetching and decrypting content from blockchain"
```

---

### Task 9: Service worker message router

Wires up all the managers and handles chrome.runtime.onMessage.

**Files:**
- Modify: `src/background/service-worker.ts`

**Step 1: Implement service worker**

Replace `src/background/service-worker.ts`:

```typescript
import { WalletManager } from "./wallet-manager";
import { DAGManager } from "./dag-manager";
import { WhatsOnChainProvider } from "@/lib/provider";
import { IndexedDBDAGCache } from "@/lib/dag-cache";
import { localGet } from "@/lib/storage";
import type {
  Message,
  Response,
  WalletCreatePayload,
  WalletImportPayload,
  WalletUnlockPayload,
  ScanDAGPayload,
  ExtensionSettings,
} from "@/lib/messages";

const walletManager = new WalletManager();
let dagManager: DAGManager | null = null;
let provider: WhatsOnChainProvider | null = null;

function getProvider(network = "main"): WhatsOnChainProvider {
  if (!provider) provider = new WhatsOnChainProvider(network);
  return provider;
}

function getDAGManager(): DAGManager {
  if (!dagManager) {
    dagManager = new DAGManager(getProvider(), new IndexedDBDAGCache());
  }
  return dagManager;
}

function ok<T>(data: T): Response<T> {
  return { ok: true, data };
}

function err(message: string): Response {
  return { ok: false, error: message };
}

async function handleMessage(msg: Message): Promise<Response> {
  try {
    switch (msg.type) {
      case "WALLET_CREATE": {
        const { password } = msg.payload as WalletCreatePayload;
        const result = await walletManager.create(password);
        return ok(result);
      }

      case "WALLET_IMPORT": {
        const { mnemonic, password } = msg.payload as WalletImportPayload;
        const address = await walletManager.import(mnemonic, password);
        return ok({ address });
      }

      case "WALLET_UNLOCK": {
        const { password } = msg.payload as WalletUnlockPayload;
        await walletManager.unlock(password);
        return ok({ success: true });
      }

      case "WALLET_LOCK":
        await walletManager.lock();
        return ok({ success: true });

      case "GET_STATUS":
        return ok(await walletManager.getStatus());

      case "GET_ADDRESS": {
        const status = await walletManager.getStatus();
        return ok({ address: status.address });
      }

      case "GET_BALANCE": {
        const status = await walletManager.getStatus();
        if (!status.address) return err("No wallet");
        const utxos = await getProvider().listUnspent(status.address);
        const confirmed = utxos.reduce((sum, u) => sum + u.amount, BigInt(0));
        return ok({ confirmed: confirmed.toString(), unconfirmed: "0" });
      }

      case "SCAN_DAG": {
        const { force } = (msg.payload as ScanDAGPayload) ?? {};
        const status = await walletManager.getStatus();
        if (!status.address) return err("No wallet");
        await getDAGManager().scan(status.address, force);
        return ok({ success: true });
      }

      case "LIST_FILES": {
        const { path } = msg.payload as { path: string };
        const files = await getDAGManager().listFiles(path);
        return ok(files);
      }

      case "BROADCAST_TX": {
        const { rawTxHex } = msg.payload as { rawTxHex: string };
        const txid = await getProvider().broadcastTx(rawTxHex);
        return ok({ txid });
      }

      case "GET_SETTINGS": {
        const settings = await localGet<ExtensionSettings>("bitfs:settings");
        return ok(
          settings ?? {
            apiProviderUrl: "https://api.whatsonchain.com",
            network: "mainnet",
            autoLockMinutes: 15,
          }
        );
      }

      case "SET_SETTINGS": {
        const settings = msg.payload as ExtensionSettings;
        const { localSet } = await import("@/lib/storage");
        await localSet("bitfs:settings", settings);
        // Recreate provider with new network
        const net = settings.network === "testnet" ? "test" : "main";
        provider = new WhatsOnChainProvider(net);
        dagManager = null; // force recreate
        walletManager.setAutoLockMinutes(settings.autoLockMinutes);
        return ok({ success: true });
      }

      default:
        return err(`Unknown message type: ${msg.type}`);
    }
  } catch (e) {
    return err(e instanceof Error ? e.message : String(e));
  }
}

// Message listener
chrome.runtime.onMessage.addListener((message, _sender, sendResponse) => {
  handleMessage(message as Message).then(sendResponse);
  return true; // async response
});

// Auto-lock alarm
chrome.alarms?.onAlarm?.addListener((alarm) => {
  if (alarm.name === "autoLock") {
    walletManager.lock();
  }
});

// Install handler
chrome.runtime.onInstalled.addListener(() => {
  console.log("BitFS extension installed");
});
```

**Step 2: Verify build**

```bash
npm run build
```

Expected: Build succeeds.

**Step 3: Commit**

```bash
git add src/background/service-worker.ts
git commit -m "feat: implement service worker message router with all handlers"
```

---

## Phase 3: Popup UI

### Task 10: Shared components and contexts

**Files:**
- Create: `src/popup/contexts/WalletContext.tsx`
- Create: `src/popup/contexts/SettingsContext.tsx`
- Create: `src/popup/components/Header.tsx`
- Create: `src/popup/components/Button.tsx`
- Create: `src/popup/components/StatusBadge.tsx`

**Step 1: Create WalletContext**

```typescript
import { createContext, useContext, useState, useEffect, useCallback, type ReactNode } from "react";
import { sendMessage, type WalletStatus, type WalletStatusResult } from "@/lib/messages";

interface WalletContextType {
  status: WalletStatus;
  address: string | null;
  balance: string | null;
  loading: boolean;
  refresh: () => Promise<void>;
  create: (password: string) => Promise<{ mnemonic: string; address: string }>;
  importWallet: (mnemonic: string, password: string) => Promise<void>;
  unlock: (password: string) => Promise<void>;
  lock: () => Promise<void>;
}

const WalletContext = createContext<WalletContextType | null>(null);

export function WalletProvider({ children }: { children: ReactNode }) {
  const [status, setStatus] = useState<WalletStatus>("none");
  const [address, setAddress] = useState<string | null>(null);
  const [balance, setBalance] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      const result = await sendMessage<WalletStatusResult>({ type: "GET_STATUS" });
      setStatus(result.status);
      setAddress(result.address ?? null);

      if (result.status === "unlocked" && result.address) {
        try {
          const bal = await sendMessage<{ confirmed: string }>({ type: "GET_BALANCE" });
          setBalance(bal.confirmed);
        } catch {
          setBalance(null);
        }
      }
    } catch {
      setStatus("none");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { refresh(); }, [refresh]);

  const create = async (password: string) => {
    const result = await sendMessage<{ mnemonic: string; address: string }>({
      type: "WALLET_CREATE",
      payload: { password },
    });
    await refresh();
    return result;
  };

  const importWallet = async (mnemonic: string, password: string) => {
    await sendMessage({ type: "WALLET_IMPORT", payload: { mnemonic, password } });
    await refresh();
  };

  const unlock = async (password: string) => {
    await sendMessage({ type: "WALLET_UNLOCK", payload: { password } });
    await refresh();
  };

  const lock = async () => {
    await sendMessage({ type: "WALLET_LOCK" });
    await refresh();
  };

  return (
    <WalletContext.Provider
      value={{ status, address, balance, loading, refresh, create, importWallet, unlock, lock }}
    >
      {children}
    </WalletContext.Provider>
  );
}

export function useWallet() {
  const ctx = useContext(WalletContext);
  if (!ctx) throw new Error("useWallet must be used within WalletProvider");
  return ctx;
}
```

**Step 2: Create SettingsContext**

```typescript
import { createContext, useContext, useState, useEffect, type ReactNode } from "react";
import { sendMessage, type ExtensionSettings } from "@/lib/messages";

const defaults: ExtensionSettings = {
  apiProviderUrl: "https://api.whatsonchain.com",
  network: "mainnet",
  autoLockMinutes: 15,
};

interface SettingsContextType {
  settings: ExtensionSettings;
  updateSettings: (s: Partial<ExtensionSettings>) => Promise<void>;
}

const SettingsContext = createContext<SettingsContextType | null>(null);

export function SettingsProvider({ children }: { children: ReactNode }) {
  const [settings, setSettings] = useState<ExtensionSettings>(defaults);

  useEffect(() => {
    sendMessage<ExtensionSettings>({ type: "GET_SETTINGS" })
      .then(setSettings)
      .catch(() => {});
  }, []);

  const updateSettings = async (partial: Partial<ExtensionSettings>) => {
    const updated = { ...settings, ...partial };
    await sendMessage({ type: "SET_SETTINGS", payload: updated });
    setSettings(updated);
  };

  return (
    <SettingsContext.Provider value={{ settings, updateSettings }}>
      {children}
    </SettingsContext.Provider>
  );
}

export function useSettings() {
  const ctx = useContext(SettingsContext);
  if (!ctx) throw new Error("useSettings must be used within SettingsProvider");
  return ctx;
}
```

**Step 3: Create shared components**

`src/popup/components/Header.tsx`:

```typescript
import { useWallet } from "@/popup/contexts/WalletContext";

export function Header() {
  const { status, lock } = useWallet();

  return (
    <header className="flex items-center justify-between px-4 py-3 border-b border-border bg-bg-secondary">
      <div className="flex items-center gap-2">
        <span className="text-accent font-bold text-lg">BitFS</span>
        <span className="text-[10px] text-text-secondary bg-bg-tertiary px-1.5 py-0.5 rounded">
          v0.1.0
        </span>
      </div>
      {status === "unlocked" && (
        <button
          onClick={lock}
          className="text-xs text-text-secondary hover:text-text transition-colors"
        >
          Lock
        </button>
      )}
    </header>
  );
}
```

`src/popup/components/Button.tsx`:

```typescript
import type { ButtonHTMLAttributes } from "react";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "ghost";
  size?: "sm" | "md";
}

export function Button({
  variant = "primary",
  size = "md",
  className = "",
  ...props
}: ButtonProps) {
  const base = "rounded font-medium transition-colors disabled:opacity-50 disabled:cursor-not-allowed";
  const variants = {
    primary: "bg-accent hover:bg-accent-hover text-bg",
    secondary: "bg-bg-tertiary hover:bg-border text-text",
    ghost: "hover:bg-bg-tertiary text-text-secondary hover:text-text",
  };
  const sizes = {
    sm: "px-3 py-1.5 text-xs",
    md: "px-4 py-2 text-sm",
  };

  return (
    <button
      className={`${base} ${variants[variant]} ${sizes[size]} ${className}`}
      {...props}
    />
  );
}
```

`src/popup/components/StatusBadge.tsx`:

```typescript
import type { WalletStatus } from "@/lib/messages";

const config: Record<WalletStatus, { label: string; color: string }> = {
  none: { label: "No Wallet", color: "text-text-secondary" },
  locked: { label: "Locked", color: "text-warning" },
  unlocked: { label: "Connected", color: "text-success" },
};

export function StatusBadge({ status }: { status: WalletStatus }) {
  const { label, color } = config[status];
  return (
    <span className={`text-xs font-medium ${color}`}>
      ● {label}
    </span>
  );
}
```

**Step 4: Commit**

```bash
git add src/popup/contexts/ src/popup/components/
git commit -m "feat: add wallet/settings contexts and shared UI components"
```

---

### Task 11: Wallet pages (Create, Import, Unlock)

**Files:**
- Create: `src/popup/pages/CreateWallet.tsx`
- Create: `src/popup/pages/ImportWallet.tsx`
- Create: `src/popup/pages/Unlock.tsx`

**Step 1: Create wallet page**

`src/popup/pages/CreateWallet.tsx`:

```typescript
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWallet } from "@/popup/contexts/WalletContext";
import { Button } from "@/popup/components/Button";

type Step = "password" | "mnemonic" | "confirm";

export function CreateWallet() {
  const navigate = useNavigate();
  const { create } = useWallet();
  const [step, setStep] = useState<Step>("password");
  const [password, setPassword] = useState("");
  const [confirmPw, setConfirmPw] = useState("");
  const [mnemonic, setMnemonic] = useState("");
  const [confirmWords, setConfirmWords] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleCreate = async () => {
    if (password.length < 8) {
      setError("Password must be at least 8 characters");
      return;
    }
    if (password !== confirmPw) {
      setError("Passwords do not match");
      return;
    }
    setError("");
    setLoading(true);
    try {
      const result = await create(password);
      setMnemonic(result.mnemonic);
      setStep("mnemonic");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to create wallet");
    } finally {
      setLoading(false);
    }
  };

  const handleConfirm = () => {
    if (confirmWords.trim().toLowerCase() !== mnemonic.trim().toLowerCase()) {
      setError("Mnemonic does not match. Please try again.");
      return;
    }
    navigate("/");
  };

  if (step === "mnemonic") {
    return (
      <div className="p-4 flex flex-col gap-4">
        <h2 className="text-lg font-semibold">Backup Your Seed Phrase</h2>
        <p className="text-xs text-text-secondary">
          Write down these 12 words in order. You will need them to recover your wallet.
        </p>
        <div className="bg-bg-tertiary rounded-lg p-3 font-mono text-sm leading-relaxed select-all">
          {mnemonic}
        </div>
        <Button onClick={() => setStep("confirm")}>I've written it down</Button>
      </div>
    );
  }

  if (step === "confirm") {
    return (
      <div className="p-4 flex flex-col gap-4">
        <h2 className="text-lg font-semibold">Confirm Seed Phrase</h2>
        <p className="text-xs text-text-secondary">
          Enter your 12-word seed phrase to confirm you've saved it.
        </p>
        <textarea
          value={confirmWords}
          onChange={(e) => setConfirmWords(e.target.value)}
          placeholder="Enter your seed phrase..."
          rows={3}
          className="w-full bg-bg-tertiary border border-border rounded-lg p-3 text-sm font-mono text-text resize-none focus:outline-none focus:border-accent"
        />
        {error && <p className="text-error text-xs">{error}</p>}
        <Button onClick={handleConfirm}>Confirm</Button>
      </div>
    );
  }

  return (
    <div className="p-4 flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Create Wallet</h2>
      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">Password</label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Min 8 characters"
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-accent"
        />
      </div>
      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">Confirm Password</label>
        <input
          type="password"
          value={confirmPw}
          onChange={(e) => setConfirmPw(e.target.value)}
          placeholder="Repeat password"
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-accent"
        />
      </div>
      {error && <p className="text-error text-xs">{error}</p>}
      <Button onClick={handleCreate} disabled={loading}>
        {loading ? "Creating..." : "Create Wallet"}
      </Button>
      <button
        onClick={() => navigate("/import")}
        className="text-xs text-accent hover:text-accent-hover text-center"
      >
        Import existing wallet
      </button>
    </div>
  );
}
```

`src/popup/pages/ImportWallet.tsx`:

```typescript
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { useWallet } from "@/popup/contexts/WalletContext";
import { Button } from "@/popup/components/Button";

export function ImportWallet() {
  const navigate = useNavigate();
  const { importWallet } = useWallet();
  const [mnemonic, setMnemonic] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPw, setConfirmPw] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleImport = async () => {
    const words = mnemonic.trim().split(/\s+/);
    if (words.length !== 12 && words.length !== 24) {
      setError("Seed phrase must be 12 or 24 words");
      return;
    }
    if (password.length < 8) {
      setError("Password must be at least 8 characters");
      return;
    }
    if (password !== confirmPw) {
      setError("Passwords do not match");
      return;
    }
    setError("");
    setLoading(true);
    try {
      await importWallet(mnemonic.trim(), password);
      navigate("/");
    } catch (e) {
      setError(e instanceof Error ? e.message : "Import failed");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="p-4 flex flex-col gap-4">
      <h2 className="text-lg font-semibold">Import Wallet</h2>
      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">Seed Phrase</label>
        <textarea
          value={mnemonic}
          onChange={(e) => setMnemonic(e.target.value)}
          placeholder="Enter your 12 or 24 word seed phrase..."
          rows={3}
          className="w-full bg-bg-tertiary border border-border rounded-lg p-3 text-sm font-mono text-text resize-none focus:outline-none focus:border-accent"
        />
      </div>
      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">New Password</label>
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          placeholder="Min 8 characters"
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-accent"
        />
      </div>
      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">Confirm Password</label>
        <input
          type="password"
          value={confirmPw}
          onChange={(e) => setConfirmPw(e.target.value)}
          placeholder="Repeat password"
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-accent"
        />
      </div>
      {error && <p className="text-error text-xs">{error}</p>}
      <Button onClick={handleImport} disabled={loading}>
        {loading ? "Importing..." : "Import Wallet"}
      </Button>
      <button
        onClick={() => navigate("/create")}
        className="text-xs text-accent hover:text-accent-hover text-center"
      >
        Create new wallet instead
      </button>
    </div>
  );
}
```

`src/popup/pages/Unlock.tsx`:

```typescript
import { useState } from "react";
import { useWallet } from "@/popup/contexts/WalletContext";
import { Button } from "@/popup/components/Button";

export function Unlock() {
  const { unlock, address } = useWallet();
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  const handleUnlock = async () => {
    setError("");
    setLoading(true);
    try {
      await unlock(password);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Wrong password");
    } finally {
      setLoading(false);
    }
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === "Enter") handleUnlock();
  };

  return (
    <div className="p-4 flex flex-col items-center justify-center gap-6 h-full">
      <div className="text-center">
        <div className="text-4xl mb-2">🔒</div>
        <h2 className="text-lg font-semibold">BitFS Locked</h2>
        {address && (
          <p className="text-xs text-text-secondary font-mono mt-1 truncate max-w-[280px]">
            {address}
          </p>
        )}
      </div>
      <div className="w-full flex flex-col gap-3">
        <input
          type="password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Enter password"
          autoFocus
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text text-center focus:outline-none focus:border-accent"
        />
        {error && <p className="text-error text-xs text-center">{error}</p>}
        <Button onClick={handleUnlock} disabled={loading} className="w-full">
          {loading ? "Unlocking..." : "Unlock"}
        </Button>
      </div>
    </div>
  );
}
```

**Step 2: Commit**

```bash
git add src/popup/pages/CreateWallet.tsx src/popup/pages/ImportWallet.tsx src/popup/pages/Unlock.tsx
git commit -m "feat: add Create, Import, and Unlock wallet pages"
```

---

### Task 12: Home page and Settings page

**Files:**
- Modify: `src/popup/pages/Home.tsx`
- Modify: `src/popup/pages/Settings.tsx`

**Step 1: Rewrite Home page**

```typescript
import { useNavigate } from "react-router-dom";
import { useWallet } from "@/popup/contexts/WalletContext";
import { StatusBadge } from "@/popup/components/StatusBadge";
import { Button } from "@/popup/components/Button";

export function Home() {
  const navigate = useNavigate();
  const { status, address, balance } = useWallet();

  if (status !== "unlocked") return null; // handled by App router

  const displayBalance = balance
    ? `${(Number(balance) / 1e8).toFixed(8)} BSV`
    : "Loading...";

  const shortAddr = address
    ? `${address.slice(0, 8)}...${address.slice(-6)}`
    : "";

  return (
    <div className="p-4 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <StatusBadge status={status} />
        <button
          onClick={() => navigate("/settings")}
          className="text-text-secondary hover:text-text text-sm"
        >
          ⚙
        </button>
      </div>

      <div className="bg-bg-secondary rounded-xl p-4 text-center">
        <p className="text-xs text-text-secondary font-mono">{shortAddr}</p>
        <p className="text-2xl font-bold mt-1 text-accent">{displayBalance}</p>
      </div>

      <div className="grid grid-cols-2 gap-2">
        <Button onClick={() => navigate("/files")} variant="secondary">
          📁 Files
        </Button>
        <Button onClick={() => navigate("/send")} variant="secondary">
          📤 Send
        </Button>
      </div>

      <button
        onClick={() => {
          if (address) {
            navigator.clipboard.writeText(address);
          }
        }}
        className="text-xs text-text-secondary hover:text-accent text-center"
      >
        Copy full address
      </button>
    </div>
  );
}
```

**Step 2: Rewrite Settings page**

```typescript
import { useNavigate } from "react-router-dom";
import { useSettings } from "@/popup/contexts/SettingsContext";
import { Button } from "@/popup/components/Button";

export function Settings() {
  const navigate = useNavigate();
  const { settings, updateSettings } = useSettings();

  return (
    <div className="p-4 flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold">Settings</h2>
        <button
          onClick={() => navigate(-1)}
          className="text-text-secondary hover:text-text text-sm"
        >
          ✕
        </button>
      </div>

      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">Network</label>
        <select
          value={settings.network}
          onChange={(e) =>
            updateSettings({ network: e.target.value as "mainnet" | "testnet" })
          }
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-accent"
        >
          <option value="mainnet">Mainnet</option>
          <option value="testnet">Testnet</option>
        </select>
      </div>

      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">Auto-lock (minutes)</label>
        <input
          type="number"
          min={1}
          max={60}
          value={settings.autoLockMinutes}
          onChange={(e) =>
            updateSettings({ autoLockMinutes: parseInt(e.target.value) || 15 })
          }
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text focus:outline-none focus:border-accent"
        />
      </div>

      <div className="flex flex-col gap-2">
        <label className="text-xs text-text-secondary">API Provider URL</label>
        <input
          type="url"
          value={settings.apiProviderUrl}
          onChange={(e) => updateSettings({ apiProviderUrl: e.target.value })}
          className="w-full bg-bg-tertiary border border-border rounded-lg px-3 py-2 text-sm text-text font-mono text-xs focus:outline-none focus:border-accent"
        />
      </div>

      <div className="mt-2 pt-4 border-t border-border">
        <p className="text-[10px] text-text-secondary text-center">
          BitFS Extension v0.1.0 · OpenBSV License
        </p>
      </div>
    </div>
  );
}
```

**Step 3: Commit**

```bash
git add src/popup/pages/Home.tsx src/popup/pages/Settings.tsx
git commit -m "feat: rewrite Home and Settings pages with Tailwind dark theme"
```

---

### Task 13: File browser and file detail pages

**Files:**
- Create: `src/popup/pages/Files.tsx`
- Create: `src/popup/pages/FileDetail.tsx`

**Step 1: Files browser**

```typescript
import { useState, useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { sendMessage, type FileEntry } from "@/lib/messages";
import { Button } from "@/popup/components/Button";

export function Files() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const currentPath = searchParams.get("path") ?? "/";
  const [files, setFiles] = useState<FileEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [scanning, setScanning] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    loadFiles();
  }, [currentPath]);

  const loadFiles = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await sendMessage<FileEntry[]>({
        type: "LIST_FILES",
        payload: { path: currentPath },
      });
      setFiles(result);
    } catch (e) {
      setError(e instanceof Error ? e.message : "Failed to load files");
    } finally {
      setLoading(false);
    }
  };

  const handleScan = async () => {
    setScanning(true);
    try {
      await sendMessage({ type: "SCAN_DAG", payload: { force: false } });
      await loadFiles();
    } catch (e) {
      setError(e instanceof Error ? e.message : "Scan failed");
    } finally {
      setScanning(false);
    }
  };

  const iconFor = (type: string) =>
    type === "dir" ? "📁" : type === "link" ? "🔗" : "📄";

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center justify-between px-4 py-2 border-b border-border">
        <div className="flex items-center gap-2">
          <button onClick={() => navigate("/")} className="text-text-secondary hover:text-text">
            ←
          </button>
          <span className="text-sm font-mono text-text-secondary truncate max-w-[200px]">
            {currentPath}
          </span>
        </div>
        <Button size="sm" variant="ghost" onClick={handleScan} disabled={scanning}>
          {scanning ? "⟳" : "↻"} Sync
        </Button>
      </div>

      <div className="flex-1 overflow-y-auto">
        {loading ? (
          <div className="p-4 text-center text-text-secondary text-sm">Loading...</div>
        ) : error ? (
          <div className="p-4 text-center text-error text-sm">{error}</div>
        ) : files.length === 0 ? (
          <div className="p-4 text-center text-text-secondary text-sm">
            <p>No files found.</p>
            <Button size="sm" variant="secondary" onClick={handleScan} className="mt-2">
              Scan Blockchain
            </Button>
          </div>
        ) : (
          <ul>
            {files.map((f) => (
              <li key={f.name}>
                <button
                  onClick={() => {
                    const newPath = currentPath === "/" ? `/${f.name}` : `${currentPath}/${f.name}`;
                    if (f.type === "dir") {
                      navigate(`/files?path=${encodeURIComponent(newPath)}`);
                    } else {
                      navigate(`/file-detail?txid=${f.txid}&name=${encodeURIComponent(f.name)}`);
                    }
                  }}
                  className="w-full flex items-center gap-3 px-4 py-2.5 hover:bg-bg-secondary transition-colors text-left"
                >
                  <span>{iconFor(f.type)}</span>
                  <div className="flex-1 min-w-0">
                    <p className="text-sm truncate">{f.name}</p>
                    {f.size !== undefined && (
                      <p className="text-[10px] text-text-secondary">
                        {f.size > 1024 ? `${(f.size / 1024).toFixed(1)} KB` : `${f.size} B`}
                      </p>
                    )}
                  </div>
                  {f.access === 2 && (
                    <span className="text-[10px] text-warning bg-warning/10 px-1.5 py-0.5 rounded">
                      Paid
                    </span>
                  )}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
```

**Step 2: File detail**

```typescript
import { useState, useEffect } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { sendMessage, type ReadFileResult } from "@/lib/messages";
import { Button } from "@/popup/components/Button";

export function FileDetail() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const txid = searchParams.get("txid") ?? "";
  const name = searchParams.get("name") ?? "Unknown";
  const [file, setFile] = useState<ReadFileResult | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!txid) return;
    setLoading(true);
    sendMessage<ReadFileResult>({
      type: "READ_FILE",
      payload: { path: txid },
    })
      .then(setFile)
      .catch((e) => setError(e instanceof Error ? e.message : "Failed"))
      .finally(() => setLoading(false));
  }, [txid]);

  const accessLabel = ["Private", "Free", "Paid"][file?.access ?? 1];

  const textContent =
    file && file.mimeType?.startsWith("text/")
      ? new TextDecoder().decode(file.content)
      : null;

  return (
    <div className="flex flex-col h-full">
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border">
        <button onClick={() => navigate(-1)} className="text-text-secondary hover:text-text">
          ←
        </button>
        <span className="text-sm font-medium truncate">{name}</span>
      </div>

      <div className="flex-1 overflow-y-auto p-4">
        {loading ? (
          <p className="text-text-secondary text-sm">Loading...</p>
        ) : error ? (
          <p className="text-error text-sm">{error}</p>
        ) : file ? (
          <div className="flex flex-col gap-3">
            <div className="grid grid-cols-2 gap-2 text-xs">
              <div>
                <p className="text-text-secondary">Size</p>
                <p>{file.size} bytes</p>
              </div>
              <div>
                <p className="text-text-secondary">Access</p>
                <p>{accessLabel}</p>
              </div>
              <div>
                <p className="text-text-secondary">MIME</p>
                <p className="font-mono">{file.mimeType ?? "unknown"}</p>
              </div>
              <div>
                <p className="text-text-secondary">TxID</p>
                <p className="font-mono truncate">{txid.slice(0, 12)}...</p>
              </div>
            </div>

            {textContent ? (
              <pre className="bg-bg-tertiary rounded-lg p-3 text-xs font-mono overflow-x-auto whitespace-pre-wrap max-h-[200px]">
                {textContent}
              </pre>
            ) : (
              <div className="bg-bg-tertiary rounded-lg p-4 text-center text-text-secondary text-sm">
                Binary content ({file.size} bytes)
              </div>
            )}

            <Button
              variant="secondary"
              size="sm"
              onClick={() => {
                if (file.content) {
                  const blob = new Blob([file.content], { type: file.mimeType ?? "application/octet-stream" });
                  const url = URL.createObjectURL(blob);
                  const a = document.createElement("a");
                  a.href = url;
                  a.download = name;
                  a.click();
                  URL.revokeObjectURL(url);
                }
              }}
            >
              Download
            </Button>
          </div>
        ) : null}
      </div>
    </div>
  );
}
```

**Step 3: Commit**

```bash
git add src/popup/pages/Files.tsx src/popup/pages/FileDetail.tsx
git commit -m "feat: add file browser and file detail pages"
```

---

### Task 14: App routing

Wire up all pages with proper auth-gated routing.

**Files:**
- Modify: `src/popup/App.tsx`
- Delete: `src/popup/pages/Wallet.tsx` (replaced by Create/Import)

**Step 1: Rewrite App.tsx**

```typescript
import { MemoryRouter, Routes, Route, Navigate } from "react-router-dom";
import { WalletProvider, useWallet } from "@/popup/contexts/WalletContext";
import { SettingsProvider } from "@/popup/contexts/SettingsContext";
import { Header } from "@/popup/components/Header";
import { Home } from "./pages/Home";
import { CreateWallet } from "./pages/CreateWallet";
import { ImportWallet } from "./pages/ImportWallet";
import { Unlock } from "./pages/Unlock";
import { Files } from "./pages/Files";
import { FileDetail } from "./pages/FileDetail";
import { Settings } from "./pages/Settings";

function AppRoutes() {
  const { status, loading } = useWallet();

  if (loading) {
    return (
      <div className="flex items-center justify-center h-full">
        <div className="text-accent text-lg">BitFS</div>
      </div>
    );
  }

  // No wallet → create/import
  if (status === "none") {
    return (
      <Routes>
        <Route path="/create" element={<CreateWallet />} />
        <Route path="/import" element={<ImportWallet />} />
        <Route path="*" element={<Navigate to="/create" replace />} />
      </Routes>
    );
  }

  // Locked → unlock
  if (status === "locked") {
    return (
      <Routes>
        <Route path="*" element={<Unlock />} />
      </Routes>
    );
  }

  // Unlocked → full app
  return (
    <>
      <Header />
      <div className="flex-1 overflow-y-auto">
        <Routes>
          <Route path="/" element={<Home />} />
          <Route path="/files" element={<Files />} />
          <Route path="/file-detail" element={<FileDetail />} />
          <Route path="/settings" element={<Settings />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </div>
    </>
  );
}

export function App() {
  return (
    <MemoryRouter>
      <WalletProvider>
        <SettingsProvider>
          <div className="flex flex-col h-full bg-bg text-text">
            <AppRoutes />
          </div>
        </SettingsProvider>
      </WalletProvider>
    </MemoryRouter>
  );
}
```

**Step 2: Delete old Wallet.tsx**

```bash
rm src/popup/pages/Wallet.tsx
```

**Step 3: Build and verify**

```bash
npm run build
```

Expected: Build succeeds.

**Step 4: Commit**

```bash
git add -A
git commit -m "feat: implement auth-gated routing with wallet state guards"
```

---

## Phase 4: Content Script

### Task 15: Link detection and payment handler

**Files:**
- Modify: `src/content-script/index.ts`

**Step 1: Implement content script**

```typescript
// BitFS Content Script
// 1. Detects bitfs:// links in page and makes them clickable
// 2. Intercepts HTTP 402 responses for payment flow
// 3. Injects window.bitfs API for DApp integration

const BITFS_URI_PATTERN = /bitfs:\/\/([a-zA-Z0-9]+)(\/[^\s<>"')\]]*)?/g;

// --- Link Detection ---

function scanForBitFSLinks(): void {
  const walker = document.createTreeWalker(
    document.body,
    NodeFilter.SHOW_TEXT,
    null
  );

  const matches: { node: Text; match: RegExpMatchArray }[] = [];
  let node: Text | null;
  while ((node = walker.nextNode() as Text | null)) {
    const text = node.textContent ?? "";
    for (const match of text.matchAll(BITFS_URI_PATTERN)) {
      matches.push({ node, match });
    }
  }

  // Process in reverse to avoid invalidating offsets
  for (const { node, match } of matches.reverse()) {
    if (!match.index && match.index !== 0) continue;
    const parent = node.parentNode;
    if (!parent) continue;

    // Don't process if already inside a link
    if (node.parentElement?.closest("a, [data-bitfs-link]")) continue;

    const before = node.textContent!.substring(0, match.index);
    const uri = match[0];
    const after = node.textContent!.substring(match.index + uri.length);

    const link = document.createElement("a");
    link.href = "#";
    link.textContent = uri;
    link.setAttribute("data-bitfs-link", uri);
    link.style.cssText =
      "color:#c9956b;text-decoration:underline;cursor:pointer;";
    link.addEventListener("click", (e) => {
      e.preventDefault();
      chrome.runtime.sendMessage({
        type: "READ_FILE",
        payload: { path: uri },
      });
    });

    const frag = document.createDocumentFragment();
    if (before) frag.appendChild(document.createTextNode(before));
    frag.appendChild(link);
    if (after) frag.appendChild(document.createTextNode(after));

    parent.replaceChild(frag, node);
  }
}

// --- DApp API Injection ---

function injectBitFSAPI(): void {
  const script = document.createElement("script");
  script.textContent = `
    window.bitfs = {
      isInstalled: true,
      version: "0.1.0",
      requestAccounts: function() {
        return new Promise(function(resolve, reject) {
          window.postMessage({ type: "BITFS_REQUEST_ACCOUNTS" }, "*");
          window.addEventListener("message", function handler(event) {
            if (event.data?.type === "BITFS_ACCOUNTS_RESULT") {
              window.removeEventListener("message", handler);
              if (event.data.error) reject(new Error(event.data.error));
              else resolve(event.data.accounts);
            }
          });
        });
      }
    };
  `;
  (document.head || document.documentElement).appendChild(script);
  script.remove();
}

// --- Message relay between page and extension ---

window.addEventListener("message", (event) => {
  if (event.source !== window) return;

  if (event.data?.type === "BITFS_REQUEST_ACCOUNTS") {
    chrome.runtime.sendMessage(
      { type: "GET_ADDRESS" },
      (response) => {
        window.postMessage(
          {
            type: "BITFS_ACCOUNTS_RESULT",
            accounts: response?.data?.address
              ? [response.data.address]
              : [],
            error: response?.error,
          },
          "*"
        );
      }
    );
  }
});

// --- Init ---

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", () => {
    scanForBitFSLinks();
    injectBitFSAPI();
  });
} else {
  scanForBitFSLinks();
  injectBitFSAPI();
}

// Re-scan on dynamic content changes
const observer = new MutationObserver(() => {
  scanForBitFSLinks();
});
observer.observe(document.body, { childList: true, subtree: true });
```

**Step 2: Commit**

```bash
git add src/content-script/index.ts
git commit -m "feat: implement content script with link detection and DApp API"
```

---

## Phase 5: Final Assembly

### Task 16: Update manifest and build verification

**Files:**
- Modify: `public/manifest.json`
- Modify: `tsconfig.json`

**Step 1: Verify manifest.json is correct**

The existing manifest is already correct for MV3. Ensure `icons/` directory has placeholder PNGs.

```bash
# Create placeholder icons if missing
mkdir -p public/icons
```

If no icon files exist, create minimal SVG placeholders (or skip — Chrome will use defaults).

**Step 2: Update tsconfig for path resolution**

Verify `tsconfig.json` includes `test/` directory:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "module": "ES2022",
    "moduleResolution": "bundler",
    "lib": ["ES2022", "DOM", "DOM.Iterable"],
    "strict": true,
    "jsx": "react-jsx",
    "esModuleInterop": true,
    "skipLibCheck": true,
    "forceConsistentCasingInFileNames": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src", "test"]
}
```

**Step 3: Full build and verify**

```bash
npm run build
```

Expected: `dist/` contains:
- `popup/index.html` + popup JS/CSS assets
- `background/service-worker.js`
- `content-script/index.js`
- `assets/` (CSS etc.)

**Step 4: Run all tests**

```bash
npm test
```

Expected: All tests pass.

**Step 5: Verify extension loads in Chrome**

1. Open `chrome://extensions`
2. Enable Developer mode
3. Click "Load unpacked" → select `dist/`
4. Extension should appear with BitFS name
5. Click extension icon → popup should render dark-themed UI

**Step 6: Final commit**

```bash
git add -A
git commit -m "feat: complete BitFS Chrome Extension v0.1.0

Full MetaMask-model wallet with:
- HD wallet create/import/lock/unlock (Argon2id encryption)
- WhatsOnChain blockchain provider
- DAG scanning and IndexedDB caching
- File browser with content decryption
- Content script with bitfs:// link detection
- DApp API (window.bitfs)
- Dark theme with Tailwind CSS (BitFS VI colors)"
```

---

## Summary

| Phase | Tasks | Description |
|-------|-------|-------------|
| 0 | 1 | Project restructure (deps, Tailwind, remove stubs) |
| 1 | 2-5 | Core infrastructure (messages, storage, WoC provider, DAG cache) |
| 2 | 6-9 | Service Worker (wallet, DAG, file reader, message router) |
| 3 | 10-14 | Popup UI (contexts, components, all pages, routing) |
| 4 | 15 | Content Script (link detection, DApp API) |
| 5 | 16 | Final assembly and verification |

**Total: 16 tasks**, estimated ~4-6 hours of implementation time.

**Key decisions:**
- `@bitfs/libbitfs` via `file:../libbitfs-ts` (no npm publish needed)
- WhatsOnChain as blockchain provider (no daemon dependency)
- IndexedDB for DAG caching (survives extension restarts)
- React Context for state (no extra state library)
- Tailwind v4 with BitFS VI dark theme colors
