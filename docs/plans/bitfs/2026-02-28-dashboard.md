# BitFS Dashboard — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Implement 5 daemon monitoring pages (status, storage, network, wallet, logs) with Dark Botanical theme, backed by 5 new Go API endpoints.

**Architecture:** Two parallel workstreams — Go backend (new `dashboard.go` + route registration + ring buffer logger) and React frontend (theme, shared components, 5 page rewrites). Backend first so frontend can test against real endpoints.

**Tech Stack:** Go 1.25.6 (daemon), React 19 + Vite 6 + TailwindCSS 4 + lucide-react (dashboard)

---

## Task 1: Add ring buffer logger to daemon

**Files:**
- Create: `bitfs/internal/daemon/logbuf.go`
- Test: `bitfs/internal/daemon/logbuf_test.go`

**Step 1: Write the test**

```go
// logbuf_test.go
package daemon

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogBuffer_Write(t *testing.T) {
	buf := newLogBuffer(3)
	buf.Add("info", "first")
	buf.Add("warn", "second")
	buf.Add("error", "third")

	entries := buf.Entries(10, "")
	require.Len(t, entries, 3)
	assert.Equal(t, "first", entries[0].Message)
	assert.Equal(t, "info", entries[0].Level)
	assert.Equal(t, "third", entries[2].Message)
}

func TestLogBuffer_Overflow(t *testing.T) {
	buf := newLogBuffer(2)
	buf.Add("info", "a")
	buf.Add("info", "b")
	buf.Add("info", "c") // evicts "a"

	entries := buf.Entries(10, "")
	require.Len(t, entries, 2)
	assert.Equal(t, "b", entries[0].Message)
	assert.Equal(t, "c", entries[1].Message)
}

func TestLogBuffer_FilterByLevel(t *testing.T) {
	buf := newLogBuffer(10)
	buf.Add("info", "ok")
	buf.Add("warn", "careful")
	buf.Add("error", "bad")

	entries := buf.Entries(10, "warn")
	require.Len(t, entries, 1)
	assert.Equal(t, "careful", entries[0].Message)
}

func TestLogBuffer_Limit(t *testing.T) {
	buf := newLogBuffer(10)
	for i := 0; i < 10; i++ {
		buf.Add("info", "msg")
	}

	entries := buf.Entries(3, "")
	assert.Len(t, entries, 3)
}

func TestLogBuffer_Empty(t *testing.T) {
	buf := newLogBuffer(5)
	entries := buf.Entries(10, "")
	assert.Empty(t, entries)
}
```

**Step 2: Run test to verify it fails**

Run: `cd bitfs && go test ./internal/daemon/ -run TestLogBuffer -v`
Expected: FAIL — `newLogBuffer` undefined

**Step 3: Write implementation**

```go
// logbuf.go
package daemon

import (
	"sync"
	"time"
)

// LogEntry represents a single log entry.
type LogEntry struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// logBuffer is a thread-safe fixed-size ring buffer for log entries.
type logBuffer struct {
	mu      sync.Mutex
	entries []LogEntry
	cap     int
	pos     int // next write position
	full    bool
}

func newLogBuffer(capacity int) *logBuffer {
	return &logBuffer{
		entries: make([]LogEntry, capacity),
		cap:     capacity,
	}
}

// Add appends a log entry to the buffer.
func (lb *logBuffer) Add(level, message string) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	lb.entries[lb.pos] = LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   message,
	}
	lb.pos = (lb.pos + 1) % lb.cap
	if lb.pos == 0 {
		lb.full = true
	}
}

// Entries returns up to limit entries, optionally filtered by level.
// Entries are returned in chronological order (oldest first).
func (lb *logBuffer) Entries(limit int, level string) []LogEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	var all []LogEntry
	if lb.full {
		// Ring buffer wrapped: read from pos..end, then 0..pos
		all = make([]LogEntry, 0, lb.cap)
		all = append(all, lb.entries[lb.pos:]...)
		all = append(all, lb.entries[:lb.pos]...)
	} else {
		all = make([]LogEntry, lb.pos)
		copy(all, lb.entries[:lb.pos])
	}

	// Filter by level if specified
	if level != "" {
		filtered := make([]LogEntry, 0, len(all))
		for _, e := range all {
			if e.Level == level {
				filtered = append(filtered, e)
			}
		}
		all = filtered
	}

	// Apply limit (take last N for most recent)
	if limit > 0 && len(all) > limit {
		all = all[len(all)-limit:]
	}

	return all
}
```

