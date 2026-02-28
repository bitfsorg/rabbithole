package buy

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	ec "github.com/bsv-blockchain/go-sdk/primitives/ec"
	"github.com/bitfsorg/libbitfs-go/x402"
)

// ErrNoBuyerConfig is returned when no wallet key is configured.
var ErrNoBuyerConfig = errors.New("buyer: no wallet key configured (set --wallet-key, BITFS_WALLET_KEY, or ~/.bitfs/buyer.conf)")

// BuyerConfig holds the buyer's wallet configuration.
type BuyerConfig struct {
	PrivKey     *ec.PrivateKey
	Network     string           // mainnet|testnet|regtest
	ManualUTXOs []*x402.HTLCUTXO // Manually specified UTXOs (from --utxo flag)
}

// LoadConfigOpts holds options for loading buyer config.
type LoadConfigOpts struct {
	DataDir       string            // Default: ~/.bitfs
	WalletKeyFlag string            // --wallet-key CLI flag (highest priority)
	UTXOFlag      string            // --utxo CLI flag
	Env           map[string]string // Environment variables (nil = use os.Getenv)
}

// LoadConfig loads buyer configuration with priority: CLI flag > env var > file.
func LoadConfig(opts LoadConfigOpts) (*BuyerConfig, error) {
	cfg := &BuyerConfig{Network: "mainnet"}

	var keyHex string
	var fileNetwork string

	// Layer 1: config file (lowest priority).
	dataDir := opts.DataDir
	if dataDir == "" {
		home, _ := os.UserHomeDir()
		dataDir = filepath.Join(home, ".bitfs")
	}
	confPath := filepath.Join(dataDir, "buyer.conf")
	if fileKey, network, err := readBuyerConf(confPath); err == nil {
		keyHex = fileKey
		fileNetwork = network
	}

	// Layer 2: environment variable.
	envKey := envGet(opts.Env, "BITFS_WALLET_KEY")
	if envKey != "" {
		keyHex = envKey
	}

	// Layer 3: CLI flag (highest priority).
	if opts.WalletKeyFlag != "" {
		keyHex = opts.WalletKeyFlag
	}

	if keyHex == "" {
		return nil, ErrNoBuyerConfig
	}

	// Parse private key.
	privKey, err := parsePrivateKey(keyHex)
	if err != nil {
		return nil, fmt.Errorf("buyer: invalid wallet key: %w", err)
	}
	cfg.PrivKey = privKey

	if fileNetwork != "" {
		cfg.Network = fileNetwork
	}

	// Parse manual UTXO if provided.
	if opts.UTXOFlag != "" {
		utxo, err := ParseUTXOFlag(opts.UTXOFlag)
		if err != nil {
			return nil, fmt.Errorf("buyer: invalid --utxo: %w", err)
		}
		// Set ScriptPubKey to buyer's P2PKH.
		utxo.ScriptPubKey = BuildP2PKHScript(privKey.PubKey().Hash())
		cfg.ManualUTXOs = []*x402.HTLCUTXO{utxo}
	}

	return cfg, nil
}

// readBuyerConf reads wallet_key and network from a buyer.conf file.
func readBuyerConf(path string) (keyHex, network string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		switch key {
		case "wallet_key":
			keyHex = value
		case "network":
			network = value
		}
	}
	return keyHex, network, scanner.Err()
}

// parsePrivateKey parses a hex-encoded private key (exactly 32 bytes).
func parsePrivateKey(hexStr string) (*ec.PrivateKey, error) {
	keyBytes, err := hex.DecodeString(hexStr)
	if err != nil {
		return nil, fmt.Errorf("invalid hex: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("invalid private key: expected 32 bytes, got %d", len(keyBytes))
	}
	privKey, _ := ec.PrivateKeyFromBytes(keyBytes)
	if privKey == nil {
		return nil, fmt.Errorf("invalid private key bytes")
	}
	return privKey, nil
}

// ParseUTXOFlag parses a UTXO from the --utxo flag (format: txid:vout:amount).
func ParseUTXOFlag(s string) (*x402.HTLCUTXO, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("expected txid:vout:amount")
	}
	txid, err := hex.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("invalid txid hex: %w", err)
	}
	if len(txid) != 32 {
		return nil, fmt.Errorf("txid must be 32 bytes")
	}
	vout, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid vout: %w", err)
	}
	amount, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid amount: %w", err)
	}
	return &x402.HTLCUTXO{
		TxID:   txid,
		Vout:   uint32(vout),
		Amount: amount,
	}, nil
}

// BuildP2PKHScript builds a standard P2PKH locking script from a 20-byte pubkey hash.
func BuildP2PKHScript(pkh []byte) []byte {
	s := make([]byte, 0, 25)
	s = append(s, 0x76, 0xa9, 0x14) // OP_DUP OP_HASH160 OP_PUSHDATA(20)
	s = append(s, pkh...)
	s = append(s, 0x88, 0xac) // OP_EQUALVERIFY OP_CHECKSIG
	return s
}

// envGet returns the value from the env map, falling back to os.Getenv if nil.
func envGet(env map[string]string, key string) string {
	if env != nil {
		return env[key]
	}
	return os.Getenv(key)
}
