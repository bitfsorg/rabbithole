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

// mputResult summarizes a recursive upload.
type mputResult struct {
	DirsCreated   int
	FilesUploaded int
	Errors        []string
}

// runMput handles the "bitfs mput" command.
func runMput(args []string) int {
	fs := flag.NewFlagSet("mput", flag.ContinueOnError)
	vaultName := fs.String("vault", "", "vault name")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if fs.NArg() < 2 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs mput <local-dir> <remote-dir> [--vault N]\n")
		return exitUsageError
	}

	localDir := fs.Arg(0)
	remoteDir := fs.Arg(1)
	access := "free"
	if fs.NArg() > 2 {
		access = fs.Arg(2)
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

	result, err := doMput(v, vaultIdx, localDir, remoteDir, access)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitError
	}

	fmt.Printf("Uploaded %d files, created %d directories\n",
		result.FilesUploaded, result.DirsCreated)
	for _, e := range result.Errors {
		fmt.Fprintf(os.Stderr, "  warning: %s\n", e)
	}

	return exitSuccess
}

// doMput recursively uploads a local directory to the vault.
func doMput(v *vault.Vault, vaultIdx uint32, localDir, remoteDir, access string) (*mputResult, error) {
	if remoteDir == "" {
		return nil, fmt.Errorf("mput: remote directory path cannot be empty")
	}
	if strings.Contains(remoteDir, "..") {
		return nil, fmt.Errorf("mput: remote directory path must not contain '..' components")
	}

	info, err := os.Stat(localDir)
	if err != nil {
		return nil, fmt.Errorf("mput: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("mput: %q is not a directory", localDir)
	}

	if access == "" {
		access = "free"
	}

	result := &mputResult{}
	baseDir := filepath.Clean(localDir)

	err = filepath.WalkDir(baseDir, func(localPath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("walk %s: %v", localPath, walkErr))
			return nil
		}

		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}

		rel, relErr := filepath.Rel(baseDir, localPath)
		if relErr != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("rel %s: %v", localPath, relErr))
			return nil
		}

		if rel == "." {
			return nil
		}

		remotePath := remoteDir
		if !strings.HasSuffix(remotePath, "/") {
			remotePath += "/"
		}
		remotePath += filepath.ToSlash(rel)

		if d.IsDir() {
			_, mkErr := v.Mkdir(&vault.MkdirOpts{
				VaultIndex: vaultIdx,
				Path:       remotePath,
			})
			if mkErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("mkdir %s: %v", remotePath, mkErr))
			} else {
				result.DirsCreated++
			}
		} else if d.Type().IsRegular() {
			_, putErr := v.PutFile(&vault.PutOpts{
				VaultIndex: vaultIdx,
				LocalFile:  localPath,
				RemotePath: remotePath,
				Access:     access,
			})
			if putErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("put %s: %v", remotePath, putErr))
			} else {
				result.FilesUploaded++
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("mput: walk directory: %w", err)
	}

	return result, nil
}
