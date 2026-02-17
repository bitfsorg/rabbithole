// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"sync"
	"time"
)

// MaxMessageSize is the maximum overlay message size (1 MB).
const MaxMessageSize = 1 * 1024 * 1024

// Node represents a Metanet Node's overlay network identity.
type Node struct {
	PubKey   []byte   // 33-byte compressed public key (node identity).
	Endpoint string   // HTTP endpoint URL.
	Topics   []string // Supported overlay topics.
	Capacity uint64   // Advertised storage capacity in bytes.
	Uptime   float64  // Self-reported uptime percentage.
	LastSeen int64    // Unix timestamp of last heartbeat.
}

// TopicManager manages subscriptions and queries for an overlay topic.
type TopicManager struct {
	Topic     string
	Nodes     []*Node
	UpdatedAt int64
}

// OverlayService represents the local node's overlay network service.
type OverlayService struct {
	LocalNode  *Node
	PeerStore  PeerStore
	TopicStore TopicStore

	contentMu  sync.RWMutex
	contentMap map[[32]byte]*ContentLocation
}

// NewOverlayService creates a new overlay network service for the local node.
func NewOverlayService(
	localNode *Node,
	peerStore PeerStore,
	topicStore TopicStore,
) *OverlayService {
	return &OverlayService{
		LocalNode:  localNode,
		PeerStore:  peerStore,
		TopicStore: topicStore,
		contentMap: make(map[[32]byte]*ContentLocation),
	}
}

// Advertise broadcasts the local node's presence and capabilities
// to known peers (BRC-87 Overlay Ads).
func (s *OverlayService) Advertise(privKey []byte) error {
	if len(privKey) != 32 {
		return ErrInvalidPrivKey
	}

	adv := &AdvertiseRequest{
		Node: s.LocalNode,
	}

	if err := SignAdvertisement(adv, privKey); err != nil {
		return err
	}

	// Update local node's last seen.
	s.LocalNode.LastSeen = time.Now().Unix()

	// In a real implementation, this would broadcast to all known peers
	// via HTTP POST. For the in-memory simulation, we just verify the
	// advertisement can be signed and the local state is updated.
	return nil
}

// SubmitTx submits a transaction to the overlay network for propagation
// (BRC-22 SHIP).
func (s *OverlayService) SubmitTx(txData []byte, topic string) error {
	if len(txData) > MaxMessageSize {
		return ErrMessageTooLarge
	}
	if len(txData) == 0 {
		return ErrMessageTooLarge
	}

	// In a real implementation, this would forward the tx to all
	// subscribers of the given topic. For now, this is a no-op
	// beyond validation.
	return nil
}

// RegisterTopic registers the local node as a subscriber for a topic
// (BRC-24 Topic Manager).
func (s *OverlayService) RegisterTopic(topic string) error {
	// Add self to topic store.
	err := s.TopicStore.Subscribe(topic, s.LocalNode)
	if err != nil {
		return err
	}

	// Update local node's topics.
	for _, t := range s.LocalNode.Topics {
		if t == topic {
			return nil // Already in topics list.
		}
	}
	s.LocalNode.Topics = append(s.LocalNode.Topics, topic)
	return nil
}

// UnregisterTopic removes the local node from a topic.
func (s *OverlayService) UnregisterTopic(topic string) error {
	err := s.TopicStore.Unsubscribe(topic, s.LocalNode.PubKey)
	if err != nil {
		return err
	}

	// Remove from local topics.
	for i, t := range s.LocalNode.Topics {
		if t == topic {
			s.LocalNode.Topics = append(s.LocalNode.Topics[:i], s.LocalNode.Topics[i+1:]...)
			break
		}
	}
	return nil
}

// HandlePeerMessage processes an incoming message from a peer node.
func (s *OverlayService) HandlePeerMessage(msg []byte) error {
	if len(msg) > MaxMessageSize {
		return ErrMessageTooLarge
	}
	if len(msg) == 0 {
		return ErrMessageTooLarge
	}

	// In a real implementation, this would parse and dispatch the message
	// (advertisement, transaction, heartbeat, etc.). For now, this validates
	// the message size.
	return nil
}

// Heartbeat sends a keepalive to all known peers, updating LastSeen.
func (s *OverlayService) Heartbeat(privKey []byte) error {
	if len(privKey) != 32 {
		return ErrInvalidPrivKey
	}

	now := time.Now().Unix()
	s.LocalNode.LastSeen = now

	// Compute heartbeat signature.
	data := make([]byte, 8+len(s.LocalNode.PubKey))
	copy(data, s.LocalNode.PubKey)

	mac := hmac.New(sha256.New, privKey)
	mac.Write(data)
	// In real implementation, signature would be sent to all peers.
	_ = mac.Sum(nil)

	return nil
}

// PruneStalePeers removes peers that have not been seen within the given
// duration (in seconds). Returns the number of pruned peers.
func (s *OverlayService) PruneStalePeers(maxAge int64) (int, error) {
	peers, err := s.PeerStore.ListPeers()
	if err != nil {
		return 0, err
	}

	now := time.Now().Unix()
	pruned := 0

	for _, peer := range peers {
		// Skip self.
		if bytes.Equal(peer.PubKey, s.LocalNode.PubKey) {
			continue
		}

		age := now - peer.LastSeen
		if age > maxAge {
			if err := s.PeerStore.RemovePeer(peer.PubKey); err == nil {
				pruned++
			}
		}
	}

	return pruned, nil
}
