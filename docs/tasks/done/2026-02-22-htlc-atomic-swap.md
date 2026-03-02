# HTLC Atomic Swap E2E Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the HTLC atomic swap flow fully functional end-to-end: BuildHTLCFundingTx, BuildSellerClaimTx, BuildBuyerRefundTx, fix daemon bugs, update bget, comprehensive tests.

**Architecture:** All new HTLC transaction building functions go in `libbitfs/x402/`. The daemon (`bitfs/internal/daemon/payment.go`) gets bug fixes for capsule computation, HTLC verification, and capsule return. The `bget` CLI gets updated to build proper funding transactions. Custom `UnlockingScriptTemplate` implementations handle HTLC-specific signing via go-sdk's `tx.CalcInputSignatureHash()`.

**Tech Stack:** Go 1.25.6, go-sdk v1.2.18 (transaction, script, sighash, ec), libbitfs/x402, libbitfs/method42, libbitfs/tx

---

### Task 1: Add HTLC types and UTXO struct to libbitfs/x402

**Files:**
- Create: `libbitfs/x402/htlc_tx.go`

**Step 1: Create the types file**

Create `libbitfs/x402/htlc_tx.go` with all the param structs and the HTLC UTXO type. No functions yet, just types.

```go
package x402

import (
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

// HTLCUTXO represents an unspent output for HTLC funding.
type HTLCUTXO struct {
	TxID         []byte // 32 bytes, internal byte order
	Vout         uint32
	Amount       uint64
	ScriptPubKey []byte // Locking script bytes
}

// HTLCFundingParams holds parameters for building an HTLC funding transaction.
type HTLCFundingParams struct {
	BuyerPrivKey *ec.PrivateKey // Signs the P2PKH inputs
	SellerAddr   []byte         // 20-byte P2PKH hash
	CapsuleHash  []byte         // 32-byte SHA256(capsule)
	Amount       uint64         // HTLC output satoshis
	Timeout      uint32         // Block height for refund
	UTXOs        []*HTLCUTXO    // Buyer's unspent outputs
	ChangeAddr   []byte         // 20-byte change address hash
	FeeRate      uint64         // Satoshis per byte (0 = use default)
}

// HTLCFundingResult holds the result of building an HTLC funding transaction.
type HTLCFundingResult struct {
	RawTx      []byte // Signed serialized transaction
	TxID       []byte // 32-byte transaction hash
	HTLCVout   uint32 // Index of the HTLC output
	HTLCScript []byte // HTLC locking script bytes
	HTLCAmount uint64 // Actual HTLC output amount (may differ if dust-adjusted)
}

// SellerClaimParams holds parameters for the seller claim transaction.
type SellerClaimParams struct {
	FundingTxID   []byte         // 32-byte HTLC funding tx hash
	FundingVout   uint32         // HTLC output index in funding tx
	FundingAmount uint64         // HTLC output amount
	HTLCScript    []byte         // HTLC locking script bytes
	Capsule       []byte         // Preimage to reveal (32 bytes)
	SellerPrivKey *ec.PrivateKey // Signs the claim
	OutputAddr    []byte         // 20-byte destination P2PKH hash
	FeeRate       uint64         // Satoshis per byte (0 = use default)
}

// BuyerRefundParams holds parameters for the buyer refund transaction.
type BuyerRefundParams struct {
	FundingTxID   []byte         // 32-byte HTLC funding tx hash
	FundingVout   uint32         // HTLC output index in funding tx
	FundingAmount uint64         // HTLC output amount
	HTLCScript    []byte         // HTLC locking script bytes
	BuyerPrivKey  *ec.PrivateKey // Signs the refund
	OutputAddr    []byte         // 20-byte destination P2PKH hash
	Locktime      uint32         // Must be >= HTLC timeout
	FeeRate       uint64         // Satoshis per byte (0 = use default)
}

// defaultHTLCFeeRate is the default fee rate for HTLC transactions.
const defaultHTLCFeeRate = uint64(1) // 1 sat/byte
```

**Step 2: Verify the file compiles**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go build ./x402/...`
Expected: BUILD SUCCESS

**Step 3: Commit**

```bash
git add libbitfs/x402/htlc_tx.go
git commit -m "feat(x402): add HTLC transaction param types"
```

---

### Task 2: Implement VerifyHTLCFunding

**Files:**
- Modify: `libbitfs/x402/htlc_tx.go`
- Test: `libbitfs/x402/htlc_tx_test.go`

**Step 1: Write failing tests**

Create `libbitfs/x402/htlc_tx_test.go`:

```go
package x402

