package buy

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/bitfsorg/libbitfs-go/network"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// ErrInsufficientBalance is returned when available UTXOs cannot cover the
// requested amount plus estimated transaction fees.
var ErrInsufficientBalance = errors.New("buyer: insufficient balance")

// EstimateFee estimates the transaction fee in satoshis for a standard
// P2PKH transaction. Each input contributes ~148 bytes (41 bytes outpoint +
// ~107 bytes unlocking script), each output ~34 bytes, plus 10 bytes overhead.
func EstimateFee(nInputs, nOutputs int, feeRate uint64) uint64 {
	size := uint64(nInputs*148 + nOutputs*34 + 10)
	return size * feeRate
}

// SelectUTXOs queries the blockchain service for UTXOs belonging to address
// and selects the minimum set needed to cover amount + estimated fee using a
// greedy largest-first algorithm. Returns HTLCUTXO slices ready for use in
// HTLC funding transactions.
func SelectUTXOs(ctx context.Context, svc network.BlockchainService, address string, amount, feeRate uint64) ([]*x402.HTLCUTXO, error) {
	netUTXOs, err := svc.ListUnspent(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("buyer: query UTXOs: %w", err)
	}

	// Sort descending by amount (greedy: pick largest first).
	sort.Slice(netUTXOs, func(i, j int) bool {
		return netUTXOs[i].Amount > netUTXOs[j].Amount
	})

	var selected []*x402.HTLCUTXO
	var totalInput uint64

	for _, u := range netUTXOs {
		txid, decErr := hex.DecodeString(u.TxID)
		if decErr != nil {
			continue
		}
		if len(txid) != 32 {
			continue
		}
		scriptPK, decErr := hex.DecodeString(u.ScriptPubKey)
		if decErr != nil {
			continue
		}

		selected = append(selected, &x402.HTLCUTXO{
			TxID:         txid,
			Vout:         u.Vout,
			Amount:       u.Amount,
			ScriptPubKey: scriptPK,
		})
		totalInput += u.Amount

		// Estimate fee assuming 2 outputs (HTLC + change).
		fee := EstimateFee(len(selected), 2, feeRate)
		if totalInput >= amount+fee {
			return selected, nil
		}
	}

	// Could not cover amount + fee with all available UTXOs.
	var totalAvailable uint64
	for _, u := range netUTXOs {
		totalAvailable += u.Amount
	}
	return nil, fmt.Errorf("%w: need %d sat, have %d sat",
		ErrInsufficientBalance,
		amount+EstimateFee(len(netUTXOs), 2, feeRate),
		totalAvailable)
}
