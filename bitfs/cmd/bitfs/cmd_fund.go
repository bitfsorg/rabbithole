// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"

	"github.com/tongxiaofeng/bitfs/internal/engine"
	"github.com/tongxiaofeng/libbitfs/config"
	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

// runFund handles the "bitfs fund" command.
// Registers externally-funded UTXOs into local state for transaction building.
func runFund(args []string) int {
	fs := flag.NewFlagSet("fund", flag.ContinueOnError)
	txid := fs.String("txid", "", "transaction ID (hex)")
	vout := fs.Int("vout", 0, "output index")
	amount := fs.Int("amount", 0, "amount in satoshis")
	dataDir := fs.String("datadir", config.DefaultDataDir(), "data directory")
	password := fs.String("password", "", "wallet password (for testing)")

	if err := fs.Parse(args); err != nil {
		return exitUsageError
	}

	if *txid == "" || *amount <= 0 {
		fmt.Fprintf(os.Stderr, "Usage: bitfs fund --txid <hex> --vout <N> --amount <sats>\n")
		return exitUsageError
	}

	// Validate txid is valid hex and 32 bytes.
	txidBytes, err := hex.DecodeString(*txid)
	if err != nil || len(txidBytes) != 32 {
		fmt.Fprintf(os.Stderr, "Error: --txid must be a 64-character hex string (32 bytes)\n")
		return exitUsageError
	}

	eng, err := engine.New(*dataDir, *password)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return exitWalletError
	}
	defer func() { _ = eng.Close() }()

	// Derive the fee receive key to get the expected pubkey and script.
	feeIdx := eng.WState.NextReceiveIndex
	feeKP, err := eng.Wallet.DeriveFeeKey(wallet.ExternalChain, feeIdx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: derive fee key: %v\n", err)
		return exitWalletError
	}

	scriptPK, err := tx.BuildP2PKHScript(feeKP.PublicKey)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: build script: %v\n", err)
		return exitError
	}

	pubHex := hex.EncodeToString(feeKP.PublicKey.Compressed())

	eng.State.AddUTXO(&engine.UTXOState{
		TxID:         *txid,
		Vout:         uint32(*vout),
		Amount:       uint64(*amount),
		ScriptPubKey: hex.EncodeToString(scriptPK),
		PubKeyHex:    pubHex,
		Type:         "fee",
	})

	eng.WState.NextReceiveIndex = feeIdx + 1

	// Save wallet state.
	statePath := *dataDir + "/state.json"
	if err := saveWalletState(statePath, eng.WState); err != nil {
		fmt.Fprintf(os.Stderr, "Error: save state: %v\n", err)
		return exitError
	}

	fmt.Printf("Registered UTXO:\n")
	fmt.Printf("  TxID:    %s\n", *txid)
	fmt.Printf("  Vout:    %d\n", *vout)
	fmt.Printf("  Amount:  %d sats\n", *amount)
	fmt.Printf("  Address: %s\n", pubHex[:16]+"...")
	fmt.Printf("  Fee key: %s\n", feeKP.Path)

	return exitSuccess
}