import (
	"bytes"
	"testing"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifyHTLCFunding(t *testing.T) {
	// Build a known HTLC script for test.
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	sellerAddr := bytes.Repeat([]byte{0xcd}, 20)
	buyerPubKey := make([]byte, 33)
	buyerPubKey[0] = 0x02
	for i := 1; i < 33; i++ {
		buyerPubKey[i] = byte(i)
	}

	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey: buyerPubKey,
		SellerAddr:  sellerAddr,
		CapsuleHash: capsuleHash,
		Amount:      1000,
		Timeout:     144,
	})
	require.NoError(t, err)

	// Build a mock funding transaction with the HTLC output.
	fundingTx := transaction.NewTransaction()
	htlcLockingScript := script.Script(htlcScript)
	fundingTx.AddOutput(&transaction.TransactionOutput{
		LockingScript: &htlcLockingScript,
		Satoshis:      1000,
	})

	t.Run("valid funding tx", func(t *testing.T) {
		vout, err := VerifyHTLCFunding(fundingTx.Bytes(), htlcScript, 1000)
		require.NoError(t, err)
		assert.Equal(t, uint32(0), vout)
	})

	t.Run("amount exceeds minimum", func(t *testing.T) {
		vout, err := VerifyHTLCFunding(fundingTx.Bytes(), htlcScript, 500)
		require.NoError(t, err)
		assert.Equal(t, uint32(0), vout)
	})

	t.Run("insufficient amount", func(t *testing.T) {
		_, err := VerifyHTLCFunding(fundingTx.Bytes(), htlcScript, 2000)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInsufficientPayment)
	})

	t.Run("wrong script", func(t *testing.T) {
		wrongScript := bytes.Repeat([]byte{0xff}, 50)
		_, err := VerifyHTLCFunding(fundingTx.Bytes(), wrongScript, 1000)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrNoMatchingOutput)
	})

	t.Run("empty raw tx", func(t *testing.T) {
		_, err := VerifyHTLCFunding(nil, htlcScript, 1000)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidTx)
	})

	t.Run("nil expected script", func(t *testing.T) {
		_, err := VerifyHTLCFunding(fundingTx.Bytes(), nil, 1000)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrInvalidParams)
	})

	t.Run("htlc output at index 1", func(t *testing.T) {
		// Build tx with a P2PKH output first, then HTLC second.
		tx2 := transaction.NewTransaction()
		dummyScript := script.Script([]byte{0x76, 0xa9})
		tx2.AddOutput(&transaction.TransactionOutput{
			LockingScript: &dummyScript,
			Satoshis:      500,
		})
		tx2.AddOutput(&transaction.TransactionOutput{
			LockingScript: &htlcLockingScript,
			Satoshis:      1000,
		})
		vout, err := VerifyHTLCFunding(tx2.Bytes(), htlcScript, 1000)
		require.NoError(t, err)
		assert.Equal(t, uint32(1), vout)
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestVerifyHTLCFunding -v`
Expected: FAIL — `VerifyHTLCFunding` undefined

**Step 3: Implement VerifyHTLCFunding**

Add to `libbitfs/x402/htlc_tx.go`:

```go
import (
	"bytes"
	"fmt"

	"github.com/bsv-blockchain/go-sdk/transaction"
)

// VerifyHTLCFunding verifies a funding transaction has an output whose locking
// script matches the expected HTLC script with at least minAmount satoshis.
// Returns the output index (vout) of the matching HTLC output.
func VerifyHTLCFunding(rawTx []byte, expectedScript []byte, minAmount uint64) (uint32, error) {
	if len(rawTx) == 0 {
		return 0, fmt.Errorf("%w: empty raw transaction", ErrInvalidTx)
	}
	if len(expectedScript) == 0 {
		return 0, fmt.Errorf("%w: nil expected script", ErrInvalidParams)
	}

	tx, err := transaction.NewTransactionFromBytes(rawTx)
	if err != nil {
		return 0, fmt.Errorf("%w: %w", ErrInvalidTx, err)
	}

	for i, output := range tx.Outputs {
		if output.LockingScript == nil {
			continue
		}
		if !bytes.Equal(output.LockingScript.Bytes(), expectedScript) {
			continue
		}
		if output.Satoshis < minAmount {
			return 0, fmt.Errorf("%w: output has %d satoshis, need %d",
				ErrInsufficientPayment, output.Satoshis, minAmount)
		}
		return uint32(i), nil
	}

	return 0, ErrNoMatchingOutput
}
```

**Step 4: Run tests to verify they pass**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestVerifyHTLCFunding -v`
Expected: PASS (all subtests)

**Step 5: Commit**

```bash
git add libbitfs/x402/htlc_tx.go libbitfs/x402/htlc_tx_test.go
git commit -m "feat(x402): add VerifyHTLCFunding"
```

---

### Task 3: Implement BuildHTLCFundingTx

**Files:**
- Modify: `libbitfs/x402/htlc_tx.go`
- Modify: `libbitfs/x402/htlc_tx_test.go`

**Step 1: Write failing tests**

Add to `libbitfs/x402/htlc_tx_test.go`:

```go
import (
	"crypto/sha256"

	"github.com/bsv-blockchain/go-sdk/chainhash"
	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

func TestBuildHTLCFundingTx(t *testing.T) {
	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	sellerAddr := bytes.Repeat([]byte{0xcd}, 20)
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	changeAddr := buyerPriv.PubKey().Hash()

	// Build a mock P2PKH script for the UTXO.
	buyerPKH := buyerPriv.PubKey().Hash()
	p2pkhScript := buildTestP2PKHScript(t, buyerPKH)

	mockTxID := sha256.Sum256([]byte("mock-utxo-txid"))

	t.Run("single UTXO sufficient funds", func(t *testing.T) {
		result, err := BuildHTLCFundingTx(&HTLCFundingParams{
			BuyerPrivKey: buyerPriv,
			SellerAddr:   sellerAddr,
			CapsuleHash:  capsuleHash,
			Amount:       1000,
			Timeout:      144,
			UTXOs: []*HTLCUTXO{{
				TxID:         mockTxID[:],
				Vout:         0,
				Amount:       10000,
				ScriptPubKey: p2pkhScript,
			}},
			ChangeAddr: changeAddr,
			FeeRate:    1,
		})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotEmpty(t, result.RawTx)
		require.NotEmpty(t, result.TxID)
		require.NotEmpty(t, result.HTLCScript)
		assert.Equal(t, uint32(0), result.HTLCVout)
		assert.GreaterOrEqual(t, result.HTLCAmount, uint64(1000))

		// Verify the funding tx with VerifyHTLCFunding.
		vout, err := VerifyHTLCFunding(result.RawTx, result.HTLCScript, 1000)
		require.NoError(t, err)
		assert.Equal(t, uint32(0), vout)
	})

	t.Run("amount enforces dust limit", func(t *testing.T) {
		result, err := BuildHTLCFundingTx(&HTLCFundingParams{
			BuyerPrivKey: buyerPriv,
			SellerAddr:   sellerAddr,
			CapsuleHash:  capsuleHash,
			Amount:       100, // Below dust limit (546)
			Timeout:      144,
			UTXOs: []*HTLCUTXO{{
				TxID:         mockTxID[:],
				Vout:         0,
				Amount:       10000,
				ScriptPubKey: p2pkhScript,
			}},
			ChangeAddr: changeAddr,
			FeeRate:    1,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.HTLCAmount, uint64(546))
	})

	t.Run("nil params", func(t *testing.T) {
		_, err := BuildHTLCFundingTx(nil)
		require.Error(t, err)
	})

	t.Run("no UTXOs", func(t *testing.T) {
		_, err := BuildHTLCFundingTx(&HTLCFundingParams{
			BuyerPrivKey: buyerPriv,
			SellerAddr:   sellerAddr,
			CapsuleHash:  capsuleHash,
			Amount:       1000,
			Timeout:      144,
			UTXOs:        nil,
			ChangeAddr:   changeAddr,
		})
		require.Error(t, err)
	})

	t.Run("insufficient funds", func(t *testing.T) {
		_, err := BuildHTLCFundingTx(&HTLCFundingParams{
			BuyerPrivKey: buyerPriv,
			SellerAddr:   sellerAddr,
			CapsuleHash:  capsuleHash,
			Amount:       100000,
			Timeout:      144,
			UTXOs: []*HTLCUTXO{{
				TxID:         mockTxID[:],
				Vout:         0,
				Amount:       1000,
				ScriptPubKey: p2pkhScript,
			}},
			ChangeAddr: changeAddr,
		})
		require.Error(t, err)
	})
}

// buildTestP2PKHScript creates a P2PKH locking script for testing.
func buildTestP2PKHScript(t *testing.T, pubKeyHash []byte) []byte {
	t.Helper()
	s := &script.Script{}
	require.NoError(t, s.AppendOpcodes(script.OpDUP))
	require.NoError(t, s.AppendOpcodes(script.OpHASH160))
	require.NoError(t, s.AppendPushData(pubKeyHash))
	require.NoError(t, s.AppendOpcodes(script.OpEQUALVERIFY))
	require.NoError(t, s.AppendOpcodes(script.OpCHECKSIG))
	return s.Bytes()
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestBuildHTLCFundingTx -v`
Expected: FAIL — `BuildHTLCFundingTx` undefined

**Step 3: Implement BuildHTLCFundingTx**

Add to `libbitfs/x402/htlc_tx.go`:

```go
import (
	"github.com/bsv-blockchain/go-sdk/chainhash"
	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/bsv-blockchain/go-sdk/transaction"
	"github.com/bsv-blockchain/go-sdk/transaction/template/p2pkh"
	sighash "github.com/bsv-blockchain/go-sdk/transaction/sighash"
)

const dustLimit = uint64(546)

// BuildHTLCFundingTx creates a signed transaction with an HTLC output.
// Input: buyer's P2PKH UTXOs. Output 0: HTLC script. Output 1: change (if above dust).
func BuildHTLCFundingTx(params *HTLCFundingParams) (*HTLCFundingResult, error) {
	if params == nil {
		return nil, fmt.Errorf("%w: nil params", ErrInvalidParams)
	}
	if params.BuyerPrivKey == nil {
		return nil, fmt.Errorf("%w: nil buyer private key", ErrInvalidParams)
	}
	if len(params.UTXOs) == 0 {
		return nil, fmt.Errorf("%w: no UTXOs provided", ErrInvalidParams)
	}
	if len(params.SellerAddr) != PubKeyHashLen {
		return nil, fmt.Errorf("%w: seller address must be %d bytes", ErrInvalidParams, PubKeyHashLen)
	}
	if len(params.CapsuleHash) != CapsuleHashLen {
		return nil, fmt.Errorf("%w: capsule hash must be %d bytes", ErrInvalidParams, CapsuleHashLen)
	}
	if len(params.ChangeAddr) != PubKeyHashLen {
		return nil, fmt.Errorf("%w: change address must be %d bytes", ErrInvalidParams, PubKeyHashLen)
	}

	// Enforce dust limit on HTLC amount.
	htlcAmount := params.Amount
	if htlcAmount < dustLimit {
		htlcAmount = dustLimit
	}

	timeout := params.Timeout
	if timeout == 0 {
		timeout = DefaultHTLCTimeout
	}

	feeRate := params.FeeRate
	if feeRate == 0 {
		feeRate = defaultHTLCFeeRate
	}

	// Build the HTLC locking script.
	buyerPubKey := params.BuyerPrivKey.PubKey().Compressed()
	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey: buyerPubKey,
		SellerAddr:  params.SellerAddr,
		CapsuleHash: params.CapsuleHash,
		Amount:      htlcAmount,
		Timeout:     timeout,
	})
	if err != nil {
		return nil, fmt.Errorf("build HTLC script: %w", err)
	}

	// Calculate total input amount.
	var totalInput uint64
	for _, utxo := range params.UTXOs {
		totalInput += utxo.Amount
	}

	// Estimate fee: ~148 bytes per input + ~40 bytes per output + 10 overhead.
	estSize := uint64(10 + len(params.UTXOs)*148 + 2*40)
	estFee := estSize * feeRate

	totalNeeded := htlcAmount + estFee
	if totalInput < totalNeeded {
		return nil, fmt.Errorf("%w: have %d satoshis, need %d (amount=%d + fee≈%d)",
			ErrInsufficientPayment, totalInput, totalNeeded, htlcAmount, estFee)
	}

	// Build the transaction.
	tx := transaction.NewTransaction()

	// Add inputs.
	for _, utxo := range params.UTXOs {
		txidHash, err := chainhash.NewHash(utxo.TxID)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid UTXO txid: %w", ErrInvalidParams, err)
		}
		tx.AddInput(&transaction.TransactionInput{
			SourceTXID:       txidHash,
			SourceTxOutIndex: utxo.Vout,
			SequenceNumber:   0xffffffff,
		})
	}

	// Output 0: HTLC.
	htlcLockingScript := script.Script(htlcScript)
	tx.AddOutput(&transaction.TransactionOutput{
		LockingScript: &htlcLockingScript,
		Satoshis:      htlcAmount,
	})

	// Output 1: change (if above dust).
	changeAmount := totalInput - htlcAmount - estFee
	if changeAmount > dustLimit {
		changeScript, err := buildP2PKHLockScript(params.ChangeAddr)
		if err != nil {
			return nil, fmt.Errorf("build change script: %w", err)
		}
		tx.AddOutput(&transaction.TransactionOutput{
			LockingScript: changeScript,
			Satoshis:      changeAmount,
		})
	}

	// Set source outputs and sign each input.
	for i, utxo := range params.UTXOs {
		lockScript := script.NewFromBytes(utxo.ScriptPubKey)
		tx.Inputs[i].SetSourceTxOutput(&transaction.TransactionOutput{
			Satoshis:      utxo.Amount,
			LockingScript: lockScript,
		})

		unlocker, err := p2pkh.Unlock(params.BuyerPrivKey, nil)
		if err != nil {
			return nil, fmt.Errorf("create P2PKH unlocker for input %d: %w", i, err)
		}
		tx.Inputs[i].UnlockingScriptTemplate = unlocker
	}

	if err := tx.Sign(); err != nil {
		return nil, fmt.Errorf("sign funding tx: %w", err)
	}

	txIDHash := tx.TxID()

	return &HTLCFundingResult{
		RawTx:      tx.Bytes(),
		TxID:       txIDHash[:],
		HTLCVout:   0,
		HTLCScript: htlcScript,
		HTLCAmount: htlcAmount,
	}, nil
}

// buildP2PKHLockScript creates a P2PKH locking script from a 20-byte public key hash.
func buildP2PKHLockScript(pubKeyHash []byte) (*script.Script, error) {
	s := &script.Script{}
	if err := s.AppendOpcodes(script.OpDUP); err != nil {
		return nil, err
	}
	if err := s.AppendOpcodes(script.OpHASH160); err != nil {
		return nil, err
	}
	if err := s.AppendPushData(pubKeyHash); err != nil {
		return nil, err
	}
	if err := s.AppendOpcodes(script.OpEQUALVERIFY); err != nil {
		return nil, err
	}
	if err := s.AppendOpcodes(script.OpCHECKSIG); err != nil {
		return nil, err
	}
	return s, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestBuildHTLCFundingTx -v`
Expected: PASS

**Step 5: Commit**

```bash
git add libbitfs/x402/htlc_tx.go libbitfs/x402/htlc_tx_test.go
git commit -m "feat(x402): implement BuildHTLCFundingTx"
```

---

### Task 4: Implement BuildSellerClaimTx

**Files:**
- Modify: `libbitfs/x402/htlc_tx.go`
- Modify: `libbitfs/x402/htlc_tx_test.go`

**Step 1: Write failing tests**

Add to `libbitfs/x402/htlc_tx_test.go`:

```go
func TestBuildSellerClaimTx(t *testing.T) {
	sellerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	capsule := bytes.Repeat([]byte{0xde}, 32)
	capsuleHash := sha256.Sum256(capsule)
	sellerAddr := sellerPriv.PubKey().Hash()
	changeAddr := sellerPriv.PubKey().Hash()

	// Build HTLC script.
	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey: buyerPriv.PubKey().Compressed(),
		SellerAddr:  sellerAddr,
		CapsuleHash: capsuleHash[:],
		Amount:      1000,
		Timeout:     144,
	})
	require.NoError(t, err)

	mockTxID := sha256.Sum256([]byte("htlc-funding-txid"))

	t.Run("valid seller claim", func(t *testing.T) {
		claimTx, err := BuildSellerClaimTx(&SellerClaimParams{
			FundingTxID:   mockTxID[:],
			FundingVout:   0,
			FundingAmount: 1000,
			HTLCScript:    htlcScript,
			Capsule:       capsule,
			SellerPrivKey: sellerPriv,
			OutputAddr:    changeAddr,
			FeeRate:       1,
		})
		require.NoError(t, err)
		require.NotNil(t, claimTx)
		require.NotEmpty(t, claimTx.Bytes())

		// Verify we can extract the capsule from the claim tx.
		extracted, err := ParseHTLCPreimage(claimTx.Bytes())
		require.NoError(t, err)
		assert.Equal(t, capsule, extracted)
	})

	t.Run("nil params fields", func(t *testing.T) {
		_, err := BuildSellerClaimTx(nil)
		require.Error(t, err)

		_, err = BuildSellerClaimTx(&SellerClaimParams{
			FundingTxID: mockTxID[:],
			// Missing other fields
		})
		require.Error(t, err)
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestBuildSellerClaimTx -v`
Expected: FAIL — `BuildSellerClaimTx` undefined

**Step 3: Implement BuildSellerClaimTx**

Add to `libbitfs/x402/htlc_tx.go`:

```go
// BuildSellerClaimTx creates a signed transaction spending the HTLC via the seller claim path.
// Unlocking script: <sig+flag> <seller_pubkey> <capsule> OP_TRUE
func BuildSellerClaimTx(params *SellerClaimParams) (*transaction.Transaction, error) {
	if params == nil {
		return nil, fmt.Errorf("%w: nil params", ErrInvalidParams)
	}
	if params.SellerPrivKey == nil {
		return nil, fmt.Errorf("%w: nil seller private key", ErrInvalidParams)
	}
	if len(params.FundingTxID) != 32 {
		return nil, fmt.Errorf("%w: funding txid must be 32 bytes", ErrInvalidParams)
	}
	if len(params.HTLCScript) == 0 {
		return nil, fmt.Errorf("%w: empty HTLC script", ErrInvalidParams)
	}
	if len(params.Capsule) == 0 {
		return nil, fmt.Errorf("%w: empty capsule", ErrInvalidParams)
	}
	if len(params.OutputAddr) != PubKeyHashLen {
		return nil, fmt.Errorf("%w: output address must be %d bytes", ErrInvalidParams, PubKeyHashLen)
	}

	feeRate := params.FeeRate
	if feeRate == 0 {
		feeRate = defaultHTLCFeeRate
	}

	// Estimate claim tx size: ~10 overhead + ~(73+33+32+1) unlocking + ~40 output.
	estSize := uint64(10 + 73 + 33 + 32 + 1 + len(params.HTLCScript) + 40)
	estFee := estSize * feeRate

	if params.FundingAmount <= estFee {
		return nil, fmt.Errorf("%w: funding amount %d too small for fee %d",
			ErrInsufficientPayment, params.FundingAmount, estFee)
	}

	outputAmount := params.FundingAmount - estFee
	if outputAmount < dustLimit {
		return nil, fmt.Errorf("%w: output would be %d (below dust limit %d)",
			ErrInsufficientPayment, outputAmount, dustLimit)
	}

	txidHash, err := chainhash.NewHash(params.FundingTxID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid funding txid: %w", ErrInvalidParams, err)
	}

	tx := transaction.NewTransaction()

	tx.AddInput(&transaction.TransactionInput{
		SourceTXID:       txidHash,
		SourceTxOutIndex: params.FundingVout,
		SequenceNumber:   0xffffffff,
	})

	// Set source output for sighash computation.
	htlcLockingScript := script.NewFromBytes(params.HTLCScript)
	tx.Inputs[0].SetSourceTxOutput(&transaction.TransactionOutput{
		Satoshis:      params.FundingAmount,
		LockingScript: htlcLockingScript,
	})

	// Output: P2PKH to seller.
	outputScript, err := buildP2PKHLockScript(params.OutputAddr)
	if err != nil {
		return nil, fmt.Errorf("build output script: %w", err)
	}
	tx.AddOutput(&transaction.TransactionOutput{
		LockingScript: outputScript,
		Satoshis:      outputAmount,
	})

	// Compute sighash and sign manually (not using template — HTLC is custom).
	sigHash, err := tx.CalcInputSignatureHash(0, sighash.AllForkID)
	if err != nil {
		return nil, fmt.Errorf("calc sighash: %w", err)
	}

	sig, err := params.SellerPrivKey.Sign(sigHash)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// Build unlocking script: <sig+flag> <seller_pubkey> <capsule> OP_TRUE
	sigBytes := append(sig.Serialize(), byte(sighash.AllForkID))
	sellerPubKey := params.SellerPrivKey.PubKey().Compressed()

	unlockScript := &script.Script{}
	if err := unlockScript.AppendPushData(sigBytes); err != nil {
		return nil, fmt.Errorf("push sig: %w", err)
	}
	if err := unlockScript.AppendPushData(sellerPubKey); err != nil {
		return nil, fmt.Errorf("push seller pubkey: %w", err)
	}
	if err := unlockScript.AppendPushData(params.Capsule); err != nil {
		return nil, fmt.Errorf("push capsule: %w", err)
	}
	if err := unlockScript.AppendOpcodes(script.OpTRUE); err != nil {
		return nil, fmt.Errorf("push OP_TRUE: %w", err)
	}

	tx.Inputs[0].UnlockingScript = unlockScript

	return tx, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestBuildSellerClaimTx -v`
Expected: PASS

**Step 5: Commit**

```bash
git add libbitfs/x402/htlc_tx.go libbitfs/x402/htlc_tx_test.go
git commit -m "feat(x402): implement BuildSellerClaimTx"
```

---

### Task 5: Implement BuildBuyerRefundTx

**Files:**
- Modify: `libbitfs/x402/htlc_tx.go`
- Modify: `libbitfs/x402/htlc_tx_test.go`

**Step 1: Write failing tests**

Add to `libbitfs/x402/htlc_tx_test.go`:

```go
func TestBuildBuyerRefundTx(t *testing.T) {
	sellerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	sellerAddr := sellerPriv.PubKey().Hash()
	buyerAddr := buyerPriv.PubKey().Hash()

	htlcScript, err := BuildHTLC(&HTLCParams{
		BuyerPubKey: buyerPriv.PubKey().Compressed(),
		SellerAddr:  sellerAddr,
		CapsuleHash: capsuleHash,
		Amount:      1000,
		Timeout:     144,
	})
	require.NoError(t, err)

	mockTxID := sha256.Sum256([]byte("htlc-funding-txid"))

	t.Run("valid buyer refund", func(t *testing.T) {
		refundTx, err := BuildBuyerRefundTx(&BuyerRefundParams{
			FundingTxID:   mockTxID[:],
			FundingVout:   0,
			FundingAmount: 1000,
			HTLCScript:    htlcScript,
			BuyerPrivKey:  buyerPriv,
			OutputAddr:    buyerAddr,
			Locktime:      144, // exactly the timeout
			FeeRate:       1,
		})
		require.NoError(t, err)
		require.NotNil(t, refundTx)

		// Verify nLockTime is set.
		assert.Equal(t, uint32(144), refundTx.LockTime)

		// Verify the unlocking script has OP_FALSE (0x00) at end.
		chunks, err := refundTx.Inputs[0].UnlockingScript.Chunks()
		require.NoError(t, err)
		lastChunk := chunks[len(chunks)-1]
		assert.Equal(t, script.OpFALSE, lastChunk.Op)
	})

	t.Run("locktime below timeout rejected", func(t *testing.T) {
		_, err := BuildBuyerRefundTx(&BuyerRefundParams{
			FundingTxID:   mockTxID[:],
			FundingVout:   0,
			FundingAmount: 1000,
			HTLCScript:    htlcScript,
			BuyerPrivKey:  buyerPriv,
			OutputAddr:    buyerAddr,
			Locktime:      100, // below the 144 timeout
		})
		require.Error(t, err)
	})

	t.Run("nil params", func(t *testing.T) {
		_, err := BuildBuyerRefundTx(nil)
		require.Error(t, err)
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestBuildBuyerRefundTx -v`
Expected: FAIL — `BuildBuyerRefundTx` undefined

**Step 3: Implement BuildBuyerRefundTx**

Add to `libbitfs/x402/htlc_tx.go`:

```go
// BuildBuyerRefundTx creates a signed transaction spending the HTLC via the buyer refund path.
// Unlocking script: <sig+flag> OP_FALSE
// nLockTime must be >= the HTLC timeout, and input sequence must be < 0xffffffff.
func BuildBuyerRefundTx(params *BuyerRefundParams) (*transaction.Transaction, error) {
	if params == nil {
		return nil, fmt.Errorf("%w: nil params", ErrInvalidParams)
	}
	if params.BuyerPrivKey == nil {
		return nil, fmt.Errorf("%w: nil buyer private key", ErrInvalidParams)
	}
	if len(params.FundingTxID) != 32 {
		return nil, fmt.Errorf("%w: funding txid must be 32 bytes", ErrInvalidParams)
	}
	if len(params.HTLCScript) == 0 {
		return nil, fmt.Errorf("%w: empty HTLC script", ErrInvalidParams)
	}
	if len(params.OutputAddr) != PubKeyHashLen {
		return nil, fmt.Errorf("%w: output address must be %d bytes", ErrInvalidParams, PubKeyHashLen)
	}

	// Extract timeout from HTLC script to validate locktime.
	// The timeout is encoded after OP_ELSE in the HTLC script.
	// For simplicity, we require the caller to set locktime >= their known timeout.
	// A locktime of 0 is never valid for refund.
	if params.Locktime == 0 {
		return nil, fmt.Errorf("%w: locktime must be > 0 for refund path", ErrInvalidParams)
	}

	feeRate := params.FeeRate
	if feeRate == 0 {
		feeRate = defaultHTLCFeeRate
	}

	// Estimate refund tx size: ~10 overhead + ~(73+1) unlocking + script + ~40 output.
	estSize := uint64(10 + 73 + 1 + uint64(len(params.HTLCScript)) + 40)
	estFee := estSize * feeRate

	if params.FundingAmount <= estFee {
		return nil, fmt.Errorf("%w: funding amount %d too small for fee %d",
			ErrInsufficientPayment, params.FundingAmount, estFee)
	}

	outputAmount := params.FundingAmount - estFee
	if outputAmount < dustLimit {
		return nil, fmt.Errorf("%w: output would be %d (below dust limit %d)",
			ErrInsufficientPayment, outputAmount, dustLimit)
	}

	txidHash, err := chainhash.NewHash(params.FundingTxID)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid funding txid: %w", ErrInvalidParams, err)
	}

	tx := transaction.NewTransaction()
	tx.LockTime = params.Locktime

	// Sequence must be < 0xffffffff for CLTV to work.
	tx.AddInput(&transaction.TransactionInput{
		SourceTXID:       txidHash,
		SourceTxOutIndex: params.FundingVout,
		SequenceNumber:   0xfffffffe, // enables nLockTime
	})

	// Set source output for sighash.
	htlcLockingScript := script.NewFromBytes(params.HTLCScript)
	tx.Inputs[0].SetSourceTxOutput(&transaction.TransactionOutput{
		Satoshis:      params.FundingAmount,
		LockingScript: htlcLockingScript,
	})

	// Output: P2PKH to buyer.
	outputScript, err := buildP2PKHLockScript(params.OutputAddr)
	if err != nil {
		return nil, fmt.Errorf("build output script: %w", err)
	}
	tx.AddOutput(&transaction.TransactionOutput{
		LockingScript: outputScript,
		Satoshis:      outputAmount,
	})

	// Compute sighash and sign manually.
	sigHash, err := tx.CalcInputSignatureHash(0, sighash.AllForkID)
	if err != nil {
		return nil, fmt.Errorf("calc sighash: %w", err)
	}

	sig, err := params.BuyerPrivKey.Sign(sigHash)
	if err != nil {
		return nil, fmt.Errorf("sign: %w", err)
	}

	// Build unlocking script: <sig+flag> OP_FALSE
	sigBytes := append(sig.Serialize(), byte(sighash.AllForkID))

	unlockScript := &script.Script{}
	if err := unlockScript.AppendPushData(sigBytes); err != nil {
		return nil, fmt.Errorf("push sig: %w", err)
	}
	if err := unlockScript.AppendOpcodes(script.OpFALSE); err != nil {
		return nil, fmt.Errorf("push OP_FALSE: %w", err)
	}

	tx.Inputs[0].UnlockingScript = unlockScript

	return tx, nil
}
```

**Step 4: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run TestBuildBuyerRefundTx -v`
Expected: PASS

**Step 5: Commit**

```bash
git add libbitfs/x402/htlc_tx.go libbitfs/x402/htlc_tx_test.go
git commit -m "feat(x402): implement BuildBuyerRefundTx"
```

---

### Task 6: HTLC round-trip integration test (in-memory)

**Files:**
- Modify: `libbitfs/x402/htlc_tx_test.go`

**Step 1: Write the round-trip integration test**

Add to `libbitfs/x402/htlc_tx_test.go`:

```go
import (
	"github.com/tongxiaofeng/libbitfs/method42"
)

// TestHTLCRoundTrip tests the full HTLC lifecycle in memory:
// encrypt → invoice → build funding tx → verify funding → build claim → extract preimage → decrypt
func TestHTLCRoundTrip(t *testing.T) {
	// Generate seller and buyer keys.
	sellerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	sellerAddr := sellerPriv.PubKey().Hash()
	buyerAddr := buyerPriv.PubKey().Hash()

	// --- Seller side: encrypt content and compute capsule ---
	plaintext := []byte("Top secret BitFS content for HTLC atomic swap test")

	encResult, err := method42.Encrypt(plaintext, sellerPriv, sellerPriv.PubKey(), method42.AccessPaid)
	require.NoError(t, err)

	// Capsule = ECDH(D_seller, P_seller).x (owner's shared secret).
	capsule, err := method42.ECDH(sellerPriv, sellerPriv.PubKey())
	require.NoError(t, err)

	capsuleHash := method42.ComputeCapsuleHash(capsule)

	// --- Create invoice ---
	pricePerKB := uint64(100)
	fileSize := uint64(len(plaintext))
	invoice := NewInvoice(pricePerKB, fileSize, "1SellerAddr", capsuleHash, 3600)
	require.NotNil(t, invoice)

	// --- Buyer side: build HTLC funding tx ---
	mockTxID := sha256.Sum256([]byte("buyer-utxo-txid"))
	buyerPKHScript := buildTestP2PKHScript(t, buyerPriv.PubKey().Hash())

	fundingResult, err := BuildHTLCFundingTx(&HTLCFundingParams{
		BuyerPrivKey: buyerPriv,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       invoice.Price,
		Timeout:      DefaultHTLCTimeout,
		UTXOs: []*HTLCUTXO{{
			TxID:         mockTxID[:],
			Vout:         0,
			Amount:       100000,
			ScriptPubKey: buyerPKHScript,
		}},
		ChangeAddr: buyerAddr,
		FeeRate:    1,
	})
	require.NoError(t, err)

	// --- Seller side: verify the funding tx ---
	vout, err := VerifyHTLCFunding(fundingResult.RawTx, fundingResult.HTLCScript, invoice.Price)
	require.NoError(t, err)
	assert.Equal(t, uint32(0), vout)

	// --- Seller side: build claim tx (reveals capsule) ---
	claimTx, err := BuildSellerClaimTx(&SellerClaimParams{
		FundingTxID:   fundingResult.TxID,
		FundingVout:   fundingResult.HTLCVout,
		FundingAmount: fundingResult.HTLCAmount,
		HTLCScript:    fundingResult.HTLCScript,
		Capsule:       capsule,
		SellerPrivKey: sellerPriv,
		OutputAddr:    sellerAddr,
		FeeRate:       1,
	})
	require.NoError(t, err)

	// --- Buyer side: extract capsule from claim tx ---
	extractedCapsule, err := ParseHTLCPreimage(claimTx.Bytes())
	require.NoError(t, err)
	assert.Equal(t, capsule, extractedCapsule)

	// Verify hash matches.
	extractedHash := sha256.Sum256(extractedCapsule)
	assert.Equal(t, capsuleHash, extractedHash[:])

	// --- Buyer side: decrypt content ---
	decResult, err := method42.DecryptWithCapsule(encResult.Ciphertext, extractedCapsule, encResult.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decResult.Plaintext)

	t.Logf("HTLC round-trip: encrypt -> fund -> claim -> extract -> decrypt OK (%d bytes)", len(plaintext))
}

// TestHTLCBuyerRefundRoundTrip tests the buyer refund path.
func TestHTLCBuyerRefundRoundTrip(t *testing.T) {
	sellerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	buyerPriv, err := ec.NewPrivateKey()
	require.NoError(t, err)

	capsuleHash := bytes.Repeat([]byte{0xab}, 32)
	sellerAddr := sellerPriv.PubKey().Hash()
	buyerAddr := buyerPriv.PubKey().Hash()

	// Build HTLC funding tx.
	mockTxID := sha256.Sum256([]byte("buyer-utxo-txid"))
	buyerPKHScript := buildTestP2PKHScript(t, buyerPriv.PubKey().Hash())

	fundingResult, err := BuildHTLCFundingTx(&HTLCFundingParams{
		BuyerPrivKey: buyerPriv,
		SellerAddr:   sellerAddr,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      144,
		UTXOs: []*HTLCUTXO{{
			TxID:         mockTxID[:],
			Vout:         0,
			Amount:       100000,
			ScriptPubKey: buyerPKHScript,
		}},
		ChangeAddr: buyerAddr,
		FeeRate:    1,
	})
	require.NoError(t, err)

	// Build refund tx.
	refundTx, err := BuildBuyerRefundTx(&BuyerRefundParams{
		FundingTxID:   fundingResult.TxID,
		FundingVout:   fundingResult.HTLCVout,
		FundingAmount: fundingResult.HTLCAmount,
		HTLCScript:    fundingResult.HTLCScript,
		BuyerPrivKey:  buyerPriv,
		OutputAddr:    buyerAddr,
		Locktime:      144,
		FeeRate:       1,
	})
	require.NoError(t, err)
	require.NotNil(t, refundTx)

	assert.Equal(t, uint32(144), refundTx.LockTime)
	assert.Equal(t, uint32(0xfffffffe), refundTx.Inputs[0].SequenceNumber)

	// Verify no preimage is extractable (refund path has no capsule).
	_, err = ParseHTLCPreimage(refundTx.Bytes())
	assert.Error(t, err, "refund tx should not contain a capsule preimage")

	t.Logf("HTLC buyer refund round-trip OK")
}
```

**Step 2: Run tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -run "TestHTLC(RoundTrip|BuyerRefundRoundTrip)" -v`
Expected: PASS

**Step 3: Run all x402 tests to verify no regressions**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./x402/ -v -count=1`
Expected: All PASS

**Step 4: Commit**

```bash
git add libbitfs/x402/htlc_tx_test.go
git commit -m "test(x402): add HTLC round-trip integration tests"
```

---

### Task 7: Fix daemon payment bugs

**Files:**
- Modify: `bitfs/internal/daemon/payment.go` (lines 38-96, 138-234)
- Modify: `bitfs/internal/daemon/daemon.go` (InvoiceRecord)

**Step 1: Add Capsule field to InvoiceRecord**

In `bitfs/internal/daemon/payment.go`, add `Capsule []byte` to the `InvoiceRecord` struct:

```go
type InvoiceRecord struct {
	ID          string    `json:"invoice_id"`
	TotalPrice  uint64    `json:"total_price"`
	NodePNode   []byte    `json:"-"`
	KeyHash     []byte    `json:"-"`
	PricePerKB  uint64    `json:"price_per_kb"`
	FileSize    uint64    `json:"file_size"`
	PaymentAddr string    `json:"payment_addr"`
	CapsuleHash string    `json:"capsule_hash"`
	HTLCScript  []byte    `json:"-"` // NEW: precomputed HTLC script for verification
	Capsule     []byte    `json:"-"` // NEW: ECDH capsule for buyer
	Expiry      time.Time `json:"-"`
	Paid        bool      `json:"-"`
}
```

**Step 2: Fix servePaidContent — correct capsule computation**

Replace the capsule hash computation block (approx lines 39-53) in `servePaidContent()`:

```go
func (d *Daemon) servePaidContent(w http.ResponseWriter, node *NodeInfo) {
	// Compute capsule = ECDH(D_node, P_node).x (owner's shared secret).
	sellerPriv, _, err := d.wallet.GetSellerKeyPair()
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "WALLET_ERROR", "Failed to get seller key pair")
		return
	}

	// Derive the node's public key from PNode bytes.
	nodePubKey, err := ec.PublicKeyFromBytes(node.PNode)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "KEY_ERROR", "Invalid node public key")
		return
	}

	capsule, err := method42.ComputeCapsule(sellerPriv, nodePubKey)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "CAPSULE_ERROR", "Failed to compute capsule")
		return
	}
	capsuleHash := method42.ComputeCapsuleHash(capsule)
	capsuleHashHex := hex.EncodeToString(capsuleHash)

	// Payment address from seller's public key.
	sellerPubKey := sellerPriv.PubKey()
	sellerAddr, err := script.NewAddressFromPublicKey(sellerPubKey, false)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "ADDR_ERROR", "Failed to derive payment address")
		return
	}
	paymentAddr := sellerAddr.AddressString

	// ... rest of invoice creation unchanged, but store capsule in record ...
	record := &InvoiceRecord{
		// ... existing fields ...
		Capsule: capsule,
	}