**Step 4: Run test to verify it passes**

Run: `cd bitfs && go test ./internal/daemon/ -run TestLogBuffer -v`
Expected: PASS

**Step 5: Commit**

```bash
cd bitfs
git add internal/daemon/logbuf.go internal/daemon/logbuf_test.go
git commit -m "feat(daemon): add ring buffer logger for dashboard"
```

---

## Task 2: Add dashboard API endpoints to daemon

**Files:**
- Create: `bitfs/internal/daemon/dashboard.go`
- Modify: `bitfs/internal/daemon/daemon.go` — add `startedAt`, `logBuf`, `StorageDir` fields
- Modify: `bitfs/internal/daemon/routes.go` — register 5 new routes

**Step 1: Add fields to Daemon struct**

In `daemon.go`, add to the `Daemon` struct (after the `invoiceDir` field at line 242):

```go
	// Dashboard support
	startedAt  time.Time
	logBuf     *logBuffer
	StorageDir string // path to ~/.bitfs/storage/ for stats
```

In `New()` function, after `d := &Daemon{...}` initialization (after line 265), add:

```go
	d.startedAt = time.Now()
	d.logBuf = newLogBuffer(200)
```

**Step 2: Add LogInfo method**

Add a helper method to Daemon so callers can push logs (after the `SetInvoiceDir` method):

```go
// LogInfo records a log entry to the ring buffer. Level should be "info", "warn", or "error".
func (d *Daemon) LogInfo(level, message string) {
	if d.logBuf != nil {
		d.logBuf.Add(level, message)
	}
}
```

**Step 3: Create dashboard.go with 5 handlers**

```go
// dashboard.go
package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

const dashboardVersion = "0.1.0"

// handleDashboardStatus returns daemon status information.
func (d *Daemon) handleDashboardStatus(w http.ResponseWriter, _ *http.Request) {
	var pnode string
	if d.wallet != nil {
		if pk, err := d.wallet.GetSellerKeyPair(); err == nil && pk != nil {
			pnode = pk.ToDERHex()
		}
	}

	resp := map[string]interface{}{
		"version":        dashboardVersion,
		"uptime_seconds": int(time.Since(d.startedAt).Seconds()),
		"listen_addr":    d.config.ListenAddr,
		"started_at":     d.startedAt.UTC().Format(time.RFC3339),
		"mainnet":        d.config.Mainnet,
	}
	if pnode != "" {
		resp["vault_pnode"] = pnode
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDashboardStorage returns content storage statistics.
func (d *Daemon) handleDashboardStorage(w http.ResponseWriter, _ *http.Request) {
	storageDir := d.StorageDir
	if storageDir == "" {
		storageDir = d.config.Storage.DataDir
	}

	var fileCount int
	var totalSize int64

	// Walk the storage directory to count files and total size.
	_ = filepath.WalkDir(storageDir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil // skip errors
		}
		if !entry.IsDir() {
			fileCount++
			if info, infoErr := entry.Info(); infoErr == nil {
				totalSize += info.Size()
			}
		}
		return nil
	})

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"file_count":       fileCount,
		"total_size_bytes": totalSize,
		"storage_path":     storageDir,
	})
}

// handleDashboardWallet returns wallet information.
func (d *Daemon) handleDashboardWallet(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{
		"available": d.wallet != nil,
	}

	if d.wallet != nil {
		if _, pk, err := d.wallet.GetSellerKeyPair(); err == nil && pk != nil {
			resp["pubkey"] = pk.ToDERHex()
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDashboardNetwork returns network/SPV status.
func (d *Daemon) handleDashboardNetwork(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{
		"mainnet":     d.config.Mainnet,
		"spv_enabled": d.spv != nil,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDashboardLogs returns recent log entries from the ring buffer.
func (d *Daemon) handleDashboardLogs(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	level := r.URL.Query().Get("level")

	entries := d.logBuf.Entries(limit, level)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"entries": entries,
	})
}
```

**Step 4: Register routes in `routes.go`**

