// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/bitfsorg/metanet/internal/config"
)

// nodeStatus represents the JSON output of the status command.
type nodeStatus struct {
	Version string `json:"version"`
	NodeID  string `json:"node_id"`
	DataDir string `json:"datadir"`
	Network string `json:"network"`
	Listen  string `json:"listen"`
	RPC     string `json:"rpc"`
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
}

// cmdStatus handles the "metanet status" command.
// It reads the config and PID file and outputs the node status.
func cmdStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	jsonOut := fs.Bool("json", false, "output JSON format")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	// Check if initialized.
	keyPath := filepath.Join(*dataDir, "node.key")
	if _, err := os.Stat(keyPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: node not initialized. Run 'metanet init' first.\n")
		return exitError
	}

	// Load node key.
	pub, _, err := loadNodeKey(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	// Load config.
	cfgPath := config.ConfigPath(*dataDir)
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot load config: %v\n", err)
		return exitError
	}

	// Check if running.
	pidPath := filepath.Join(*dataDir, "metanet.pid")
	pid, pidErr := readPIDFile(pidPath)
	running := pidErr == nil

	status := nodeStatus{
		Version: Version,
		NodeID:  hex.EncodeToString(pub),
		DataDir: cfg.DataDir,
		Network: cfg.Network,
		Listen:  cfg.ListenAddr,
		RPC:     cfg.RPCAddr,
		Running: running,
	}
	if running {
		status.PID = pid
	}

	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(status); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return exitError
		}
	} else {
		fmt.Printf("Metanet Node Status\n")
		fmt.Printf("  Version:   %s\n", status.Version)
		fmt.Printf("  Node ID:   %s\n", status.NodeID)
		fmt.Printf("  Data dir:  %s\n", status.DataDir)
		fmt.Printf("  Network:   %s\n", status.Network)
		fmt.Printf("  Listen:    %s\n", status.Listen)
		fmt.Printf("  RPC:       %s\n", status.RPC)
		if running {
			fmt.Printf("  Status:    running (PID %d)\n", status.PID)
		} else {
			fmt.Printf("  Status:    stopped\n")
		}
	}

	return exitSuccess
}