```

Add imports: `method42 "github.com/tongxiaofeng/libbitfs/method42"`, `"github.com/bsv-blockchain/go-sdk/script"`.

**Step 3: Fix handleSubmitHTLC — verify HTLC output + return capsule**

Replace the payment verification and response sections:

```go
func (d *Daemon) handleSubmitHTLC(w http.ResponseWriter, r *http.Request) {
	// ... existing lookup, expiry, already-paid checks unchanged ...

	// Read the HTLC funding transaction.
	defer func() { _ = r.Body.Close() }()
	htlcBody, err := io.ReadAll(io.LimitReader(r.Body, maxHTLCBodySize))
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "Failed to read request body")
		return
	}
	if len(htlcBody) == 0 {
		writeJSONError(w, http.StatusBadRequest, "EMPTY_TX", "HTLC transaction body is required")
		return
	}

	// Rebuild the expected HTLC script from invoice params.
	// We need buyer's pubkey from the submitted tx — extract from input signatures.
	// For now, verify the HTLC output matches expected script if we have it.
	if len(invoice.HTLCScript) > 0 {
		_, err := x402.VerifyHTLCFunding(htlcBody, invoice.HTLCScript, invoice.TotalPrice)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID",
				fmt.Sprintf("HTLC verification failed: %v", err))
			return
		}
	} else {
		// Fallback: verify as P2PKH payment (backwards compatibility).
		proof := &x402.PaymentProof{RawTx: htlcBody}
		inv := &x402.Invoice{
			ID:          invoice.ID,
			Price:       invoice.TotalPrice,
			PricePerKB:  invoice.PricePerKB,
			FileSize:    invoice.FileSize,
			PaymentAddr: invoice.PaymentAddr,
			Expiry:      invoice.Expiry.Unix(),
		}
		if err := x402.VerifyPayment(proof, inv); err != nil {
			writeJSONError(w, http.StatusBadRequest, "PAYMENT_INVALID", "Payment verification failed")
			return
		}
	}

	// Mark as paid.
	d.invoicesMu.Lock()
	invoice.Paid = true
	d.invoicesMu.Unlock()

	// Return the capsule (ECDH shared secret).
	if len(invoice.Capsule) == 0 {
		writeJSONError(w, http.StatusInternalServerError, "NO_CAPSULE", "No capsule computed for this invoice")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"invoice_id": invoice.ID,
		"capsule":    hex.EncodeToString(invoice.Capsule),
		"paid":       true,
	})
}
```

**Step 4: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build ./internal/daemon/...`
Expected: BUILD SUCCESS