In `RegisterRoutes()`, add after the SPV routes (after line 41):

```go
	// Dashboard API
	mux.HandleFunc("GET /_bitfs/dashboard/status", wrap(d.handleDashboardStatus))
	mux.HandleFunc("GET /_bitfs/dashboard/storage", wrap(d.handleDashboardStorage))
	mux.HandleFunc("GET /_bitfs/dashboard/wallet", wrap(d.handleDashboardWallet))
	mux.HandleFunc("GET /_bitfs/dashboard/network", wrap(d.handleDashboardNetwork))
	mux.HandleFunc("GET /_bitfs/dashboard/logs", wrap(d.handleDashboardLogs))
```

**Step 5: Run existing tests to verify nothing is broken**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1`
Expected: All existing tests PASS

**Step 6: Commit**

```bash
cd bitfs
git add internal/daemon/dashboard.go internal/daemon/daemon.go internal/daemon/routes.go
git commit -m "feat(daemon): add 5 dashboard API endpoints"
```

---

## Task 3: Add dashboard API tests

**Files:**
- Create: `bitfs/internal/daemon/dashboard_test.go`

**Step 1: Write tests**

```go
package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDashboardStatus(t *testing.T) {
	d := newTestDaemon(t)
	req := httptest.NewRequest(http.MethodGet, "/_bitfs/dashboard/status", nil)
	w := httptest.NewRecorder()

	d.handleDashboardStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, dashboardVersion, resp["version"])
	assert.Contains(t, resp, "uptime_seconds")
	assert.Contains(t, resp, "listen_addr")
	assert.Contains(t, resp, "started_at")
}

func TestDashboardStorage(t *testing.T) {
	d := newTestDaemon(t)

	// Create a temp storage dir with some files
	tmpDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "a.dat"), []byte("hello"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "b.dat"), []byte("world!"), 0600))
	d.StorageDir = tmpDir

	req := httptest.NewRequest(http.MethodGet, "/_bitfs/dashboard/storage", nil)
	w := httptest.NewRecorder()

	d.handleDashboardStorage(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(2), resp["file_count"])
	assert.Equal(t, float64(11), resp["total_size_bytes"]) // "hello" + "world!" = 11
	assert.Equal(t, tmpDir, resp["storage_path"])
}

func TestDashboardWallet(t *testing.T) {
	d := newTestDaemon(t)
	req := httptest.NewRequest(http.MethodGet, "/_bitfs/dashboard/wallet", nil)
	w := httptest.NewRecorder()

	d.handleDashboardWallet(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "available")
}

func TestDashboardNetwork(t *testing.T) {
	d := newTestDaemon(t)
	req := httptest.NewRequest(http.MethodGet, "/_bitfs/dashboard/network", nil)
	w := httptest.NewRecorder()

	d.handleDashboardNetwork(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp, "mainnet")
	assert.Contains(t, resp, "spv_enabled")
}

func TestDashboardLogs(t *testing.T) {
	d := newTestDaemon(t)
	d.LogInfo("info", "started")
	d.LogInfo("warn", "low disk")
	d.LogInfo("error", "connection failed")

	req := httptest.NewRequest(http.MethodGet, "/_bitfs/dashboard/logs?limit=2", nil)
	w := httptest.NewRecorder()

	d.handleDashboardLogs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []LogEntry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Entries, 2)
}

