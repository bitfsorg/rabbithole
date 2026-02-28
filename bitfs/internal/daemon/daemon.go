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
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
)

// WalletService defines the wallet interface needed by the daemon.
type WalletService interface {
	// DeriveNodePubKey returns the public key for a node at the given vault/path.
	DeriveNodePubKey(vaultIndex uint32, filePath []uint32, hardened []bool) (*ec.PublicKey, error)

	// DeriveNodeKeyPair returns the private/public key pair for a node
	// identified by its compressed public key bytes.
	// Used for capsule computation in the paid content flow.
	DeriveNodeKeyPair(pnode []byte) (*ec.PrivateKey, *ec.PublicKey, error)

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

// SPVService defines the SPV verification interface needed by the daemon.
type SPVService interface {
	// VerifyTx performs on-demand SPV verification of a transaction.
	VerifyTx(ctx context.Context, txid string) (*SPVResult, error)
}

// SPVResult holds the result of an SPV verification.
type SPVResult struct {
	Confirmed   bool   `json:"confirmed"`
	BlockHash   string `json:"block_hash,omitempty"`
	BlockHeight uint64 `json:"block_height,omitempty"`
}

// NodeInfo holds simplified node information for daemon use.
type NodeInfo struct {
	PNode      []byte
	Type       string // "file", "dir", "link"
	MimeType   string
	FileSize   uint64
	KeyHash    []byte
	FileTxID   []byte // 32-byte file transaction ID (binds capsule hash to file identity)
	Access     string // "free", "paid", "private"
	PricePerKB uint64
	Children   []ChildInfo
	Timestamp  uint64
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
	Mainnet    bool           `toml:"mainnet"` // true = mainnet addresses, false = testnet/regtest
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
	TrustProxy     bool            `toml:"trust_proxy"`
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

// ChainService provides blockchain interaction for payment verification.
type ChainService interface {
	BroadcastTx(ctx context.Context, rawTxHex string) (string, error)
}

// Daemon is the main daemon server.
type Daemon struct {
	config  *Config
	wallet  WalletService
	store   ContentStore
	metanet MetanetService
	spv     SPVService   // optional; nil = SPV endpoints disabled
	chain   ChainService // optional; nil = skip broadcast
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

	// Replay protection: maps txid → invoice_id
	usedTxIDs   map[string]string
	usedTxIDsMu sync.Mutex

	// Rate limiting
	rateLimiter *rateLimiter

	// Background cleanup
	stopCleanup    chan struct{}
	cancelEviction context.CancelFunc // stops the invoice eviction goroutine

	// Invoice persistence directory (empty = disabled).
	invoiceDir string

	// Dashboard support
	startedAt  time.Time
	logBuf     *logBuffer
	StorageDir string
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
		config:    config,
		wallet:    wallet,
		store:     store,
		metanet:   metanet,
		sessions:  make(map[string]*Session),
		invoices:  make(map[string]*InvoiceRecord),
		usedTxIDs: make(map[string]string),
	}

	d.startedAt = time.Now()
	d.logBuf = newLogBuffer(200)

	// Initialize rate limiter
	if config.Security.RateLimit.RPM > 0 {
		d.rateLimiter = newRateLimiter(config.Security.RateLimit.RPM, config.Security.RateLimit.Burst)
	}

	// Setup routes
	d.mux = http.NewServeMux()
	d.RegisterRoutes(d.mux)

	d.server = &http.Server{
		Addr:              config.ListenAddr,
		Handler:           d.mux,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	return d, nil
}

// SetSPV attaches an SPV verification service. Must be called before Start.
func (d *Daemon) SetSPV(spv SPVService) {
	d.spv = spv
}

// SetChain attaches a blockchain service for payment broadcast. Must be called before Start.
func (d *Daemon) SetChain(c ChainService) {
	d.chain = c
}

// Start starts the daemon HTTP server.
func (d *Daemon) Start() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.running {
		return ErrAlreadyRunning
	}

	// Validate TLS cert/key files exist before attempting to start.
	if d.config.TLS.Enabled {
		if d.config.TLS.CertFile == "" || d.config.TLS.KeyFile == "" {
			return fmt.Errorf("daemon: TLS enabled but cert or key path is empty")
		}
		if _, err := os.Stat(d.config.TLS.CertFile); err != nil {
			return fmt.Errorf("daemon: TLS cert file not accessible: %w", err)
		}
		if _, err := os.Stat(d.config.TLS.KeyFile); err != nil {
			return fmt.Errorf("daemon: TLS key file not accessible: %w", err)
		}
	}

	d.running = true

	// Recover persisted invoices from a previous run.
	d.recoverPersistedInvoices()

	// Start background cleanup goroutine.
	d.stopCleanup = make(chan struct{})
	go d.runCleanup()

	// Start invoice eviction goroutine (context-based lifecycle).
	evictCtx, evictCancel := context.WithCancel(context.Background())
	d.cancelEviction = evictCancel
	d.startInvoiceEviction(evictCtx)

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

	if d.stopCleanup != nil {
		close(d.stopCleanup)
	}
	if d.cancelEviction != nil {
		d.cancelEviction()
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
// Uses a single write lock to avoid TOCTOU race between expiry check and delete.
func (d *Daemon) GetSession(id string) (*Session, error) {
	d.sessionsMu.Lock()
	defer d.sessionsMu.Unlock()

	session, ok := d.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.ExpiresAt) {
		delete(d.sessions, id)
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

// SetInvoiceDir configures the directory for invoice persistence. Must be called before Start.
func (d *Daemon) SetInvoiceDir(dir string) {
	d.invoiceDir = dir
}

// LogInfo adds a log entry to the dashboard ring buffer.
func (d *Daemon) LogInfo(level, message string) {
	if d.logBuf != nil {
		d.logBuf.Add(level, message)
	}
}

// persistInvoice writes an invoice to disk atomically (write-to-tmp then rename).
func (d *Daemon) persistInvoice(inv *InvoiceRecord) error {
	if d.invoiceDir == "" {
		return nil // persistence disabled
	}
	data, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshal invoice: %w", err)
	}
	if err := os.MkdirAll(d.invoiceDir, 0700); err != nil {
		return fmt.Errorf("create invoice dir: %w", err)
	}
	path := filepath.Join(d.invoiceDir, inv.ID+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write invoice: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename invoice: %w", err)
	}
	return nil
}

// loadInvoice reads a persisted invoice from disk.
func (d *Daemon) loadInvoice(invoiceID string) (*InvoiceRecord, error) {
	path := filepath.Join(d.invoiceDir, invoiceID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var inv InvoiceRecord
	if err := json.Unmarshal(data, &inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

// recoverPersistedInvoices loads paid invoices from disk on startup.
func (d *Daemon) recoverPersistedInvoices() {
	if d.invoiceDir == "" {
		return
	}
	entries, err := os.ReadDir(d.invoiceDir)
	if err != nil {
		return
	}
	d.invoicesMu.Lock()
	defer d.invoicesMu.Unlock()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(d.invoiceDir, entry.Name()))
		if readErr != nil {
			continue
		}
		var inv InvoiceRecord
		if unmarshalErr := json.Unmarshal(data, &inv); unmarshalErr != nil {
			continue
		}
		if inv.Paid && inv.ID != "" {
			d.invoices[inv.ID] = &inv
		}
	}
}

// runCleanup periodically cleans up expired sessions, invoices, and rate limiter entries.
func (d *Daemon) runCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.cleanupExpiredSessions()
			d.evictExpiredInvoices()
			if d.rateLimiter != nil {
				d.rateLimiter.cleanup()
			}
		case <-d.stopCleanup:
			return
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

// cleanup removes client entries that haven't been seen in 24 hours.
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for ip, client := range rl.clients {
		if client.lastCheck.Before(cutoff) {
			delete(rl.clients, ip)
		}
	}
}

// extractClientIP gets the client IP from the request.
// When trustProxy is true, it reads X-Forwarded-For / X-Real-IP headers.
// Otherwise it uses RemoteAddr only (safe against header spoofing).
func extractClientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if idx := strings.Index(xff, ","); idx > 0 {
				return strings.TrimSpace(xff[:idx])
			}
			return strings.TrimSpace(xff)
		}
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return strings.TrimSpace(xri)
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeJSONError writes a JSON error response.
func writeJSONError(w http.ResponseWriter, code int, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, `{"error":{"code":%q,"message":%q,"retry":%t,"cached":false}}`,
		errCode, message, code == http.StatusTooManyRequests || code >= 500)
}