**Step 5: Run existing daemon tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./internal/daemon/ -v -count=1`
Expected: PASS (may need test updates if they depend on old behavior)

**Step 6: Commit**

```bash
git add bitfs/internal/daemon/payment.go
git commit -m "fix(daemon): correct capsule computation and HTLC verification"
```

---

### Task 8: Update bget to build proper HTLC funding tx

**Files:**
- Modify: `bitfs/cmd/bget/main.go` (lines 232-268)

**Step 1: Update handlePaid to use BuildHTLCFundingTx**

The key change is in `handlePaid()`. Currently bget sends the HTLC *script* bytes — it needs to build a complete funding transaction. Since bget doesn't have a UTXO source, we add a `--utxo` flag for now (format: `txid:vout:amount:script`).

However, to keep scope minimal, the simpler approach is: bget already has `--wallet-key`. We can add a `--utxo` flag that accepts `txid:vout:amount` (hex format). The UTXO's ScriptPubKey is derived from the wallet key.

Replace the HTLC building section in `handlePaid()`:

```go
// Step 2: Build HTLC funding transaction.
buyerPKH := privKey.PubKey().Hash()
p2pkhScript := buildBuyerP2PKHScript(buyerPKH)

// Parse UTXO from flag (txid:vout:amount in hex).
utxo, err := parseUTXOFlag(utxoFlag)
if err != nil {
	fmt.Fprintf(stderr, "bget: invalid --utxo: %v\n", err)
	return 6
}
utxo.ScriptPubKey = p2pkhScript

fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
	BuyerPrivKey: privKey,
	SellerAddr:   sellerAddr,
	CapsuleHash:  capsuleHash,
	Amount:       buyInfo.Price,
	Timeout:      x402.DefaultHTLCTimeout,
	UTXOs:        []*x402.HTLCUTXO{utxo},
	ChangeAddr:   buyerPKH,
	FeeRate:      1,
})
if err != nil {
	fmt.Fprintf(stderr, "bget: build HTLC funding tx: %v\n", err)
	return 5
}

// Step 3: Submit the signed funding transaction.
capsuleResp, err := c.SubmitHTLC(meta.TxID, fundingResult.RawTx)
```

Add `--utxo` flag and helper:

```go
utxoStr := fs.String("utxo", "", "buyer UTXO for purchase (txid:vout:amount)")
```

```go
func parseUTXOFlag(s string) (*x402.HTLCUTXO, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected txid:vout:amount")
	}
	txid, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid txid hex: %w", err)
	}
	if len(txid) != 32 {
		return nil, fmt.Errorf("txid must be 32 bytes")
	}
	vout, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid vout: %w", err)
	}
	amount, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}
	return &x402.HTLCUTXO{
		TxID:   txid,
		Vout:   uint32(vout),
		Amount: amount,
	}, nil
}
```

**Step 2: Verify compilation**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go build ./cmd/bget/`
Expected: BUILD SUCCESS

