package daemon

import (
	"encoding/hex"
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
	resp := map[string]interface{}{
		"version":        dashboardVersion,
		"uptime_seconds": time.Since(d.startedAt).Seconds(),
		"listen_addr":    d.config.ListenAddr,
		"started_at":     d.startedAt.Format(time.RFC3339),
		"mainnet":        d.config.Mainnet,
	}

	if d.wallet != nil {
		_, pub, err := d.wallet.GetSellerKeyPair()
		if err == nil && pub != nil {
			resp["vault_pnode"] = hex.EncodeToString(pub.Compressed())
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDashboardStorage returns storage statistics.
func (d *Daemon) handleDashboardStorage(w http.ResponseWriter, _ *http.Request) {
	storageDir := d.StorageDir
	if storageDir == "" {
		storageDir = d.config.Storage.DataDir
	}

	var fileCount int
	var totalSize int64

	_ = filepath.WalkDir(storageDir, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			if info, infoErr := entry.Info(); infoErr == nil {
				fileCount++
				totalSize += info.Size()
			}
		}
		return nil
	})

	resp := map[string]interface{}{
		"file_count":       fileCount,
		"total_size_bytes": totalSize,
		"storage_path":     storageDir,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDashboardWallet returns wallet availability and public key.
func (d *Daemon) handleDashboardWallet(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]interface{}{
		"available": d.wallet != nil,
	}

	if d.wallet != nil {
		_, pub, err := d.wallet.GetSellerKeyPair()
		if err == nil && pub != nil {
			resp["pubkey"] = hex.EncodeToString(pub.Compressed())
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleDashboardNetwork returns network configuration status.
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
	limitStr := r.URL.Query().Get("limit")
	limit := 50
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 200 {
		limit = 200
	}

	level := r.URL.Query().Get("level")

	entries := d.logBuf.Entries(limit, level)

	resp := map[string]interface{}{
		"entries": entries,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
