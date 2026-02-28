package buy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 32-byte test private key (hex).
const testKeyHex = "0000000000000000000000000000000000000000000000000000000000000001"

func TestLoadConfig_FromFile(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "buyer.conf")
	require.NoError(t, os.WriteFile(confPath, []byte("wallet_key = "+testKeyHex+"\nnetwork = regtest\n"), 0600))

	cfg, err := LoadConfig(LoadConfigOpts{DataDir: dir})
	require.NoError(t, err)
	assert.NotNil(t, cfg.PrivKey)
	assert.Equal(t, "regtest", cfg.Network)
}

func TestLoadConfig_CLIFlagOverridesFile(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "buyer.conf")
	require.NoError(t, os.WriteFile(confPath, []byte("wallet_key = ffff\nnetwork = mainnet\n"), 0600))

	cfg, err := LoadConfig(LoadConfigOpts{DataDir: dir, WalletKeyFlag: testKeyHex})
	require.NoError(t, err)
	assert.NotNil(t, cfg.PrivKey)
}

func TestLoadConfig_EnvVarOverridesFile(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "buyer.conf")
	require.NoError(t, os.WriteFile(confPath, []byte("wallet_key = ffff\n"), 0600))

	cfg, err := LoadConfig(LoadConfigOpts{DataDir: dir, Env: map[string]string{"BITFS_WALLET_KEY": testKeyHex}})
	require.NoError(t, err)
	assert.NotNil(t, cfg.PrivKey)
}

func TestLoadConfig_NoConfig(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadConfig(LoadConfigOpts{DataDir: dir})
	assert.ErrorIs(t, err, ErrNoBuyerConfig)
}

func TestLoadConfig_ManualUTXO(t *testing.T) {
	cfg, err := LoadConfig(LoadConfigOpts{
		WalletKeyFlag: testKeyHex,
		UTXOFlag:      "aabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccddaabbccdd:0:10000",
	})
	require.NoError(t, err)
	assert.NotNil(t, cfg.PrivKey)
	assert.Len(t, cfg.ManualUTXOs, 1)
	assert.Equal(t, uint64(10000), cfg.ManualUTXOs[0].Amount)
}

func TestLoadConfig_InvalidKey(t *testing.T) {
	_, err := LoadConfig(LoadConfigOpts{WalletKeyFlag: "not-hex"})
	assert.Error(t, err)
}
