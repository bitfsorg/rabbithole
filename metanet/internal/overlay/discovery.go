// Copyright (c) 2024 The Metanet developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package overlay

import "bytes"

// LookupRequest is a query to the overlay network.
type LookupRequest struct {
	ContentHash *[32]byte // Find nodes storing this content (optional).
	Topic       string    // Filter by topic (optional).
	MaxResults  int       // Maximum number of results.
}

// LookupResponse contains the results of an overlay query.
type LookupResponse struct {
	Nodes     []*Node
	Locations []*ContentLocation
}

// ContentLocation maps a content hash to the nodes that store it.
type ContentLocation struct {
	ContentHash [32]byte   // SHA256 of the content.
	Nodes       []*Node    // Nodes that have this content.
	DealTxIDs   [][32]byte // StorageDeal TxIDs.
}

// Discover queries the overlay network for peers matching the given
// criteria (BRC-23 SLAP + BRC-25 Lookup Service).
func (s *OverlayService) Discover(req *LookupRequest) (*LookupResponse, error) {
	resp := &LookupResponse{}

	maxResults := req.MaxResults
	if maxResults <= 0 {
		maxResults = 100 // Default.
	}

	// If topic is specified, get subscribers for that topic.
	if req.Topic != "" {
		subscribers, err := s.TopicStore.GetSubscribers(req.Topic)
		if err != nil {
			return nil, err
		}
		for _, node := range subscribers {
			if len(resp.Nodes) >= maxResults {
				break
			}
			// Exclude self.
			if !bytes.Equal(node.PubKey, s.LocalNode.PubKey) {
				resp.Nodes = append(resp.Nodes, node)
			}
		}
		return resp, nil
	}

	// If content hash is specified, locate content.
	if req.ContentHash != nil {
		location, err := s.LocateContent(*req.ContentHash)
		if err != nil {
			return resp, nil // Not finding content is not an error for discover.
		}
		resp.Locations = append(resp.Locations, location)
		resp.Nodes = location.Nodes
		return resp, nil
	}

	// Otherwise, return all known peers.
	peers, err := s.PeerStore.ListPeers()
	if err != nil {
		return nil, err
	}
	for _, peer := range peers {
		if len(resp.Nodes) >= maxResults {
			break
		}
		if !bytes.Equal(peer.PubKey, s.LocalNode.PubKey) {
			resp.Nodes = append(resp.Nodes, peer)
		}
	}

	return resp, nil
}

// LocateContent finds Metanet Nodes that store a specific content hash.
func (s *OverlayService) LocateContent(contentHash [32]byte) (*ContentLocation, error) {
	s.contentMu.RLock()
	defer s.contentMu.RUnlock()

	loc, ok := s.contentMap[contentHash]
	if !ok || len(loc.Nodes) == 0 {
		return nil, ErrNoNodesFound
	}
	return loc, nil
}

// RegisterContent registers that a node stores specific content.
func (s *OverlayService) RegisterContent(contentHash [32]byte, node *Node, dealTxID *[32]byte) {
	s.contentMu.Lock()
	defer s.contentMu.Unlock()

	loc, ok := s.contentMap[contentHash]
	if !ok {
		loc = &ContentLocation{
			ContentHash: contentHash,
		}
		s.contentMap[contentHash] = loc
	}

	// Check for duplicate node.
	for _, existing := range loc.Nodes {
		if bytes.Equal(existing.PubKey, node.PubKey) {
			return // Already registered.
		}
	}
	loc.Nodes = append(loc.Nodes, node)

	if dealTxID != nil {
		loc.DealTxIDs = append(loc.DealTxIDs, *dealTxID)
	}
}
