package main

import (
	"encoding/hex"
	"fmt"

	"github.com/bitfsorg/libbitfs-go/metanet"
)

// Method42Analysis holds the encryption analysis for a Metanet node.
type Method42Analysis struct {
	TxID       string
	PNode      string
	Access     string
	Encrypted  bool
	KeyHash    string // hex
	KeyHashLen int
	AccessMode string // description of how ECDH works for this mode
	CanDecrypt string // explanation of what's needed to decrypt
}

// AnalyzeMethod42 examines a Metanet node's encryption properties.
func AnalyzeMethod42(txid string, node *metanet.Node, pNodeHex string) *Method42Analysis {
	a := &Method42Analysis{
		TxID:  txid,
		PNode: pNodeHex,
	}

	if node == nil {
		return a
	}

	a.Encrypted = node.Encrypted

	switch node.Access {
	case metanet.AccessPrivate:
		a.Access = "PRIVATE"
	case metanet.AccessFree:
		a.Access = "FREE"
	case metanet.AccessPaid:
		a.Access = "PAID"
	default:
		a.Access = fmt.Sprintf("UNKNOWN(%d)", node.Access)
	}

	if len(node.KeyHash) > 0 {
		a.KeyHash = hex.EncodeToString(node.KeyHash)
		a.KeyHashLen = len(node.KeyHash)
	}

	switch node.Access {
	case metanet.AccessPrivate:
		a.AccessMode = "PRIVATE: aes_key = HKDF-SHA256(ECDH(D_node, P_node).x, key_hash)"
		a.CanDecrypt = fmt.Sprintf("Requires D_node (private key for P_node=%s). Only the node owner can decrypt.", truncHash(pNodeHex))
	case metanet.AccessFree:
		a.AccessMode = "FREE: aes_key = HKDF-SHA256(ECDH(1, P_node).x, key_hash) — D_node=1 (trivial key)"
		a.CanDecrypt = "Anyone can decrypt — the private key is the scalar 1 (trivial key trick)."
	case metanet.AccessPaid:
		a.AccessMode = "PAID: aes_key derived from HTLC capsule after atomic swap payment"
		a.CanDecrypt = "Requires completing HTLC atomic swap payment to obtain the ECDH capsule."
	}

	return a
}