func TestDashboardLogs_FilterLevel(t *testing.T) {
	d := newTestDaemon(t)
	d.LogInfo("info", "ok")
	d.LogInfo("error", "bad")

	req := httptest.NewRequest(http.MethodGet, "/_bitfs/dashboard/logs?level=error", nil)
	w := httptest.NewRecorder()

	d.handleDashboardLogs(w, req)

	var resp struct {
		Entries []LogEntry `json:"entries"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Entries, 1)
	assert.Equal(t, "bad", resp.Entries[0].Message)
}
```

Note: `newTestDaemon` is a helper that should already exist in the test files. If not, add one:

```go
func newTestDaemon(t *testing.T) *Daemon {
	t.Helper()
	d, err := New(DefaultConfig(), &mockWallet{}, &mockStore{}, nil)
	require.NoError(t, err)
	return d
}
```

Check `daemon_test.go` or `daemon_supplementary_test.go` for existing test helpers. Reuse them if available.

**Step 2: Run tests**

Run: `cd bitfs && go test ./internal/daemon/ -run TestDashboard -v`
Expected: All PASS

**Step 3: Commit**

```bash
cd bitfs
git add internal/daemon/dashboard_test.go
git commit -m "test(daemon): add dashboard API endpoint tests"
```

---

## Task 4: Dark Botanical theme + shared components (React)

**Files:**
- Modify: `bitfs/dashboard/src/index.css` — theme CSS variables
- Modify: `bitfs/dashboard/index.html` — font imports
- Create: `bitfs/dashboard/src/components/StatCard.tsx`
- Create: `bitfs/dashboard/src/components/DataTable.tsx`
- Create: `bitfs/dashboard/src/components/Badge.tsx`
- Create: `bitfs/dashboard/src/hooks/usePolling.ts`
- Modify: `bitfs/dashboard/src/lib/api.ts` — real API functions
- Modify: `bitfs/dashboard/src/layouts/MainLayout.tsx` — dark theme

**Step 1: Update index.html for fonts**

Replace the existing `<head>` content in `index.html` to add Google Fonts:

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>BitFS Dashboard</title>
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&family=JetBrains+Mono:wght@400;500&display=swap" rel="stylesheet" />
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

**Step 2: Update index.css with Dark Botanical theme**

Replace `bitfs/dashboard/src/index.css`:

```css
@import "tailwindcss";

@theme {
  --color-bg-primary: #1a1a1a;
  --color-bg-card: #242424;
  --color-bg-card-hover: #2a2a2a;
  --color-bg-sidebar: #1e1e1e;
  --color-border: #333333;
  --color-accent: #c9956b;
  --color-accent-hover: #d4a57a;
  --color-text-primary: #e5e5e5;
  --color-text-secondary: #999999;
  --color-text-muted: #666666;
  --color-success: #4ade80;
  --color-warning: #fbbf24;
  --color-error: #f87171;
  --font-sans: "Inter", system-ui, sans-serif;
  --font-mono: "JetBrains Mono", ui-monospace, monospace;
}

body {
  background-color: var(--color-bg-primary);
  color: var(--color-text-primary);
  font-family: var(--font-sans);
}
```

**Step 3: Update MainLayout.tsx for dark theme**

Replace `bitfs/dashboard/src/layouts/MainLayout.tsx`:

```tsx
import { NavLink, Outlet } from "react-router-dom";
import {
  LayoutDashboard,
  HardDrive,
  Globe,
  Wallet,
  ScrollText,
} from "lucide-react";

const navItems = [
  { to: "/", label: "Dashboard", icon: LayoutDashboard },
  { to: "/storage", label: "Storage", icon: HardDrive },
  { to: "/network", label: "Network", icon: Globe },
  { to: "/wallet", label: "Wallet", icon: Wallet },
  { to: "/logs", label: "Logs", icon: ScrollText },
];

function MainLayout() {
  return (
    <div className="flex h-screen bg-bg-primary text-text-primary">
      <aside className="flex w-56 flex-col border-r border-border bg-bg-sidebar">
        <div className="flex h-14 items-center border-b border-border px-4">
          <span className="text-lg font-semibold text-accent">BitFS</span>
        </div>
        <nav className="flex-1 space-y-1 p-2">
          {navItems.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === "/"}
              className={({ isActive }) =>
                `flex items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-bg-card text-accent"
                    : "text-text-secondary hover:bg-bg-card hover:text-text-primary"
                }`
              }
            >
              <Icon className="h-4 w-4" />
              {label}
            </NavLink>
          ))}
        </nav>
        <div className="border-t border-border p-3 text-xs text-text-muted">
          BitFS LFCP Dashboard
        </div>
      </aside>
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}

export default MainLayout;
```

**Step 4: Create usePolling hook**

Create `bitfs/dashboard/src/hooks/usePolling.ts`:

```ts
import { useState, useEffect, useCallback } from "react";

interface UsePollingResult<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  refresh: () => void;
}

export function usePolling<T>(url: string, intervalMs = 5000): UsePollingResult<T> {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(async () => {
    try {
      const res = await fetch(url);
      if (!res.ok) throw new Error(`HTTP ${res.status}`);
      const json = await res.json();
      setData(json);
      setError(null);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Unknown error");
    } finally {
      setLoading(false);
    }
  }, [url]);

  useEffect(() => {
    fetchData();
    const id = setInterval(fetchData, intervalMs);
    return () => clearInterval(id);
  }, [fetchData, intervalMs]);

  return { data, error, loading, refresh: fetchData };
}
```

**Step 5: Create StatCard component**

Create `bitfs/dashboard/src/components/StatCard.tsx`:

```tsx
import type { LucideIcon } from "lucide-react";

interface StatCardProps {
  label: string;
  value: string | number;
  icon?: LucideIcon;
  mono?: boolean;
}

export function StatCard({ label, value, icon: Icon, mono }: StatCardProps) {
  return (
    <div className="rounded-lg border border-border bg-bg-card p-4">
      <div className="flex items-center gap-2 text-sm text-text-secondary">
        {Icon && <Icon className="h-4 w-4" />}
        {label}
      </div>
      <div
        className={`mt-2 text-xl font-semibold ${mono ? "font-mono text-base" : ""}`}
      >
        {value}
      </div>
    </div>
  );
}
```

**Step 6: Create Badge component**

Create `bitfs/dashboard/src/components/Badge.tsx`:

```tsx
const variants: Record<string, string> = {
  info: "text-text-secondary bg-bg-card-hover",
  warn: "text-warning bg-warning/10",
  error: "text-error bg-error/10",
  success: "text-success bg-success/10",
};

interface BadgeProps {
  variant: string;
  children: React.ReactNode;
}

export function Badge({ variant, children }: BadgeProps) {
  return (
    <span
      className={`inline-block rounded px-2 py-0.5 text-xs font-medium ${variants[variant] ?? variants.info}`}
    >
      {children}
    </span>
  );
}
```

**Step 7: Create DataTable component**

Create `bitfs/dashboard/src/components/DataTable.tsx`:

```tsx
interface Column<T> {
  key: string;
  label: string;
  mono?: boolean;
  render?: (row: T) => React.ReactNode;
}

interface DataTableProps<T> {
  columns: Column<T>[];
  data: T[];
  emptyMessage?: string;
}

export function DataTable<T extends Record<string, unknown>>({
  columns,
  data,
  emptyMessage = "No data",
}: DataTableProps<T>) {
  return (
    <div className="overflow-x-auto rounded-lg border border-border">
      <table className="w-full text-sm">
        <thead>
          <tr className="border-b border-border bg-bg-card">
            {columns.map((col) => (
              <th
                key={col.key}
                className="px-4 py-2 text-left text-xs font-medium text-text-secondary"
              >
                {col.label}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>
          {data.length === 0 ? (
            <tr>
              <td
                colSpan={columns.length}
                className="px-4 py-8 text-center text-text-muted"
              >
                {emptyMessage}
              </td>
            </tr>
          ) : (
            data.map((row, i) => (
              <tr
                key={i}
                className="border-b border-border last:border-0 hover:bg-bg-card-hover"
              >
                {columns.map((col) => (
                  <td
                    key={col.key}
                    className={`px-4 py-2 ${col.mono ? "font-mono text-xs" : ""}`}
                  >
                    {col.render ? col.render(row) : String(row[col.key] ?? "")}
                  </td>
                ))}
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
```

**Step 8: Update api.ts**

Replace `bitfs/dashboard/src/lib/api.ts`:

```ts
/** Base URL for the BitFS daemon dashboard API. */
export const API_BASE = "/_bitfs/dashboard";

export interface StatusResponse {
  version: string;
  uptime_seconds: number;
  listen_addr: string;
  started_at: string;
  mainnet: boolean;
  vault_pnode?: string;
}

export interface StorageResponse {
  file_count: number;
  total_size_bytes: number;
  storage_path: string;
}

export interface WalletResponse {
  available: boolean;
  pubkey?: string;
}

export interface NetworkResponse {
  mainnet: boolean;
  spv_enabled: boolean;
}

export interface LogEntry {
  timestamp: string;
  level: string;
  message: string;
}

export interface LogsResponse {
  entries: LogEntry[];
}

export interface SaleRecord {
  invoice_id: string;
  price: number;
  key_hash: string;
  timestamp: number;
  paid: boolean;
}
```

**Step 9: Verify build**

Run: `cd bitfs/dashboard && npm run build`
Expected: Build succeeds with no TypeScript errors

**Step 10: Commit**

```bash
cd bitfs
git add dashboard/index.html dashboard/src/index.css dashboard/src/layouts/MainLayout.tsx dashboard/src/hooks/usePolling.ts dashboard/src/components/StatCard.tsx dashboard/src/components/Badge.tsx dashboard/src/components/DataTable.tsx dashboard/src/lib/api.ts
git commit -m "feat(dashboard): dark botanical theme + shared components + api types"
```

---

## Task 5: Implement DashboardHome page

**Files:**
- Modify: `bitfs/dashboard/src/pages/DashboardHome.tsx`

**Step 1: Replace stub with real implementation**

```tsx
import { Clock, HardDrive, Coins, Link } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { DataTable } from "@/components/DataTable";
import { Badge } from "@/components/Badge";
import { usePolling } from "@/hooks/usePolling";
import type { StatusResponse, StorageResponse, SaleRecord } from "@/lib/api";

function formatUptime(seconds: number): string {
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h ${m}m`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** i).toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

