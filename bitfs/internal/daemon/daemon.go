// Package daemon implements the BitFS LFCP (Local Full-Copy Peer) HTTP server.
//
// It serves content retrieval, Metanet metadata queries, Method 42 ECDH handshake
// for identity verification, x402 payment handling for paid content, and content
// negotiation for different client types (HTML, Markdown, JSON).
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sync"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

// WalletService defines the wallet interface needed by the daemon.
type WalletService interface {
	// DeriveNodePubKey returns the public key for a node at the given vault/path.
	DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error)

	// GetSellerKeyPair returns the seller's key pair for the daemon.
	// Returns (privateKey, publicKey) for the default vault root.
	GetSellerKeyPair() (*ec.PrivateKey, *ec.PublicKey, error)

	// GetVaultPubKey resolves a vault alias to its compressed hex public key.
	GetVaultPubKey(alias string) (string, error)
}

// ContentStore defines the content storage interface needed by the daemon.
type ContentStore interface {
	// Get retrieves encrypted content by key hash.
	Get(keyHash []byte) ([]byte, error)

	// Has checks if content exists for the given key hash.
	Has(keyHash []byte) (bool, error)

	// Size returns the size of stored content.
	Size(keyHash []byte) (int64, error)
}

// MetanetService defines the Metanet service interface needed by the daemon.
type MetanetService interface {
	// GetNodeByPath resolves a filesystem path and returns the node.
	GetNodeByPath(path string) (*NodeInfo, error)
}

// NodeInfo holds simplified node information for daemon use.
type NodeInfo struct {
	PNode      []byte
	Type       string // "file", "dir", "link"
	MimeType   string
	FileSize   uint64
	KeyHash    []byte
	Access     string // "free", "paid", "private"
	PricePerKB uint64
	Children   []ChildInfo
	Domain     string
}

// ChildInfo holds simplified child entry information.
type ChildInfo struct {
	Name string
	Type string
}

// Config holds daemon configuration.
type Config struct {
	ListenAddr string         `toml:"listen"`
	TLS        TLSConfig      `toml:"tls"`
	X402       X402Config     `toml:"x402"`
	Security   SecurityConfig `toml:"security"`
	Storage    StorageConfig  `toml:"storage"`
	Log        LogConfig      `toml:"log"`
}

// TLSConfig holds TLS configuration.
type TLSConfig struct {
	Enabled  bool   `toml:"enabled"`
	CertFile string `toml:"cert"`
	KeyFile  string `toml:"key"`
}

// X402Config holds x402 payment configuration.
type X402Config struct {
	Enabled       bool   `toml:"enabled"`
	PricePerMB    uint64 `toml:"price_per_mb"`
	FreeQuotaMB   uint64 `toml:"free_quota_mb"`
	InvoiceExpiry int64  `toml:"invoice_expiry"`
}

// SecurityConfig holds security configuration.
type SecurityConfig struct {
	RateLimit      RateLimitConfig `toml:"rate_limit"`
	CORS           CORSConfig      `toml:"cors"`
	MaxRequestSize string          `toml:"max_request_size"`
}

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	RPM   int `toml:"rpm"`
	Burst int `toml:"burst"`
}

// CORSConfig holds CORS configuration.
type CORSConfig struct {
	Origins []string `toml:"origins"`
	Methods []string `toml:"methods"`
}

// StorageConfig holds storage configuration.
type StorageConfig struct {
	DataDir   string `toml:"data_dir"`
	DBPath    string `toml:"db_path"`
	CacheSize string `toml:"cache_size"`
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

// Session represents an authenticated Method 42 session.
type Session struct {
	ID         string
	BuyerPub   []byte // Buyer's compressed public key
	SellerPub  []byte // Seller's compressed public key (P_node)
	SessionKey []byte // Derived session encryption key
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// IsExpired returns true if the session has expired.
func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		ListenAddr: ":8080",
		TLS: TLSConfig{
			Enabled: false,
		},
		X402: X402Config{
			Enabled:       false,
			PricePerMB:    100,
			FreeQuotaMB:   10,
			InvoiceExpiry: 3600,
		},
		Security: SecurityConfig{
			RateLimit: RateLimitConfig{
				RPM:   60,
				Burst: 20,
			},
			CORS: CORSConfig{
				Origins: []string{"*"},
				Methods: []string{"GET", "POST", "OPTIONS"},
			},
			MaxRequestSize: "10MB",
		},
		Storage: StorageConfig{
			DataDir:   "./data",
			DBPath:    "./data/bitfs.db",
			CacheSize: "256MB",
		},
		Log: LogConfig{
			Level: "info",
		},
	}
}

// Daemon is the main daemon server.
type Daemon struct {
	config  *Config
	wallet  WalletService
	store   ContentStore
	metanet MetanetService
	server  *http.Server
	mux     *http.ServeMux
	running bool
	mu      sync.Mutex

	// Session management
	sessions   map[string]*Session
	sessionsMu sync.RWMutex

	// Invoice management
	invoices   map[string]*InvoiceRecord
	invoicesMu sync.RWMutex

	// Rate limiting
	rateLimiter *rateLimiter
}

