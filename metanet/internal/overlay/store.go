// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

import (
	"bytes"
	"sync"
)

// PeerStore is the interface for persistent peer storage.
type PeerStore interface {
	AddPeer(node *Node) error
	RemovePeer(pubKey []byte) error
	GetPeer(pubKey []byte) (*Node, error)
	ListPeers() ([]*Node, error)
	UpdateLastSeen(pubKey []byte, timestamp int64) error
}

// TopicStore is the interface for topic subscription storage.
type TopicStore interface {
	Subscribe(topic string, node *Node) error
	Unsubscribe(topic string, pubKey []byte) error
	GetSubscribers(topic string) ([]*Node, error)
	ListTopics() ([]string, error)
}

// InMemoryPeerStore implements PeerStore with an in-memory map.
type InMemoryPeerStore struct {
	mu    sync.RWMutex
	peers map[string]*Node // keyed by hex(pubkey) for simplicity
}

// NewInMemoryPeerStore creates a new in-memory peer store.
func NewInMemoryPeerStore() *InMemoryPeerStore {
	return &InMemoryPeerStore{
		peers: make(map[string]*Node),
	}
}

func (s *InMemoryPeerStore) AddPeer(node *Node) error {
	if node == nil {
		return ErrNilNode
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := pubKeyHex(node.PubKey)
	s.peers[key] = node
	return nil
}

func (s *InMemoryPeerStore) RemovePeer(pubKey []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := pubKeyHex(pubKey)
	if _, ok := s.peers[key]; !ok {
		return ErrPeerNotFound
	}
	delete(s.peers, key)
	return nil
}

func (s *InMemoryPeerStore) GetPeer(pubKey []byte) (*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	key := pubKeyHex(pubKey)
	node, ok := s.peers[key]
	if !ok {
		return nil, ErrPeerNotFound
	}
	return node, nil
}

func (s *InMemoryPeerStore) ListPeers() ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peers := make([]*Node, 0, len(s.peers))
	for _, node := range s.peers {
		peers = append(peers, node)
	}
	return peers, nil
}

func (s *InMemoryPeerStore) UpdateLastSeen(pubKey []byte, timestamp int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := pubKeyHex(pubKey)
	node, ok := s.peers[key]
	if !ok {
		return ErrPeerNotFound
	}
	node.LastSeen = timestamp
	return nil
}

// InMemoryTopicStore implements TopicStore with an in-memory map.
type InMemoryTopicStore struct {
	mu     sync.RWMutex
	topics map[string][]*Node // topic -> subscribers
}

// NewInMemoryTopicStore creates a new in-memory topic store.
func NewInMemoryTopicStore() *InMemoryTopicStore {
	return &InMemoryTopicStore{
		topics: make(map[string][]*Node),
	}
}

func (s *InMemoryTopicStore) Subscribe(topic string, node *Node) error {
	if node == nil {
		return ErrNilNode
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check for duplicate.
	for _, existing := range s.topics[topic] {
		if bytes.Equal(existing.PubKey, node.PubKey) {
			// Already subscribed, update.
			*existing = *node
			return nil
		}
	}

	s.topics[topic] = append(s.topics[topic], node)
	return nil
}

func (s *InMemoryTopicStore) Unsubscribe(topic string, pubKey []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	nodes, ok := s.topics[topic]
	if !ok {
		return ErrTopicNotFound
	}

	for i, node := range nodes {
		if bytes.Equal(node.PubKey, pubKey) {
			s.topics[topic] = append(nodes[:i], nodes[i+1:]...)
			return nil
		}
	}
	return ErrPeerNotFound
}

func (s *InMemoryTopicStore) GetSubscribers(topic string) ([]*Node, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	nodes, ok := s.topics[topic]
	if !ok {
		return nil, nil // No subscribers is not an error.
	}

	// Return a copy.
	result := make([]*Node, len(nodes))
	copy(result, nodes)
	return result, nil
}

func (s *InMemoryTopicStore) ListTopics() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	topics := make([]string, 0, len(s.topics))
	for topic := range s.topics {
		topics = append(topics, topic)
	}
	return topics, nil
}

// pubKeyHex returns the hex string of a public key for use as map key.
func pubKeyHex(pubKey []byte) string {
	const hexChars = "0123456789abcdef"
	result := make([]byte, len(pubKey)*2)
	for i, b := range pubKey {
		result[i*2] = hexChars[b>>4]
		result[i*2+1] = hexChars[b&0x0f]
	}
	return string(result)
}
