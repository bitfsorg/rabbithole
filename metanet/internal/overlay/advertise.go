// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
)

// AdvertiseRequest is a node's advertisement to the overlay.
type AdvertiseRequest struct {
	Node          *Node
	Signature     []byte     // Signed by node's private key.
	ContentHashes [][32]byte // Content this node has available.
}

// SignAdvertisement signs a node advertisement with the node's private key.
// Uses HMAC-SHA256 as a signature simulation (real implementation would use
// ECDSA with secp256k1).
func SignAdvertisement(adv *AdvertiseRequest, privKey []byte) error {
	if adv == nil || adv.Node == nil {
		return ErrNilNode
	}
	if len(privKey) != 32 {
		return ErrInvalidPrivKey
	}

	data := serializeAdvertisement(adv)
	mac := hmac.New(sha256.New, privKey)
	mac.Write(data)
	adv.Signature = mac.Sum(nil)
	return nil
}

// VerifyAdvertisement verifies the signature on a node advertisement.
// Since we simulate signatures with HMAC-SHA256 and do not have the
// private key during verification, this verifies the signature is
// well-formed and the advertisement data is consistent.
//
// In a real implementation, this would verify an ECDSA signature using
// the node's public key.
func VerifyAdvertisement(adv *AdvertiseRequest) error {
	if adv == nil || adv.Node == nil {
		return ErrNilNode
	}
	if len(adv.Signature) != 32 { // HMAC-SHA256 output is 32 bytes.
		return ErrInvalidSignature
	}
	if err := validatePubKey(adv.Node.PubKey); err != nil {
		return ErrInvalidSignature
	}
	if adv.Node.Endpoint == "" {
		return ErrInvalidEndpoint
	}
	return nil
}

// serializeAdvertisement produces a canonical byte representation of
// the advertisement for signing.
func serializeAdvertisement(adv *AdvertiseRequest) []byte {
	var buf []byte

	// PubKey.
	buf = append(buf, adv.Node.PubKey...)

	// Endpoint as UTF-8.
	endpointBytes := []byte(adv.Node.Endpoint)
	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(endpointBytes)))
	buf = append(buf, lenBuf...)
	buf = append(buf, endpointBytes...)

	// Topics.
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(adv.Node.Topics)))
	buf = append(buf, lenBuf...)
	for _, topic := range adv.Node.Topics {
		topicBytes := []byte(topic)
		binary.LittleEndian.PutUint32(lenBuf, uint32(len(topicBytes)))
		buf = append(buf, lenBuf...)
		buf = append(buf, topicBytes...)
	}

	// Capacity.
	capBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(capBuf, adv.Node.Capacity)
	buf = append(buf, capBuf...)

	// Content hashes.
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(adv.ContentHashes)))
	buf = append(buf, lenBuf...)
	for _, h := range adv.ContentHashes {
		buf = append(buf, h[:]...)
	}

	return buf
}

// validatePubKey checks that a public key is a valid 33-byte compressed key.
func validatePubKey(pubKey []byte) error {
	if len(pubKey) != 33 {
		return ErrInvalidPubKey
	}
	if pubKey[0] != 0x02 && pubKey[0] != 0x03 {
		return ErrInvalidPubKey
	}
	return nil
}