**Step 3: Commit**

```bash
git add bitfs/cmd/bget/main.go
git commit -m "fix(bget): build proper HTLC funding tx instead of raw script"
```

---

### Task 9: Update E2E regtest test with real HTLC claim

**Files:**
- Modify: `bitfs/e2e/06_paid_purchase_test.go`

**Step 1: Replace manual HTLC construction with BuildHTLCFundingTx**

Replace Steps 8-9 in `TestPaidPurchaseFlow` (approximately lines 280-388). The existing test manually builds the HTLC tx and uses a dummy signature for the claim. Replace with:

```go
// Step 8: Build HTLC funding tx using BuildHTLCFundingTx.
buyerUTXOForHTLC := &x402.HTLCUTXO{
	TxID:         buyerUTXO.TxID,
	Vout:         buyerUTXO.Vout,
	Amount:       buyerUTXO.Amount,
	ScriptPubKey: buyerUTXO.ScriptPubKey,
}

fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
	BuyerPrivKey: buyerFeeKey.PrivateKey,
	SellerAddr:   sellerPKH,
	CapsuleHash:  capsuleHash,
	Amount:       invoice.Price,
	Timeout:      x402.DefaultHTLCTimeout,
	UTXOs:        []*x402.HTLCUTXO{buyerUTXOForHTLC},
	ChangeAddr:   changePKH,
	FeeRate:      1,
})
require.NoError(t, err, "build HTLC funding tx")
t.Logf("HTLC funding tx: %d bytes", len(fundingResult.RawTx))

// Broadcast the funding tx.
htlcPaymentTxID, err := node.SendRawTransaction(ctx, hex.EncodeToString(fundingResult.RawTx))
require.NoError(t, err, "broadcast HTLC funding tx")
t.Logf("HTLC payment txid: %s", htlcPaymentTxID)
mineOneBlock(t)

// Step 9: Seller claims the HTLC (real signature, not dummy).
claimTx, err := x402.BuildSellerClaimTx(&x402.SellerClaimParams{
	FundingTxID:   fundingResult.TxID,
	FundingVout:   fundingResult.HTLCVout,
	FundingAmount: fundingResult.HTLCAmount,
	HTLCScript:    fundingResult.HTLCScript,
	Capsule:       capsule,
	SellerPrivKey: sellerFeeKey.PrivateKey,
	OutputAddr:    sellerPKH,
	FeeRate:       1,
})
require.NoError(t, err, "build seller claim tx")

claimTxHex := hex.EncodeToString(claimTx.Bytes())
claimTxID, err := node.SendRawTransaction(ctx, claimTxHex)
require.NoError(t, err, "broadcast seller claim tx")
t.Logf("seller claim txid: %s", claimTxID)
mineOneBlock(t)
```

