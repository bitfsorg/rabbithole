// Command seed populates a BSV regtest node with sample Metanet transactions
// so the Den blockchain explorer has meaningful data to display.
//
// Usage: go run ./cmd/seed
package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/bsv-blockchain/go-sdk/script"
	"github.com/tongxiaofeng/libbitfs-go/metanet"
	"github.com/tongxiaofeng/libbitfs-go/network"
	"github.com/tongxiaofeng/libbitfs-go/tx"
	"github.com/tongxiaofeng/libbitfs-go/wallet"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	rpc := network.NewRPCClient(network.RPCConfig{
		URL:      "http://localhost:18332",
		User:     "bitfs",
		Password: "bitfs",
	})

	// Check connectivity.
	var blockCount int64
	if err := rpc.Call(ctx, "getblockcount", nil, &blockCount); err != nil {
		log.Fatalf("cannot reach regtest node: %v", err)
	}
	fmt.Printf("regtest block height: %d\n", blockCount)

	// ── 1. Create wallet & derive keys ──────────────────────────────
	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	must(err, "generate mnemonic")
	fmt.Printf("mnemonic: %s\n", mnemonic)

	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	must(err, "seed from mnemonic")

	w, err := wallet.NewWallet(seed, &wallet.RegTest)
	must(err, "create wallet")

	feeKey, err := w.DeriveFeeKey(wallet.ExternalChain, 0)
	must(err, "derive fee key")

	rootKey, err := w.DeriveNodeKey(0, nil, nil)
	must(err, "derive root key")

	docsKey, err := w.DeriveNodeKey(0, []uint32{0}, nil)
	must(err, "derive docs dir key")

	helloKey, err := w.DeriveNodeKey(0, []uint32{0, 0}, nil)
	must(err, "derive hello.txt key")

	readmeKey, err := w.DeriveNodeKey(0, []uint32{0, 1}, nil)
	must(err, "derive readme.md key")

	// ── 2. Fund fee address ─────────────────────────────────────────
	feeAddr, err := script.NewAddressFromPublicKey(feeKey.PublicKey, false)
	must(err, "fee address")
	fmt.Printf("fee address: %s\n", feeAddr.AddressString)

	// Import address so node tracks UTXOs.
	must(rpc.Call(ctx, "importaddress", []interface{}{feeAddr.AddressString, "", false}, nil), "importaddress")

	// Ensure coinbase is spendable.
	mineAddr := mineNewAddress(ctx, rpc)
	mine(ctx, rpc, 101, mineAddr)

	// Send 0.1 BSV to fee address.
	var fundTxID string
	must(rpc.Call(ctx, "sendtoaddress", []interface{}{feeAddr.AddressString, 0.1}, &fundTxID), "sendtoaddress")
	fmt.Printf("funding tx: %s\n", fundTxID)

	mine(ctx, rpc, 1, mineAddr)

	// Get funded UTXO.
	utxos, err := rpc.ListUnspent(ctx, feeAddr.AddressString)
	must(err, "list unspent")
	if len(utxos) == 0 {
		log.Fatal("no UTXOs found for fee address")
	}

	feeUTXO := convertUTXO(utxos[0], feeKey)
	fmt.Printf("fee UTXO: %s:%d (%d sat)\n", utxos[0].TxID, utxos[0].Vout, feeUTXO.Amount)

	// ── 3. Create ROOT directory ────────────────────────────────────
	rootNode := &metanet.Node{
		Version:        1,
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpCreate,
		Access:         metanet.AccessFree,
		Domain:         "bitfs.local",
		Description:    "BitFS root vault",
		Timestamp:      uint64(time.Now().Unix()),
		NextChildIndex: 2,
		Children: []metanet.ChildEntry{
			{Index: 0, Name: "docs", Type: metanet.NodeTypeDir, PubKey: docsKey.PublicKey.Compressed()},
		},
	}
	rootPayload, err := metanet.SerializePayload(rootNode)
	must(err, "serialize root payload")

	rootBatch := tx.NewMutationBatch()
	rootBatch.AddCreateRoot(rootKey.PublicKey, rootPayload)
	rootBatch.AddFeeInput(feeUTXO)
	rootBatch.SetChange(feeKey.PublicKey.Hash())
	rootBatch.SetFeeRate(1)

	rootResult, err := rootBatch.Build()
	must(err, "build root tx")

	rootSignedHex, err := rootBatch.Sign(rootResult)
	must(err, "sign root tx")

	rootTxIDStr, err := rpc.BroadcastTx(ctx, rootSignedHex)
	must(err, "broadcast root tx")
	fmt.Printf("ROOT dir tx:  %s\n", rootTxIDStr)

	mine(ctx, rpc, 1, mineAddr)

	// Prepare UTXOs for next tx.
	rootNodeUTXO := rootResult.NodeOps[0].NodeUTXO
	prepareUTXO(rootNodeUTXO, rootKey)

	changeUTXO := rootResult.ChangeUTXO
	prepareUTXO(changeUTXO, feeKey)

	// ── 4. Create DOCS child directory ──────────────────────────────
	docsNode := &metanet.Node{
		Version:        1,
		Type:           metanet.NodeTypeDir,
		Op:             metanet.OpCreate,
		Access:         metanet.AccessFree,
		Description:    "Documentation directory",
		Timestamp:      uint64(time.Now().Unix()),
		Parent:         rootKey.PublicKey.Compressed(),
		Index:          0,
		NextChildIndex: 2,
		Children: []metanet.ChildEntry{
			{Index: 0, Name: "hello.txt", Type: metanet.NodeTypeFile, PubKey: helloKey.PublicKey.Compressed()},
			{Index: 1, Name: "readme.md", Type: metanet.NodeTypeFile, PubKey: readmeKey.PublicKey.Compressed()},
		},
	}
	docsPayload, err := metanet.SerializePayload(docsNode)
	must(err, "serialize docs payload")

	docsBatch := tx.NewMutationBatch()
	docsBatch.AddCreateChild(docsKey.PublicKey, rootResult.TxID, docsPayload, rootNodeUTXO, rootKey.PrivateKey)
	docsBatch.AddFeeInput(changeUTXO)
	docsBatch.SetChange(feeKey.PublicKey.Hash())
	docsBatch.SetFeeRate(1)

	docsResult, err := docsBatch.Build()
	must(err, "build docs tx")

	docsSignedHex, err := docsBatch.Sign(docsResult)
	must(err, "sign docs tx")

	docsTxIDStr, err := rpc.BroadcastTx(ctx, docsSignedHex)
	must(err, "broadcast docs tx")
	fmt.Printf("DOCS dir tx:  %s\n", docsTxIDStr)

	mine(ctx, rpc, 1, mineAddr)

	docsNodeUTXO := docsResult.NodeOps[0].NodeUTXO
	prepareUTXO(docsNodeUTXO, docsKey)

	changeUTXO2 := docsResult.ChangeUTXO
	prepareUTXO(changeUTXO2, feeKey)

	// ── 5. Create hello.txt and readme.md (both children of docs) ──
	// Both children share the same parent (docs), so they are combined
	// into one atomic batch. The docs UTXO is consumed once (deduped).
	helloNode := &metanet.Node{
		Version:     1,
		Type:        metanet.NodeTypeFile,
		Op:          metanet.OpCreate,
		Access:      metanet.AccessFree,
		MimeType:    "text/plain",
		FileSize:    uint64(len([]byte("Hello, BitFS! This is a file stored on the BSV blockchain.\nDecentralized. Encrypted. Permanent."))),
		Description: "A greeting from BitFS",
		Timestamp:   uint64(time.Now().Unix()),
		Parent:      docsKey.PublicKey.Compressed(),
		Index:       0,
		OnChain:     true,
	}
	helloPayload, err := metanet.SerializePayload(helloNode)
	must(err, "serialize hello payload")

	readmeNode := &metanet.Node{
		Version:     1,
		Type:        metanet.NodeTypeFile,
		Op:          metanet.OpCreate,
		Access:      metanet.AccessPaid,
		PricePerKB:  100,
		MimeType:    "text/markdown",
		FileSize:    uint64(len([]byte("# BitFS README\n\nBitFS is a Unix-style decentralized encrypted file system on BSV.\n\n## Features\n- Method 42 encryption\n- SPV verification\n- Metanet DAG\n"))),
		Description: "BitFS project README",
		Keywords:    "bitfs,readme,documentation",
		Timestamp:   uint64(time.Now().Unix()),
		Parent:      docsKey.PublicKey.Compressed(),
		Index:       1,
		OnChain:     true,
	}
	readmePayload, err := metanet.SerializePayload(readmeNode)
	must(err, "serialize readme payload")

	childBatch := tx.NewMutationBatch()
	childBatch.AddCreateChild(helloKey.PublicKey, docsResult.TxID, helloPayload, docsNodeUTXO, docsKey.PrivateKey)
	childBatch.AddCreateChild(readmeKey.PublicKey, docsResult.TxID, readmePayload, docsNodeUTXO, docsKey.PrivateKey)
	childBatch.AddFeeInput(changeUTXO2)
	childBatch.SetChange(feeKey.PublicKey.Hash())
	childBatch.SetFeeRate(1)

	childResult, err := childBatch.Build()
	must(err, "build children tx")

	childSignedHex, err := childBatch.Sign(childResult)
	must(err, "sign children tx")

	childTxIDStr, err := rpc.BroadcastTx(ctx, childSignedHex)
	must(err, "broadcast children tx")

	helloTxIDStr := childTxIDStr
	readmeTxIDStr := childTxIDStr
	fmt.Printf("hello.txt + readme.md tx: %s\n", childTxIDStr)

	mine(ctx, rpc, 1, mineAddr)

	// ── Summary ─────────────────────────────────────────────────────
	fmt.Println("\n=== Metanet DAG seeded ===")
	fmt.Printf("  /           → %s\n", rootTxIDStr)
	fmt.Printf("  /docs/      → %s\n", docsTxIDStr)
	fmt.Printf("  /docs/hello.txt → %s\n", helloTxIDStr)
	fmt.Printf("  /docs/readme.md → %s\n", readmeTxIDStr)
	fmt.Println("\nStart Den explorer to view:")
	fmt.Println("  go run . --network regtest")
}

