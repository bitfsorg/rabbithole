// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

import (
	"bytes"
	"crypto/sha256"
	"testing"
	"time"
)

// testLocalPubKey is a valid 33-byte compressed public key for the local node.
var testLocalPubKey = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x02
	h := sha256.Sum256([]byte("local-node-pubkey"))
	copy(key[1:], h[:])
	return key
}()

// testLocalPrivKey is a 32-byte private key for the local node.
var testLocalPrivKey = func() []byte {
	h := sha256.Sum256([]byte("local-node-privkey"))
	return h[:]
}()

// testPeer1PubKey is a valid 33-byte compressed public key for peer 1.
var testPeer1PubKey = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x03
	h := sha256.Sum256([]byte("peer1-pubkey"))
	copy(key[1:], h[:])
	return key
}()

// testPeer2PubKey is a valid 33-byte compressed public key for peer 2.
var testPeer2PubKey = func() []byte {
	key := make([]byte, 33)
	key[0] = 0x02
	h := sha256.Sum256([]byte("peer2-pubkey"))
	copy(key[1:], h[:])
	return key
}()

func newTestNode(pubKey []byte, endpoint string) *Node {
	return &Node{
		PubKey:   pubKey,
		Endpoint: endpoint,
		Topics:   []string{TopicStorage, TopicContent},
		Capacity: 1_000_000_000,
		Uptime:   99.5,
		LastSeen: time.Now().Unix(),
	}
}

func newTestService() *OverlayService {
	localNode := newTestNode(testLocalPubKey, "https://local.metanet.org:8080")
	return NewOverlayService(localNode, NewInMemoryPeerStore(), NewInMemoryTopicStore())
}

// ---------------------------------------------------------------------------
// PeerStore tests
// ---------------------------------------------------------------------------

func TestInMemoryPeerStoreAddGet(t *testing.T) {
	store := NewInMemoryPeerStore()
	peer := newTestNode(testPeer1PubKey, "https://peer1.example.com")

	if err := store.AddPeer(peer); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	got, err := store.GetPeer(testPeer1PubKey)
	if err != nil {
		t.Fatalf("GetPeer: %v", err)
	}
	if !bytes.Equal(got.PubKey, peer.PubKey) {
		t.Error("retrieved peer has wrong pubkey")
	}
	if got.Endpoint != peer.Endpoint {
		t.Errorf("Endpoint = %q, want %q", got.Endpoint, peer.Endpoint)
	}
}

func TestInMemoryPeerStoreRemove(t *testing.T) {
	store := NewInMemoryPeerStore()
	peer := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	store.AddPeer(peer)

	if err := store.RemovePeer(testPeer1PubKey); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}

	_, err := store.GetPeer(testPeer1PubKey)
	if err != ErrPeerNotFound {
		t.Errorf("got %v, want ErrPeerNotFound", err)
	}
}

func TestInMemoryPeerStoreRemoveNotFound(t *testing.T) {
	store := NewInMemoryPeerStore()
	err := store.RemovePeer(testPeer1PubKey)
	if err != ErrPeerNotFound {
		t.Errorf("got %v, want ErrPeerNotFound", err)
	}
}

func TestInMemoryPeerStoreListPeers(t *testing.T) {
	store := NewInMemoryPeerStore()
	store.AddPeer(newTestNode(testPeer1PubKey, "https://peer1.example.com"))
	store.AddPeer(newTestNode(testPeer2PubKey, "https://peer2.example.com"))

	peers, err := store.ListPeers()
	if err != nil {
		t.Fatalf("ListPeers: %v", err)
	}
	if len(peers) != 2 {
		t.Errorf("got %d peers, want 2", len(peers))
	}
}

func TestInMemoryPeerStoreUpdateLastSeen(t *testing.T) {
	store := NewInMemoryPeerStore()
	peer := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	peer.LastSeen = 1000
	store.AddPeer(peer)

	newTime := int64(2000)
	if err := store.UpdateLastSeen(testPeer1PubKey, newTime); err != nil {
		t.Fatalf("UpdateLastSeen: %v", err)
	}

	got, _ := store.GetPeer(testPeer1PubKey)
	if got.LastSeen != newTime {
		t.Errorf("LastSeen = %d, want %d", got.LastSeen, newTime)
	}
}

// ---------------------------------------------------------------------------
// TopicStore tests
// ---------------------------------------------------------------------------

func TestInMemoryTopicStoreSubscribe(t *testing.T) {
	store := NewInMemoryTopicStore()
	node := newTestNode(testPeer1PubKey, "https://peer1.example.com")

	if err := store.Subscribe(TopicStorage, node); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	subs, err := store.GetSubscribers(TopicStorage)
	if err != nil {
		t.Fatalf("GetSubscribers: %v", err)
	}
	if len(subs) != 1 {
		t.Errorf("got %d subscribers, want 1", len(subs))
	}
}