// New creates a new Daemon instance.
func New(config *Config, wallet WalletService, store ContentStore, metanet MetanetService) (*Daemon, error) {
	if config == nil {
		return nil, ErrNilConfig
	}
	if wallet == nil {
		return nil, ErrNilWallet
	}
	if store == nil {
		return nil, ErrNilStore
	}

	d := &Daemon{
		config:   config,
		wallet:   wallet,
		store:    store,
		metanet:  metanet,
		sessions: make(map[string]*Session),
		invoices: make(map[string]*InvoiceRecord),
	}

	// Initialize rate limiter
	if config.Security.RateLimit.RPM > 0 {
		d.rateLimiter = newRateLimiter(config.Security.RateLimit.RPM, config.Security.RateLimit.Burst)
	}

	// Setup routes
	d.mux = http.NewServeMux()
	d.RegisterRoutes(d.mux)

	d.server = &http.Server{
		Addr:    config.ListenAddr,
		Handler: d.mux,
	}

	return d, nil
}

// Start starts the daemon HTTP server.
func (d *Daemon) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return ErrAlreadyRunning
	}

	d.running = true

	// Start in background
	go func() {
		var err error
		if d.config.TLS.Enabled {
			err = d.server.ListenAndServeTLS(d.config.TLS.CertFile, d.config.TLS.KeyFile)
		} else {
			err = d.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			d.mu.Lock()
			d.running = false
			d.mu.Unlock()
		}
	}()

	return nil
}

// Stop gracefully shuts down the daemon.
func (d *Daemon) Stop(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.running {
		return ErrNotRunning
	}

	err := d.server.Shutdown(ctx)
	d.running = false
	return err
}

// Handler returns the daemon's HTTP handler for testing.
func (d *Daemon) Handler() http.Handler {
	return d.mux
}

// CreateSession creates a new session from handshake data.
func (d *Daemon) CreateSession(buyerPub, sellerPub []byte, sharedX, nonceB, nonceS []byte, ttl time.Duration) *Session {
	// Compute session key: SHA256(shared.x || nonce_b || nonce_s)
	h := sha256.New()
	h.Write(sharedX)
	h.Write(nonceB)
	h.Write(nonceS)
	sessionKey := h.Sum(nil)

	sessionID := hex.EncodeToString(sessionKey[:16])
	now := time.Now()

	session := &Session{
		ID:         sessionID,
		BuyerPub:   buyerPub,
		SellerPub:  sellerPub,
		SessionKey: sessionKey,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}

	d.sessionsMu.Lock()
	d.sessions[sessionID] = session
	d.sessionsMu.Unlock()

	return session
}

// GetSession retrieves a session by ID.
func (d *Daemon) GetSession(id string) (*Session, error) {
	d.sessionsMu.RLock()
	session, ok := d.sessions[id]
	d.sessionsMu.RUnlock()

	if !ok {
		return nil, ErrSessionNotFound
	}

	if session.IsExpired() {
		d.sessionsMu.Lock()
		delete(d.sessions, id)
		d.sessionsMu.Unlock()
		return nil, ErrSessionExpired
	}

	return session, nil
}

// cleanupExpiredSessions removes expired sessions from the map.
func (d *Daemon) cleanupExpiredSessions() {
	d.sessionsMu.Lock()
	defer d.sessionsMu.Unlock()

	now := time.Now()
	for id, session := range d.sessions {
		if now.After(session.ExpiresAt) {
			delete(d.sessions, id)
		}
	}
}

// rateLimiter implements a simple per-IP token bucket rate limiter.
type rateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientRate
	rpm     int
	burst   int
}

type clientRate struct {
	tokens    float64
	lastCheck time.Time
}

func newRateLimiter(rpm, burst int) *rateLimiter {
	return &rateLimiter{
		clients: make(map[string]*clientRate),
		rpm:     rpm,
		burst:   burst,
	}
}

// Allow returns true if the request from the given IP should be allowed.
func (rl *rateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	client, ok := rl.clients[ip]
	if !ok {
		client = &clientRate{
			tokens:    float64(rl.burst),
			lastCheck: now,
		}
		rl.clients[ip] = client
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(client.lastCheck).Seconds()
	client.tokens += elapsed * (float64(rl.rpm) / 60.0)
	if client.tokens > float64(rl.burst) {
		client.tokens = float64(rl.burst)
	}
	client.lastCheck = now

	if client.tokens < 1 {
		return false
	}

	client.tokens--
	return true
}

// extractClientIP gets the client IP from the request.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return xff
	}
	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	return r.RemoteAddr
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, code int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, `{"error":{"code":%q,"message":%q,"retry":%t,"cached":false}}`,
		errCode, message, code == http.StatusTooManyRequests || code >= 500)
}
