package buy

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bitfsorg/bitfs/internal/client"
	"github.com/bitfsorg/libbitfs-go/network"
	"github.com/bitfsorg/libbitfs-go/x402"
)

const defaultFeeRate = uint64(1) // 1 sat/byte

// BuyResult holds the result of a successful purchase.
type BuyResult struct {
	Capsule      []byte // Decryption capsule (raw bytes)
	CapsuleNonce []byte // Per-invoice nonce for capsule unlinkability (nil = legacy)
	HTLCTxID     string // HTLC funding transaction ID (hex)
	CostSatoshis uint64 // Total cost including fees
}

// BuyParams holds parameters for the Buy function.
type BuyParams struct {
	Client     *client.Client            // HTTP client for daemon communication
	TxID       string                    // Transaction ID of the paid content
	Config     *BuyerConfig              // Buyer wallet configuration
	Blockchain network.BlockchainService // Optional: for auto UTXO selection
}

func (p *BuyParams) validate() error {
	if p.Config == nil || p.Config.PrivKey == nil {
		return fmt.Errorf("buyer: wallet key is required")
	}
	if p.TxID == "" {
		return fmt.Errorf("buyer: transaction ID is required")
	}
	return nil
}

// Buy executes the full x402 purchase flow:
//  1. GetBuyInfo — fetch capsule_hash, price, payment_addr from the daemon
//  2. Resolve UTXOs — use manual UTXOs if configured, otherwise query blockchain
//  3. Build HTLC funding tx — create and sign the HTLC funding transaction
//  4. SubmitHTLC — send the signed tx to the daemon and receive the capsule
func Buy(params *BuyParams) (*BuyResult, error) {
	if err := params.validate(); err != nil {
		return nil, err
	}

	privKey := params.Config.PrivKey

	// Step 1: Get buy info from daemon.
	buyerPubHex := hex.EncodeToString(privKey.PubKey().Compressed())
	buyInfo, err := params.Client.GetBuyInfo(params.TxID, buyerPubHex)
	if err != nil {
		return nil, fmt.Errorf("buyer: get buy info: %w", err)
	}

	// Decode capsule hash from hex.
	capsuleHash, err := hex.DecodeString(buyInfo.CapsuleHash)
	if err != nil {
		return nil, fmt.Errorf("buyer: invalid capsule hash hex: %w", err)
	}

	// Decode seller payment address (base58 P2PKH) to 20-byte pubkey hash.
	sellerAddrObj, err := script.NewAddressFromString(buyInfo.PaymentAddr)
	if err != nil {
		return nil, fmt.Errorf("buyer: invalid payment address: %w", err)
	}
	sellerAddr := []byte(sellerAddrObj.PublicKeyHash)

	// Decode seller pubkey (33-byte compressed, hex-encoded).
	sellerPubKey, err := hex.DecodeString(buyInfo.SellerPubKey)
	if err != nil {
		return nil, fmt.Errorf("buyer: invalid seller pubkey hex: %w", err)
	}

	// Step 2: Resolve UTXOs.
	utxos, err := resolveUTXOs(params, buyInfo.Price)
	if err != nil {
		return nil, err
	}

	// Step 3: Build HTLC funding transaction.
	buyerPKH := privKey.PubKey().Hash() // 20-byte pubkey hash for change address

	fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
		BuyerPrivKey: privKey,
		SellerAddr:   sellerAddr,
		SellerPubKey: sellerPubKey,
		CapsuleHash:  capsuleHash,
		Amount:       buyInfo.Price,
		Timeout:      x402.DefaultHTLCTimeout,
		UTXOs:        utxos,
		ChangeAddr:   buyerPKH,
		FeeRate:      defaultFeeRate,
	})
	if err != nil {
		return nil, fmt.Errorf("buyer: build HTLC funding tx: %w", err)
	}

	// Step 4: Submit HTLC to daemon and receive capsule.
	capsuleResp, err := params.Client.SubmitHTLC(params.TxID, fundingResult.RawTx)
	if err != nil {
		return nil, fmt.Errorf("buyer: submit HTLC: %w", err)
	}

	// Decode capsule from hex.
	capsule, err := hex.DecodeString(capsuleResp.Capsule)
	if err != nil {
		return nil, fmt.Errorf("buyer: invalid capsule hex in response: %w", err)
	}

	// Decode capsule nonce if present (used for per-purchase unlinkability).
	var capsuleNonce []byte
	if capsuleResp.CapsuleNonce != "" {
		capsuleNonce, err = hex.DecodeString(capsuleResp.CapsuleNonce)
		if err != nil {
			return nil, fmt.Errorf("buyer: invalid capsule nonce hex in response: %w", err)
		}
	}

	// Compute total cost: HTLC amount + fee (total input - change).
	var totalInput uint64
	for _, u := range utxos {
		totalInput += u.Amount
	}
	cost := totalInput // Conservative: total spent from wallet
	if fundingResult.HTLCAmount < totalInput {
		// Fee = totalInput - htlcAmount - changeAmount, but we report total spent.
		cost = totalInput
	}

	return &BuyResult{
		Capsule:      capsule,
		CapsuleNonce: capsuleNonce,
		HTLCTxID:     hex.EncodeToString(fundingResult.TxID),
		CostSatoshis: cost,
	}, nil
}

// resolveUTXOs returns UTXOs for the HTLC funding transaction. It uses
// manually specified UTXOs if available, otherwise queries the blockchain
// service for the buyer's UTXOs.
func resolveUTXOs(params *BuyParams, price uint64) ([]*x402.HTLCUTXO, error) {
	// Prefer manual UTXOs from config (--utxo flag).
	if len(params.Config.ManualUTXOs) > 0 {
		// Validate that manual UTXOs cover the price.
		var total uint64
		for _, u := range params.Config.ManualUTXOs {
			total += u.Amount
		}
		fee := EstimateFee(len(params.Config.ManualUTXOs), 2, defaultFeeRate)
		if total < price+fee {
			return nil, fmt.Errorf("%w: manual UTXOs have %d sat, need %d sat (price=%d + fee~%d)",
				ErrInsufficientBalance, total, price+fee, price, fee)
		}
		return params.Config.ManualUTXOs, nil
	}

	// Auto-select from blockchain.
	if params.Blockchain == nil {
		return nil, fmt.Errorf("buyer: no UTXOs available (provide --utxo or configure blockchain service)")
	}

	// Derive the buyer's P2PKH address (base58) for UTXO lookup.
	addrObj, err := script.NewAddressFromPublicKey(params.Config.PrivKey.PubKey(), params.Config.Network == "mainnet")
	if err != nil {
		return nil, fmt.Errorf("buyer: derive address: %w", err)
	}

	return SelectUTXOs(context.Background(), params.Blockchain, addrObj.AddressString, price, defaultFeeRate)
}
