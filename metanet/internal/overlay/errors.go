// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

import "errors"

var (
	// ErrPeerNotFound indicates the requested peer is not in the peer store.
	ErrPeerNotFound = errors.New("overlay: peer not found")

	// ErrInvalidSignature indicates the advertisement signature verification
	// failed.
	ErrInvalidSignature = errors.New("overlay: invalid advertisement signature")

	// ErrTopicNotFound indicates the requested topic does not exist.
	ErrTopicNotFound = errors.New("overlay: topic not found")

	// ErrNoNodesFound indicates no nodes were found for the content query.
	ErrNoNodesFound = errors.New("overlay: no nodes found")

	// ErrPeerUnreachable indicates the peer endpoint cannot be reached.
	ErrPeerUnreachable = errors.New("overlay: peer unreachable")

	// ErrMessageTooLarge indicates the overlay message exceeds size limit.
	ErrMessageTooLarge = errors.New("overlay: message too large")

	// ErrSelfAdvertise indicates the node attempted to add itself as a peer.
	ErrSelfAdvertise = errors.New("overlay: cannot add self as peer")

	// ErrInvalidPubKey indicates a public key is not valid.
	ErrInvalidPubKey = errors.New("overlay: invalid public key")

	// ErrInvalidPrivKey indicates a private key is not valid.
	ErrInvalidPrivKey = errors.New("overlay: invalid private key")

	// ErrInvalidEndpoint indicates the endpoint URL is empty or invalid.
	ErrInvalidEndpoint = errors.New("overlay: invalid endpoint")

	// ErrNilNode indicates a nil node was provided.
	ErrNilNode = errors.New("overlay: nil node")
)
