// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"fmt"
	"os"

	"github.com/bitfsorg/libbitfs-go/network"
	"github.com/bitfsorg/libbitfs-go/vault"
)

// configureChain resolves RPC configuration and attaches a BlockchainService
// to the vault. If no RPC URL can be resolved, the vault stays in offline mode.
func configureChain(v *vault.Vault, rpcURL, rpcUser, rpcPass, netName string) {
	flags := &network.RPCConfig{
		URL:      rpcURL,
		User:     rpcUser,
		Password: rpcPass,
	}

	env := envToMap()
	cfg, err := network.ResolveConfig(flags, env, netName)
	if err != nil {
		// No valid config — stay offline.
		fmt.Fprintf(os.Stderr, "Note: no RPC configured, running in offline mode (%v)\n", err)
		return
	}

	v.Chain = network.NewRPCClient(*cfg)
}

// envToMap reads BITFS_RPC_* environment variables into a map.
func envToMap() map[string]string {
	m := make(map[string]string)
	for _, key := range []string{"BITFS_RPC_URL", "BITFS_RPC_USER", "BITFS_RPC_PASS"} {
		if v := os.Getenv(key); v != "" {
			m[key] = v
		}
	}
	return m
}
