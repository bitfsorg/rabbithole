// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package config

import (
	"fmt"
	"net"
	"strings"
)

// validLogLevels lists the accepted log level strings.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// ValidateConfig checks that all configuration values are within acceptable
// ranges and returns the first error encountered, or nil if valid.
func ValidateConfig(cfg Config) error {
	if cfg.DataDir == "" {
		return ErrEmptyDataDir
	}

	if cfg.Network != "mainnet" && cfg.Network != "testnet" {
		return ErrInvalidNetwork
	}

	if err := validateAddr(cfg.ListenAddr); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidListenAddr, err)
	}

	if err := validateAddr(cfg.RPCAddr); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidRPCAddr, err)
	}

	if cfg.MaxPeers < 0 || cfg.MaxPeers > 1000 {
		return ErrInvalidMaxPeers
	}

	if !validLogLevels[strings.ToLower(cfg.LogLevel)] {
		return ErrInvalidLogLevel
	}

	return nil
}

// validateAddr checks that addr is a valid host:port address.
func validateAddr(addr string) error {
	_, _, err := net.SplitHostPort(addr)
	return err
}