const salesColumns = [
  { key: "invoice_id", label: "Invoice", mono: true },
  { key: "price", label: "Price (sat)" },
  {
    key: "paid",
    label: "Status",
    render: (row: SaleRecord) => (
      <Badge variant={row.paid ? "success" : "warn"}>
        {row.paid ? "Paid" : "Pending"}
      </Badge>
    ),
  },
];

function DashboardHome() {
  const { data: status } = usePolling<StatusResponse>("/_bitfs/dashboard/status");
  const { data: storage } = usePolling<StorageResponse>("/_bitfs/dashboard/storage");
  const { data: sales } = usePolling<SaleRecord[]>("/_bitfs/sales", 10000);

  return (
    <div>
      <h1 className="text-2xl font-bold">Dashboard</h1>
      <p className="mt-1 text-sm text-text-secondary">
        Node overview and status
      </p>

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatCard
          label="Uptime"
          value={status ? formatUptime(status.uptime_seconds) : "—"}
          icon={Clock}
        />
        <StatCard
          label="Files"
          value={storage ? storage.file_count : "—"}
          icon={HardDrive}
        />
        <StatCard
          label="Storage"
          value={storage ? formatBytes(storage.total_size_bytes) : "—"}
          icon={HardDrive}
        />
        <StatCard
          label="Version"
          value={status?.version ?? "—"}
          icon={Link}
          mono
        />
      </div>

      <h2 className="mt-8 text-lg font-semibold">Recent Sales</h2>
      <div className="mt-3">
        <DataTable
          columns={salesColumns}
          data={sales ?? []}
          emptyMessage="No sales yet"
        />
      </div>
    </div>
  );
}

