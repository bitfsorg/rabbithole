// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bitfsorg/libbitfs-go/config"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// mgetResult summarizes a recursive download.
type mgetResult struct {
	DirsCreated     int
	FilesDownloaded int
	Errors          []string
}

// runMget handles the "bitfs mget" command.
func runMget(args []string) int {
	fs := flag.NewFlagSet("mget", flag.ContinueOnError)
	vaultName := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 1 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs mget <remote-dir> [local-dir] [--vault N]\n")
		return exitUsageError
	}

	remotePath := fs.Arg(0)
	localDir, _ := os.Getwd()
	if fs.NArg() > 1 {
		localDir = fs.Arg(1)
	}

	pass, err := resolvePassword(*password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}

	v, err := vault.New(*dataDir, pass)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = v.Close() }()

	vaultIdx, err := v.ResolveVaultIndex(*vaultName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitNotFound
	}

	result, err := doMget(v, vaultIdx, remotePath, localDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Printf("Downloaded %d files, created %d directories\n",
		result.FilesDownloaded, result.DirsCreated)
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", e)
	}

	return exitSuccess
}

// doMget recursively downloads a vault directory to local disk.
func doMget(v *vault.Vault, vaultIdx uint32, remotePath, localDir string) (*mgetResult, error) {
	node := v.State.FindNodeByPath(remotePath)
	if node == nil {
		return nil, fmt.Errorf("mget: %q not found", remotePath)
	}
	if node.Type != "dir" {
		return nil, fmt.Errorf("mget: %q is not a directory", remotePath)
	}

	result := &mgetResult{}
	mgetRecurse(v, vaultIdx, node, localDir, result)
	return result, nil
}

// mgetRecurse recursively downloads directory contents.
func mgetRecurse(v *vault.Vault, vaultIdx uint32, dir *vault.NodeState, localDir string, result *mgetResult) {
	if err := os.MkdirAll(localDir, 0755); err != nil {
		result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", localDir, err))
		return
	}
	result.DirsCreated++

	for _, child := range dir.Children {
		if strings.Contains(child.Name, "..") || strings.ContainsAny(child.Name, "/\\") || child.Name == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("unsafe child name %q, skipping", child.Name))
			continue
		}

		childNode := v.State.GetNode(child.PubKey)
		if childNode == nil {
			pubPrefix := child.PubKey
			if len(pubPrefix) > 8 {
				pubPrefix = pubPrefix[:8]
			}
			result.Errors = append(result.Errors, fmt.Sprintf("node %s not found for %s", pubPrefix, child.Name))
			continue
		}

		switch childNode.Type {
		case "dir":
			childLocalDir := filepath.Join(localDir, child.Name)
			mgetRecurse(v, vaultIdx, childNode, childLocalDir, result)
		case "file":
			localPath := filepath.Join(localDir, child.Name)
			_, getErr := v.Get(&vault.GetOpts{
				VaultIndex: vaultIdx,
				RemotePath: childNode.Path,
				LocalPath:  localPath,
			})
			if getErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("get %s: %v", childNode.Path, getErr))
			} else {
				result.FilesDownloaded++
			}
		}
	}
}
