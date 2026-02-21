package engine

import (
	"encoding/hex"
	"fmt"
	"path/filepath"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/tongxiaofeng/libbitfs/storage"
	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

// Engine is the shared business logic layer. CLI commands, shell REPL,
// and daemon adapters all call Engine methods to perform filesystem operations.
type Engine struct {
	Wallet  *wallet.Wallet
	WState  *wallet.WalletState
	Store   *storage.FileStore
	State   *LocalState
	DataDir string
	DNS     DNSResolver // injectable for testing; nil uses default net.LookupTXT
}

// Result holds the output of an engine operation.
type Result struct {
	TxHex    string // signed transaction hex (empty if build-only)
	TxID     string // transaction ID hex
	Message  string // human-readable summary
	NodePub  string // created/updated node pubkey hex
}

// New creates a new Engine from a data directory.
func New(dataDir, password string) (*Engine, error) {
	// Load wallet.
	walletPath := filepath.Join(dataDir, "wallet.enc")
	encrypted, err := readFile(walletPath)
	if err != nil {
		return nil, fmt.Errorf("engine: read wallet: %w", err)
	}

	if password == "" {
		password = "bitfs"
	}

	seed, err := wallet.DecryptSeed(encrypted, password)
	if err != nil {
		return nil, fmt.Errorf("engine: decrypt wallet: %w", err)
	}

	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	if err != nil {
		return nil, fmt.Errorf("engine: create wallet: %w", err)
	}

	statePath := filepath.Join(dataDir, "state.json")
	wState, err := loadWalletState(statePath)
	if err != nil {
		return nil, fmt.Errorf("engine: load wallet state: %w", err)
	}

	// Initialize content store.
	storeDir := filepath.Join(dataDir, "storage")
	store, err := storage.NewFileStore(storeDir)
	if err != nil {
		return nil, fmt.Errorf("engine: init storage: %w", err)
	}

	// Load local state (nodes.json).
	localStatePath := filepath.Join(dataDir, "nodes.json")
	localState, err := LoadLocalState(localStatePath)
	if err != nil {
		return nil, fmt.Errorf("engine: load local state: %w", err)
	}

	return &Engine{
		Wallet:  w,
		WState:  wState,
		Store:   store,
		State:   localState,
		DataDir: dataDir,
	}, nil
}

// Close persists state. Should be called when done.
func (e *Engine) Close() error {
	return e.State.Save()
}

// ResolveVaultIndex resolves a vault name to its account index.
// Empty name uses the first active vault.
func (e *Engine) ResolveVaultIndex(vaultName string) (uint32, error) {
	if vaultName == "" {
		vaults := e.Wallet.ListVaults(e.WState)
		if len(vaults) == 0 {
			return 0, fmt.Errorf("engine: no vaults found; run 'bitfs vault create <name>'")
		}
		return vaults[0].AccountIndex, nil
	}
	vault, err := e.Wallet.GetVault(e.WState, vaultName)
	if err != nil {
		return 0, err
	}
	return vault.AccountIndex, nil
}

// DeriveChangeAddr derives a change address (20-byte pubkey hash) from the fee chain.
func (e *Engine) DeriveChangeAddr() ([]byte, *ec.PrivateKey, error) {
	idx := e.WState.NextChangeIndex
	kp, err := e.Wallet.DeriveFeeKey(wallet.InternalChain, idx)
	if err != nil {
		return nil, nil, fmt.Errorf("engine: derive change key: %w", err)
	}
	e.WState.NextChangeIndex++
	hash := pubKeyHash(kp.PublicKey)
	return hash, kp.PrivateKey, nil
}

// AllocateFeeUTXO finds a fee UTXO with enough funds and returns the tx UTXO
// with the private key attached.
func (e *Engine) AllocateFeeUTXO(minAmount uint64) (*tx.UTXO, error) {
	utxoState := e.State.AllocateFeeUTXO(minAmount)
	if utxoState == nil {
		return nil, fmt.Errorf("engine: no fee UTXO with >= %d sats; run 'bitfs fund' first", minAmount)
	}
	return e.utxoStateToTx(utxoState)
}

// utxoStateToTx converts a UTXOState to a tx.UTXO with private key attached.
func (e *Engine) utxoStateToTx(us *UTXOState) (*tx.UTXO, error) {
	txID, err := hex.DecodeString(us.TxID)
	if err != nil {
		return nil, fmt.Errorf("engine: invalid UTXO txid: %w", err)
	}
	scriptPK, err := hex.DecodeString(us.ScriptPubKey)
	if err != nil {
		return nil, fmt.Errorf("engine: invalid UTXO script: %w", err)
	}

	// Look up the private key for the UTXO's pubkey.
	privKey, err := e.lookupPrivKey(us.PubKeyHex, us.Type)
	if err != nil {
		return nil, err
	}

	return &tx.UTXO{
		TxID:         txID,
		Vout:         us.Vout,
		Amount:       us.Amount,
		ScriptPubKey: scriptPK,
		PrivateKey:   privKey,
	}, nil
}

// lookupPrivKey finds the private key for a UTXO based on its pubkey and type.
func (e *Engine) lookupPrivKey(pubKeyHex, utxoType string) (*ec.PrivateKey, error) {
	if utxoType == "fee" {
		// Fee UTXOs use the fee key chain — try all derived indices.
		// In practice, we store the index or try the most recent ones.
		// For now, iterate the known fee range.
		for i := uint32(0); i < e.WState.NextReceiveIndex+10; i++ {
			kp, err := e.Wallet.DeriveFeeKey(wallet.ExternalChain, i)
			if err != nil {
				continue
			}
			if hex.EncodeToString(kp.PublicKey.Compressed()) == pubKeyHex {
				return kp.PrivateKey, nil
			}
		}
		for i := uint32(0); i < e.WState.NextChangeIndex+10; i++ {
			kp, err := e.Wallet.DeriveFeeKey(wallet.InternalChain, i)
			if err != nil {
				continue
			}
			if hex.EncodeToString(kp.PublicKey.Compressed()) == pubKeyHex {
				return kp.PrivateKey, nil
			}
		}
		return nil, fmt.Errorf("engine: no private key found for fee UTXO pubkey %s", pubKeyHex)
	}

	// Node UTXO — look up the node state for derivation indices.
	node := e.State.GetNode(pubKeyHex)
	if node == nil {
		return nil, fmt.Errorf("engine: unknown node %s", pubKeyHex)
	}
	kp, err := e.Wallet.DeriveNodeKey(node.VaultIndex, node.ChildIndices, nil)
	if err != nil {
		return nil, fmt.Errorf("engine: derive node key: %w", err)
	}
	return kp.PrivateKey, nil
}

// TrackNewUTXOs adds UTXOs produced by a MetanetTx to local state.
func (e *Engine) TrackNewUTXOs(mtx *tx.MetanetTx, nodePubHex, changePubHex string) {
	txIDHex := hex.EncodeToString(mtx.TxID)

	if mtx.NodeUTXO != nil {
		scriptPK, _ := tx.BuildP2PKHScript(mustDecompressPubKey(nodePubHex))
		e.State.AddUTXO(&UTXOState{
			TxID:         txIDHex,
			Vout:         mtx.NodeUTXO.Vout,
			Amount:       mtx.NodeUTXO.Amount,
			ScriptPubKey: hex.EncodeToString(scriptPK),
			PubKeyHex:    nodePubHex,
			Type:         "node",
		})
	}

	if mtx.ParentUTXO != nil {
		// Parent refresh — we need the parent's pubkey from the params.
		// This is handled by the caller.
	}

	if mtx.ChangeUTXO != nil && changePubHex != "" {
		scriptPK, _ := tx.BuildP2PKHScript(mustDecompressPubKey(changePubHex))
		e.State.AddUTXO(&UTXOState{
			TxID:         txIDHex,
			Vout:         mtx.ChangeUTXO.Vout,
			Amount:       mtx.ChangeUTXO.Amount,
			ScriptPubKey: hex.EncodeToString(scriptPK),
			PubKeyHex:    changePubHex,
			Type:         "fee",
		})
	}
}

// TrackParentRefreshUTXO adds the parent refresh UTXO to local state.
func (e *Engine) TrackParentRefreshUTXO(mtx *tx.MetanetTx, parentPubHex string) {
	if mtx.ParentUTXO == nil {
		return
	}
	txIDHex := hex.EncodeToString(mtx.TxID)
	scriptPK, _ := tx.BuildP2PKHScript(mustDecompressPubKey(parentPubHex))
	e.State.AddUTXO(&UTXOState{
		TxID:         txIDHex,
		Vout:         mtx.ParentUTXO.Vout,
		Amount:       mtx.ParentUTXO.Amount,
		ScriptPubKey: hex.EncodeToString(scriptPK),
		PubKeyHex:    parentPubHex,
		Type:         "node",
	})
}