export default DashboardHome;
```

**Step 2: Verify build**

Run: `cd bitfs/dashboard && npm run build`
Expected: PASS

**Step 3: Commit**

```bash
cd bitfs
git add dashboard/src/pages/DashboardHome.tsx
git commit -m "feat(dashboard): implement DashboardHome page"
```

---

## Task 6: Implement Storage page

**Files:**
- Modify: `bitfs/dashboard/src/pages/Storage.tsx`

**Step 1: Replace stub**

```tsx
import { HardDrive, FolderOpen, Database } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { usePolling } from "@/hooks/usePolling";
import type { StorageResponse } from "@/lib/api";

function formatBytes(bytes: number): string {
  if (bytes === 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  const i = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1);
  return `${(bytes / 1024 ** i).toFixed(i > 0 ? 1 : 0)} ${units[i]}`;
}

function Storage() {
  const { data, error, loading } = usePolling<StorageResponse>(
    "/_bitfs/dashboard/storage",
  );

  return (
    <div>
      <h1 className="text-2xl font-bold">Storage</h1>
      <p className="mt-1 text-sm text-text-secondary">
        Content-addressed file store
      </p>

      {error && (
        <p className="mt-4 text-sm text-error">Failed to load: {error}</p>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard
          label="Files"
          value={data ? data.file_count : loading ? "..." : "—"}
          icon={HardDrive}
        />
        <StatCard
          label="Total Size"
          value={data ? formatBytes(data.total_size_bytes) : loading ? "..." : "—"}
          icon={Database}
        />
        <StatCard
          label="Storage Path"
          value={data?.storage_path ?? "—"}
          icon={FolderOpen}
          mono
        />
      </div>
    </div>
  );
}

export default Storage;
```

**Step 2: Verify build & commit**

Run: `cd bitfs/dashboard && npm run build`

```bash
cd bitfs
git add dashboard/src/pages/Storage.tsx
git commit -m "feat(dashboard): implement Storage page"
```

---

## Task 7: Implement Network page

**Files:**
- Modify: `bitfs/dashboard/src/pages/Network.tsx`

**Step 1: Replace stub**

```tsx
import { Globe, Shield, Radio } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { Badge } from "@/components/Badge";
import { usePolling } from "@/hooks/usePolling";
import type { NetworkResponse } from "@/lib/api";

function Network() {
  const { data, error, loading } = usePolling<NetworkResponse>(
    "/_bitfs/dashboard/network",
  );

  return (
    <div>
      <h1 className="text-2xl font-bold">Network</h1>
      <p className="mt-1 text-sm text-text-secondary">
        Blockchain connectivity and SPV status
      </p>

      {error && (
        <p className="mt-4 text-sm text-error">Failed to load: {error}</p>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
        <StatCard
          label="Network"
          value={
            data
              ? data.mainnet
                ? "Mainnet"
                : "Testnet / Regtest"
              : loading
                ? "..."
                : "—"
          }
          icon={Globe}
        />
        <StatCard
          label="SPV"
          value={
            data
              ? data.spv_enabled
                ? "Enabled"
                : "Disabled"
              : loading
                ? "..."
                : "—"
          }
          icon={Shield}
        />
        <StatCard
          label="Blockchain Service"
          value={data ? "Connected" : loading ? "..." : "—"}
          icon={Radio}
        />
      </div>

      <div className="mt-6 rounded-lg border border-border bg-bg-card p-4">
        <h3 className="text-sm font-medium text-text-secondary">
          Connection Status
        </h3>
        <div className="mt-3 flex items-center gap-2">
          <span
            className={`h-2 w-2 rounded-full ${data ? "bg-success" : "bg-error"}`}
          />
          <span className="text-sm">
            {data ? "Daemon responding" : error ? "Connection error" : "Loading..."}
          </span>
        </div>
        {data && (
          <div className="mt-2 flex items-center gap-2">
            <Badge variant={data.spv_enabled ? "success" : "warn"}>
              SPV {data.spv_enabled ? "ON" : "OFF"}
            </Badge>
          </div>
        )}
      </div>
    </div>
  );
}

export default Network;
```

**Step 2: Verify build & commit**

Run: `cd bitfs/dashboard && npm run build`

```bash
cd bitfs
git add dashboard/src/pages/Network.tsx
git commit -m "feat(dashboard): implement Network page"
```

---

## Task 8: Implement Wallet page

**Files:**
- Modify: `bitfs/dashboard/src/pages/Wallet.tsx`

**Step 1: Replace stub**

```tsx
import { Wallet as WalletIcon, Key, Copy } from "lucide-react";
import { StatCard } from "@/components/StatCard";
import { usePolling } from "@/hooks/usePolling";
import type { WalletResponse } from "@/lib/api";
import { useState } from "react";

function Wallet() {
  const { data, error, loading } = usePolling<WalletResponse>(
    "/_bitfs/dashboard/wallet",
  );
  const [copied, setCopied] = useState(false);

  const copyPubkey = async () => {
    if (data?.pubkey) {
      await navigator.clipboard.writeText(data.pubkey);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    }
  };

  return (
    <div>
      <h1 className="text-2xl font-bold">Wallet</h1>
      <p className="mt-1 text-sm text-text-secondary">
        HD wallet information
      </p>

      {error && (
        <p className="mt-4 text-sm text-error">Failed to load: {error}</p>
      )}

      <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2">
        <StatCard
          label="Status"
          value={
            data
              ? data.available
                ? "Active"
                : "Not configured"
              : loading
                ? "..."
                : "—"
          }
          icon={WalletIcon}
        />
        <StatCard
          label="Derivation"
          value="m/44'/236'/0'"
          icon={Key}
          mono
        />
      </div>

      {data?.pubkey && (
        <div className="mt-6 rounded-lg border border-border bg-bg-card p-4">
          <div className="flex items-center justify-between">
            <span className="text-sm text-text-secondary">
              Vault Public Key
            </span>
            <button
              onClick={copyPubkey}
              className="flex items-center gap-1 rounded px-2 py-1 text-xs text-text-secondary hover:bg-bg-card-hover hover:text-accent"
            >
              <Copy className="h-3 w-3" />
              {copied ? "Copied" : "Copy"}
            </button>
          </div>
          <p className="mt-2 break-all font-mono text-sm">{data.pubkey}</p>
        </div>
      )}
    </div>
  );
}

export default Wallet;
```

**Step 2: Verify build & commit**

Run: `cd bitfs/dashboard && npm run build`

```bash
cd bitfs
git add dashboard/src/pages/Wallet.tsx
git commit -m "feat(dashboard): implement Wallet page"
```

---

## Task 9: Implement Logs page

**Files:**
- Modify: `bitfs/dashboard/src/pages/Logs.tsx`

**Step 1: Replace stub**

```tsx
import { Badge } from "@/components/Badge";
import { DataTable } from "@/components/DataTable";
import { usePolling } from "@/hooks/usePolling";
import type { LogEntry, LogsResponse } from "@/lib/api";
import { useState } from "react";

const levelVariant: Record<string, string> = {
  info: "info",
  warn: "warn",
  error: "error",
};

function Logs() {
  const [level, setLevel] = useState("");
  const { data, error } = usePolling<LogsResponse>(
    `/_bitfs/dashboard/logs?limit=100${level ? `&level=${level}` : ""}`,
  );

  const columns = [
    {
      key: "timestamp",
      label: "Time",
      mono: true,
      render: (row: LogEntry) => {
        const d = new Date(row.timestamp);
        return d.toLocaleTimeString();
      },
    },
    {
      key: "level",
      label: "Level",
      render: (row: LogEntry) => (
        <Badge variant={levelVariant[row.level] ?? "info"}>
          {row.level.toUpperCase()}
        </Badge>
      ),
    },
    { key: "message", label: "Message", mono: true },
  ];

  return (
    <div>
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Logs</h1>
          <p className="mt-1 text-sm text-text-secondary">
            Daemon log entries (auto-refresh 5s)
          </p>
        </div>
        <select
          value={level}
          onChange={(e) => setLevel(e.target.value)}
          className="rounded border border-border bg-bg-card px-3 py-1.5 text-sm text-text-primary"
        >
          <option value="">All levels</option>
          <option value="info">Info</option>
          <option value="warn">Warn</option>
          <option value="error">Error</option>
        </select>
      </div>

      {error && (
        <p className="mt-4 text-sm text-error">Failed to load: {error}</p>
      )}

      <div className="mt-4">
        <DataTable
          columns={columns}
          data={data?.entries ?? []}
          emptyMessage="No log entries"
        />
      </div>
    </div>
  );
}

export default Logs;
```

**Step 2: Verify build & commit**

Run: `cd bitfs/dashboard && npm run build`

```bash
cd bitfs
git add dashboard/src/pages/Logs.tsx
git commit -m "feat(dashboard): implement Logs page"
```

---

## Task 10: Final verification + build

**Step 1: Run all Go daemon tests**

Run: `cd bitfs && go test ./internal/daemon/ -v -count=1 -race`
Expected: All PASS

**Step 2: Run Go lint**

Run: `cd bitfs && golangci-lint run ./internal/daemon/`
Expected: No new warnings

**Step 3: Build dashboard**

Run: `cd bitfs/dashboard && npm run build`
Expected: Build succeeds, `dist/` directory updated

**Step 4: Build Go binary** (verify embed compiles)

Run: `cd bitfs && go build ./cmd/bitfs`
Expected: Compiles without errors (embed is commented out so this should work regardless)

**Step 5: Commit build artifacts**

```bash
cd bitfs
git add dashboard/dist/
git commit -m "build(dashboard): update dist for embedding"
```
