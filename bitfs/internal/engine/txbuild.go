package engine

import (
	"fmt"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"

	"github.com/tongxiaofeng/libbitfs/tx"
	"github.com/tongxiaofeng/libbitfs/wallet"
)

// buildUnsignedCreateRootTx wraps tx.BuildUnsignedCreateRootTx.
func buildUnsignedCreateRootTx(kp *wallet.KeyPair, payload []byte, feeUTXO *tx.UTXO, changeAddr []byte) (*tx.MetanetTx, error) {
	params := &tx.CreateRootParams{
		NodePubKey:  kp.PublicKey,
		NodePrivKey: kp.PrivateKey,
		Payload:     payload,
		FeeUTXO:     feeUTXO,
		ChangeAddr:  changeAddr,
	}
	return tx.BuildUnsignedCreateRootTx(params)
}

// buildUnsignedCreateChildTx wraps tx.BuildUnsignedCreateChildTx.
func buildUnsignedCreateChildTx(childKP *wallet.KeyPair, parentTxID []byte, payload []byte, parentUTXO, feeUTXO *tx.UTXO, parentPubKey, changeAddr []byte) (*tx.MetanetTx, error) {
	parentPub, err := pubKeyFromBytes(parentPubKey)
	if err != nil {
		return nil, err
	}

	params := &tx.CreateChildParams{
		NodePubKey:    childKP.PublicKey,
		ParentTxID:    parentTxID,
		Payload:       payload,
		ParentUTXO:    parentUTXO,
		ParentPrivKey: parentUTXO.PrivateKey,
		FeeUTXO:       feeUTXO,
		ParentPubKey:  parentPub,
		ChangeAddr:    changeAddr,
	}
	return tx.BuildUnsignedCreateChildTx(params)
}

// buildUnsignedSelfUpdateTx wraps tx.BuildUnsignedSelfUpdateTx.
func buildUnsignedSelfUpdateTx(kp *wallet.KeyPair, parentTxID []byte, payload []byte, nodeUTXO, feeUTXO *tx.UTXO, changeAddr []byte) (*tx.MetanetTx, error) {
	params := &tx.SelfUpdateParams{
		NodePubKey:  kp.PublicKey,
		NodePrivKey: kp.PrivateKey,
		ParentTxID:  parentTxID,
		Payload:     payload,
		NodeUTXO:    nodeUTXO,
		FeeUTXO:     feeUTXO,
		ChangeAddr:  changeAddr,
	}
	return tx.BuildUnsignedSelfUpdateTx(params)
}

// signCreateRootTx signs a CreateRoot MetanetTx (1 input: fee UTXO).
func signCreateRootTx(mtx *tx.MetanetTx, feeUTXO *tx.UTXO) (string, error) {
	return tx.SignMetanetTx(mtx, []*tx.UTXO{feeUTXO})
}

// signCreateChildTx signs a CreateChild MetanetTx (2 inputs: parent + fee).
func signCreateChildTx(mtx *tx.MetanetTx, parentUTXO, feeUTXO *tx.UTXO) (string, error) {
	return tx.SignMetanetTx(mtx, []*tx.UTXO{parentUTXO, feeUTXO})
}

// signSelfUpdateTx signs a SelfUpdate MetanetTx (2 inputs: node + fee).
func signSelfUpdateTx(mtx *tx.MetanetTx, nodeUTXO, feeUTXO *tx.UTXO) (string, error) {
	return tx.SignMetanetTx(mtx, []*tx.UTXO{nodeUTXO, feeUTXO})
}

// pubKeyFromBytes parses compressed public key bytes.
func pubKeyFromBytes(data []byte) (*ec.PublicKey, error) {
	if len(data) != 33 {
		return nil, fmt.Errorf("engine: invalid pubkey length %d", len(data))
	}
	pub, err := ec.PublicKeyFromBytes(data)
	if err != nil {
		return nil, fmt.Errorf("engine: parse pubkey: %w", err)
	}
	return pub, nil
}