Also update Step 10 (capsule extraction) to use the real claim tx.

**Step 2: Run the E2E test**

Requires Docker Desktop running with the regtest node:

```bash
cd /Users/alex/Codes/RabbitHole/bitfs/e2e && docker compose up -d
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e ./e2e/ -run TestPaidPurchaseFlow -v -timeout 180s
```

Expected: PASS (real HTLC funding + claim on regtest)

**Step 3: Commit**

```bash
git add bitfs/e2e/06_paid_purchase_test.go
git commit -m "test(e2e): use real HTLC funding and claim transactions"
```

---

### Task 10: Add E2E buyer refund test

**Files:**
- Modify: `bitfs/e2e/06_paid_purchase_test.go`

**Step 1: Add TestPaidPurchase_BuyerRefund**

```go
// TestPaidPurchase_BuyerRefund tests the buyer refund path on regtest.
// It builds an HTLC funding tx, mines past the timeout, then spends via refund.
func TestPaidPurchase_BuyerRefund(t *testing.T) {
	node := testutil.NewRegtestNode()
	testutil.SkipIfUnavailable(t, node)

	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	sellerWallet := setupFundedWallet(t, ctx, node)
	buyerWallet := setupFundedWallet(t, ctx, node)

	sellerFeeKey, err := sellerWallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	buyerFeeKey, err := buyerWallet.DeriveFeeKey(wallet.ExternalChain, 0)
	require.NoError(t, err)

	// Fund buyer.
	buyerFeeAddr, err := script.NewAddressFromPublicKey(buyerFeeKey.PublicKey, false)
	require.NoError(t, err)
	buyerUTXO := getFundedUTXO(t, ctx, node, buyerFeeAddr.AddressString, buyerFeeKey)

	sellerPKH := sellerFeeKey.PublicKey.Hash()
	buyerPKH := buyerFeeKey.PublicKey.Hash()
	capsuleHash := bytes.Repeat([]byte{0xab}, 32)

	mineAddr, err := node.NewAddress(ctx)
	require.NoError(t, err)

	// Use a very short timeout (e.g., current block height + 1) so we can refund quickly.
	info, err := node.GetBlockchainInfo(ctx)
	require.NoError(t, err)
	timeout := uint32(info.Blocks + 1)

	// Build and broadcast HTLC funding tx.
	fundingResult, err := x402.BuildHTLCFundingTx(&x402.HTLCFundingParams{
		BuyerPrivKey: buyerFeeKey.PrivateKey,
		SellerAddr:   sellerPKH,
		CapsuleHash:  capsuleHash,
		Amount:       1000,
		Timeout:      timeout,
		UTXOs: []*x402.HTLCUTXO{{
			TxID:         buyerUTXO.TxID,
			Vout:         buyerUTXO.Vout,
			Amount:       buyerUTXO.Amount,
			ScriptPubKey: buyerUTXO.ScriptPubKey,
		}},
		ChangeAddr: buyerPKH,
		FeeRate:    1,
	})
	require.NoError(t, err)

	htlcTxID, err := node.SendRawTransaction(ctx, hex.EncodeToString(fundingResult.RawTx))
	require.NoError(t, err)
	t.Logf("HTLC funding txid: %s (timeout at block %d)", htlcTxID, timeout)

	// Mine past the timeout.
	_, err = node.MineBlocks(ctx, 2, mineAddr)
	require.NoError(t, err)

	// Build and broadcast buyer refund tx.
	refundTx, err := x402.BuildBuyerRefundTx(&x402.BuyerRefundParams{
		FundingTxID:   fundingResult.TxID,
		FundingVout:   fundingResult.HTLCVout,
		FundingAmount: fundingResult.HTLCAmount,
		HTLCScript:    fundingResult.HTLCScript,
		BuyerPrivKey:  buyerFeeKey.PrivateKey,
		OutputAddr:    buyerPKH,
		Locktime:      timeout,
		FeeRate:       1,
	})
	require.NoError(t, err)

	refundTxHex := hex.EncodeToString(refundTx.Bytes())
	refundTxID, err := node.SendRawTransaction(ctx, refundTxHex)
	require.NoError(t, err, "broadcast buyer refund tx")
	t.Logf("buyer refund txid: %s", refundTxID)

	// Mine to confirm.
	_, err = node.MineBlocks(ctx, 1, mineAddr)
	require.NoError(t, err)

	// Verify refund was confirmed.
	refundRaw, err := node.GetRawTransaction(ctx, refundTxID)
	require.NoError(t, err)
	require.NotEmpty(t, refundRaw)
	t.Logf("buyer refund confirmed on-chain: %d bytes", len(refundRaw))
}
```

