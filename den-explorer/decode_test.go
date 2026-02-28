package main

import (
	"encoding/hex"
	"testing"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bitfsorg/libbitfs-go/metanet"
	libtx "github.com/bitfsorg/libbitfs-go/tx"
)

func TestDecodeMetanetTx_NonMetanet(t *testing.T) {
	// A minimal coinbase-like raw tx (won't have OP_RETURN)
	result := DecodeMetanetTx([]byte{0x01, 0x00})
	if result.IsMetanet {
		t.Error("should not be detected as Metanet")
	}
}

func TestDecodeTLVFields(t *testing.T) {
	// Manually construct a TLV: Version=1 (tag=0x01, len=4, value=LE uint32(1))
	payload := []byte{
		0x01, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, // Version=1
		0x02, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, // Type=DIR
	}
	fields := DecodeTLVFields(hex.EncodeToString(payload))
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields[0].TagName != "Version" {
		t.Errorf("expected Version, got %s", fields[0].TagName)
	}
	if fields[0].ValueStr != "1" {
		t.Errorf("expected '1', got '%s'", fields[0].ValueStr)
	}
	if fields[1].TagName != "Type" {
		t.Errorf("expected Type, got %s", fields[1].TagName)
	}
	if fields[1].ValueStr != "DIR" {
		t.Errorf("expected DIR, got '%s'", fields[1].ValueStr)
	}
}

func TestDecodeMetanetTx_RoundTrip(t *testing.T) {
	// Build a real Metanet root tx using libbitfs/tx
	privKey, _ := ec.NewPrivateKey()
	pubKey := privKey.PubKey()

	// Create a minimal Node and serialize
	node := &metanet.Node{
		Version: 1,
		Type:    metanet.NodeTypeDir,
		Op:      metanet.OpCreate,
		Access:  metanet.AccessFree,
	}
	payload, err := metanet.SerializePayload(node)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}

	pushes, err := libtx.BuildOPReturnData(pubKey, nil, payload)
	if err != nil {
		t.Fatalf("build OP_RETURN: %v", err)
	}

	// Verify ParseOPReturnData round-trips
	pNode, parentTxID, payloadOut, err := libtx.ParseOPReturnData(pushes)
	if err != nil {
		t.Fatalf("parse OP_RETURN: %v", err)
	}
	if len(pNode) != 33 {
		t.Errorf("pNode length: %d", len(pNode))
	}
	if len(parentTxID) != 0 {
		t.Error("root should have empty parentTxID")
	}
	if len(payloadOut) == 0 {
		t.Error("payload should not be empty")
	}
}