func TestInMemoryTopicStoreUnsubscribe(t *testing.T) {
	store := NewInMemoryTopicStore()
	node := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	store.Subscribe(TopicStorage, node)

	if err := store.Unsubscribe(TopicStorage, testPeer1PubKey); err != nil {
		t.Fatalf("Unsubscribe: %v", err)
	}

	subs, _ := store.GetSubscribers(TopicStorage)
	if len(subs) != 0 {
		t.Errorf("got %d subscribers after unsubscribe, want 0", len(subs))
	}
}

func TestInMemoryTopicStoreListTopics(t *testing.T) {
	store := NewInMemoryTopicStore()
	store.Subscribe(TopicStorage, newTestNode(testPeer1PubKey, "a"))
	store.Subscribe(TopicContent, newTestNode(testPeer2PubKey, "b"))

	topics, err := store.ListTopics()
	if err != nil {
		t.Fatalf("ListTopics: %v", err)
	}
	if len(topics) != 2 {
		t.Errorf("got %d topics, want 2", len(topics))
	}
}

// ---------------------------------------------------------------------------
// OverlayService tests
// ---------------------------------------------------------------------------

func TestNewOverlayService(t *testing.T) {
	svc := newTestService()
	if svc.LocalNode == nil {
		t.Fatal("LocalNode is nil")
	}
	if svc.PeerStore == nil {
		t.Fatal("PeerStore is nil")
	}
	if svc.TopicStore == nil {
		t.Fatal("TopicStore is nil")
	}
}

func TestAdvertise(t *testing.T) {
	svc := newTestService()
	err := svc.Advertise(testLocalPrivKey)
	if err != nil {
		t.Fatalf("Advertise: %v", err)
	}
	if svc.LocalNode.LastSeen == 0 {
		t.Error("LastSeen should be updated after advertise")
	}
}

func TestAdvertiseInvalidKey(t *testing.T) {
	svc := newTestService()
	err := svc.Advertise([]byte{0x01})
	if err != ErrInvalidPrivKey {
		t.Errorf("got %v, want ErrInvalidPrivKey", err)
	}
}

func TestRegisterUnregisterTopic(t *testing.T) {
	svc := newTestService()

	// Register.
	if err := svc.RegisterTopic(TopicProof); err != nil {
		t.Fatalf("RegisterTopic: %v", err)
	}

	// Check topic is in local node's topics.
	found := false
	for _, topic := range svc.LocalNode.Topics {
		if topic == TopicProof {
			found = true
			break
		}
	}
	if !found {
		t.Error("topic should be in local node's topics after register")
	}

	// Unregister.
	if err := svc.UnregisterTopic(TopicProof); err != nil {
		t.Fatalf("UnregisterTopic: %v", err)
	}

	found = false
	for _, topic := range svc.LocalNode.Topics {
		if topic == TopicProof {
			found = true
			break
		}
	}
	if found {
		t.Error("topic should not be in local node's topics after unregister")
	}
}

func TestDiscoverByTopic(t *testing.T) {
	svc := newTestService()
	peer := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	svc.TopicStore.Subscribe(TopicStorage, peer)

	resp, err := svc.Discover(&LookupRequest{Topic: TopicStorage, MaxResults: 10})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(resp.Nodes) != 1 {
		t.Errorf("got %d nodes, want 1", len(resp.Nodes))
	}
}

func TestDiscoverAllPeers(t *testing.T) {
	svc := newTestService()
	svc.PeerStore.AddPeer(newTestNode(testPeer1PubKey, "https://peer1.example.com"))
	svc.PeerStore.AddPeer(newTestNode(testPeer2PubKey, "https://peer2.example.com"))

	resp, err := svc.Discover(&LookupRequest{MaxResults: 10})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(resp.Nodes) != 2 {
		t.Errorf("got %d nodes, want 2", len(resp.Nodes))
	}
}

func TestLocateContent(t *testing.T) {
	svc := newTestService()
	contentHash := sha256.Sum256([]byte("test-content"))
	peer := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	dealTxID := sha256.Sum256([]byte("deal-txid"))

	svc.RegisterContent(contentHash, peer, &dealTxID)

	loc, err := svc.LocateContent(contentHash)
	if err != nil {
		t.Fatalf("LocateContent: %v", err)
	}
	if len(loc.Nodes) != 1 {
		t.Errorf("got %d nodes, want 1", len(loc.Nodes))
	}
	if loc.ContentHash != contentHash {
		t.Error("content hash mismatch")
	}
	if len(loc.DealTxIDs) != 1 {
		t.Errorf("got %d deal txids, want 1", len(loc.DealTxIDs))
	}
}

func TestLocateContentNotFound(t *testing.T) {
	svc := newTestService()
	_, err := svc.LocateContent([32]byte{0xff})
	if err != ErrNoNodesFound {
		t.Errorf("got %v, want ErrNoNodesFound", err)
	}
}

func TestSubmitTx(t *testing.T) {
	svc := newTestService()
	if err := svc.SubmitTx([]byte("tx-data"), TopicStorage); err != nil {
		t.Fatalf("SubmitTx: %v", err)
	}
}