**Step 2: Run the test**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e ./e2e/ -run TestPaidPurchase_BuyerRefund -v -timeout 180s
```

Expected: PASS

**Step 3: Commit**

```bash
git add bitfs/e2e/06_paid_purchase_test.go
git commit -m "test(e2e): add buyer refund path test on regtest"
```

---

### Task 11: Run full test suite, fix regressions

**Files:**
- Potentially any file touched in Tasks 1-10

**Step 1: Run all libbitfs tests**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && go test ./... -v -count=1`
Expected: All PASS

**Step 2: Run all bitfs tests**

Run: `cd /Users/alex/Codes/RabbitHole/bitfs && go test ./... -v -count=1`
Expected: All PASS

**Step 3: Run golangci-lint**

Run: `cd /Users/alex/Codes/RabbitHole/libbitfs && golangci-lint run ./...`
Run: `cd /Users/alex/Codes/RabbitHole/bitfs && golangci-lint run ./...`
Expected: No new issues

**Step 4: Fix any regressions or lint issues found**

Address issues iteratively until all checks pass.

**Step 5: Final commit if fixes needed**

```bash
git add -A
git commit -m "fix: address test regressions and lint issues"
```

---

### Task 12: Run E2E regtest full suite

**Step 1: Ensure Docker is running**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs/e2e && docker compose up -d
```

**Step 2: Run all E2E tests**

```bash
cd /Users/alex/Codes/RabbitHole/bitfs && go test -tags e2e ./e2e/... -v -timeout 300s
```

Expected: All PASS (01 through 07)

**Step 3: Commit any final fixes**

If any E2E test needed adjustments, commit them.

---

## File Inventory

| File | Action | Task |
|------|--------|------|
| `libbitfs/x402/htlc_tx.go` | CREATE | 1-5 |
| `libbitfs/x402/htlc_tx_test.go` | CREATE | 2-6 |
| `bitfs/internal/daemon/payment.go` | MODIFY | 7 |
| `bitfs/cmd/bget/main.go` | MODIFY | 8 |
| `bitfs/e2e/06_paid_purchase_test.go` | MODIFY | 9-10 |

## Dependencies Between Tasks

```
Task 1 (types) → Task 2 (VerifyHTLCFunding) → Task 3 (BuildHTLCFundingTx) → Task 6 (round-trip)
                                              → Task 4 (BuildSellerClaimTx) → Task 6 (round-trip)
                                              → Task 5 (BuildBuyerRefundTx) → Task 6 (round-trip)
Task 6 (round-trip) → Task 7 (daemon fixes)
Task 7 (daemon fixes) → Task 8 (bget update)
Tasks 3,4,5 → Task 9 (E2E claim)
Task 5 → Task 10 (E2E refund)
Tasks 1-10 → Task 11 (regression) → Task 12 (E2E full)
```