// ── Helpers ─────────────────────────────────────────────────────────

func must(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %v", msg, err)
	}
}

func mineNewAddress(ctx context.Context, rpc *network.RPCClient) string {
	var addr string
	must(rpc.Call(ctx, "getnewaddress", nil, &addr), "getnewaddress")
	return addr
}

func mine(ctx context.Context, rpc *network.RPCClient, n int, addr string) {
	var hashes []string
	must(rpc.Call(ctx, "generatetoaddress", []interface{}{n, addr}, &hashes), "mine blocks")
}

func convertUTXO(u *network.UTXO, kp *wallet.KeyPair) *tx.UTXO {
	txidBytes, err := hex.DecodeString(u.TxID)
	must(err, "decode utxo txid")
	reverseBytes(txidBytes)

	scriptPubKey, err := tx.BuildP2PKHScript(kp.PublicKey)
	must(err, "build p2pkh script")

	return &tx.UTXO{
		TxID:         txidBytes,
		Vout:         u.Vout,
		Amount:       uint64(u.Amount),
		ScriptPubKey: scriptPubKey,
		PrivateKey:   kp.PrivateKey,
	}
}

func prepareUTXO(utxo *tx.UTXO, kp *wallet.KeyPair) {
	s, err := tx.BuildP2PKHScript(kp.PublicKey)
	must(err, "build p2pkh for utxo")
	utxo.ScriptPubKey = s
	utxo.PrivateKey = kp.PrivateKey
}

func reverseBytes(b []byte) {
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
}