func TestSubmitTxTooLarge(t *testing.T) {
	svc := newTestService()
	data := make([]byte, MaxMessageSize+1)
	err := svc.SubmitTx(data, TopicStorage)
	if err != ErrMessageTooLarge {
		t.Errorf("got %v, want ErrMessageTooLarge", err)
	}
}

func TestSubmitTxEmpty(t *testing.T) {
	svc := newTestService()
	err := svc.SubmitTx(nil, TopicStorage)
	if err != ErrMessageTooLarge {
		t.Errorf("got %v, want ErrMessageTooLarge", err)
	}
}

func TestHeartbeat(t *testing.T) {
	svc := newTestService()
	svc.LocalNode.LastSeen = 0

	err := svc.Heartbeat(testLocalPrivKey)
	if err != nil {
		t.Fatalf("Heartbeat: %v", err)
	}
	if svc.LocalNode.LastSeen == 0 {
		t.Error("LastSeen should be updated after heartbeat")
	}
}

func TestHeartbeatInvalidKey(t *testing.T) {
	svc := newTestService()
	err := svc.Heartbeat([]byte{0x01})
	if err != ErrInvalidPrivKey {
		t.Errorf("got %v, want ErrInvalidPrivKey", err)
	}
}

func TestPruneStalePeers(t *testing.T) {
	svc := newTestService()

	// Add a stale peer (last seen 1 hour ago).
	stalePeer := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	stalePeer.LastSeen = time.Now().Unix() - 3600
	svc.PeerStore.AddPeer(stalePeer)

	// Add a fresh peer.
	freshPeer := newTestNode(testPeer2PubKey, "https://peer2.example.com")
	freshPeer.LastSeen = time.Now().Unix()
	svc.PeerStore.AddPeer(freshPeer)

	// Prune with 30-minute max age.
	pruned, err := svc.PruneStalePeers(1800)
	if err != nil {
		t.Fatalf("PruneStalePeers: %v", err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	// Fresh peer should still exist.
	peers, _ := svc.PeerStore.ListPeers()
	if len(peers) != 1 {
		t.Errorf("remaining peers = %d, want 1", len(peers))
	}
}

// ---------------------------------------------------------------------------
// Advertisement tests
// ---------------------------------------------------------------------------

func TestSignVerifyAdvertisement(t *testing.T) {
	node := newTestNode(testPeer1PubKey, "https://peer1.example.com")
	adv := &AdvertiseRequest{
		Node:          node,
		ContentHashes: [][32]byte{sha256.Sum256([]byte("content1"))},
	}

	privKey := func() []byte {
		h := sha256.Sum256([]byte("peer1-privkey"))
		return h[:]
	}()

	if err := SignAdvertisement(adv, privKey); err != nil {
		t.Fatalf("SignAdvertisement: %v", err)
	}
	if len(adv.Signature) != 32 {
		t.Errorf("signature length = %d, want 32", len(adv.Signature))
	}

	if err := VerifyAdvertisement(adv); err != nil {
		t.Fatalf("VerifyAdvertisement: %v", err)
	}
}

func TestSignAdvertisementInvalidKey(t *testing.T) {
	adv := &AdvertiseRequest{Node: newTestNode(testPeer1PubKey, "https://peer.com")}
	err := SignAdvertisement(adv, []byte{0x01})
	if err != ErrInvalidPrivKey {
		t.Errorf("got %v, want ErrInvalidPrivKey", err)
	}
}

func TestVerifyAdvertisementBadSignature(t *testing.T) {
	adv := &AdvertiseRequest{
		Node:      newTestNode(testPeer1PubKey, "https://peer.com"),
		Signature: []byte("short"), // Wrong length.
	}
	err := VerifyAdvertisement(adv)
	if err != ErrInvalidSignature {
		t.Errorf("got %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyAdvertisementNilNode(t *testing.T) {
	err := VerifyAdvertisement(nil)
	if err != ErrNilNode {
		t.Errorf("got %v, want ErrNilNode", err)
	}
}

// ---------------------------------------------------------------------------
// Topic constants tests
// ---------------------------------------------------------------------------

func TestAllTopics(t *testing.T) {
	topics := AllTopics()
	if len(topics) != 4 {
		t.Errorf("got %d topics, want 4", len(topics))
	}

	expected := map[string]bool{
		TopicStorage: true, TopicProof: true,
		TopicPayment: true, TopicContent: true,
	}
	for _, topic := range topics {
		if !expected[topic] {
			t.Errorf("unexpected topic: %s", topic)
		}
	}
}

func TestHandlePeerMessage(t *testing.T) {
	svc := newTestService()
	if err := svc.HandlePeerMessage([]byte("valid message")); err != nil {
		t.Fatalf("HandlePeerMessage: %v", err)
	}
}

func TestHandlePeerMessageEmpty(t *testing.T) {
	svc := newTestService()
	err := svc.HandlePeerMessage(nil)
	if err != ErrMessageTooLarge {
		t.Errorf("got %v, want ErrMessageTooLarge", err)
	}
}
