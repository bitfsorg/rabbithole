package client

import "errors"

// ErrOfflineCacheMiss is returned when offline mode has no cached data.
var ErrOfflineCacheMiss = errors.New("client: offline mode, no cached data")

// MetaGetter abstracts the GetMeta call for testability.
type MetaGetter interface {
	GetMeta(pnode, path string) (*MetaResponse, error)
}

// CachedClient wraps a MetaGetter with cache-aware GetMeta.
type CachedClient struct {
	inner   MetaGetter
	cache   *MetaCache
	NoCache bool // skip cache read, still populate
	Offline bool // cache-only, fail on miss
}

// NewCachedClient wraps a MetaGetter with a MetaCache.
func NewCachedClient(inner MetaGetter, cache *MetaCache) *CachedClient {
	return &CachedClient{inner: inner, cache: cache}
}

// GetMeta returns metadata, using cache according to NoCache/Offline settings.
func (cc *CachedClient) GetMeta(pnode, path string) (*MetaResponse, error) {
	// Check cache first (unless NoCache).
	if !cc.NoCache {
		cached, err := cc.cache.Get(pnode, path)
		if err == nil && cached != nil {
			return cached, nil
		}
	}

	// In offline mode, can't fetch from network.
	if cc.Offline {
		return nil, ErrOfflineCacheMiss
	}

	// Fetch from inner client.
	resp, err := cc.inner.GetMeta(pnode, path)
	if err != nil {
		return nil, err
	}

	// Populate cache.
	cc.cache.Put(pnode, path, resp)
	return resp, nil
}
