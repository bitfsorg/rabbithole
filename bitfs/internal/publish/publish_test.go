package publish

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/bitfsorg/libbitfs-go/vault"
	"github.com/bitfsorg/libbitfs-go/wallet"
)

const testPassword = "testpass"

// initTestVault creates a temporary data directory with an initialized wallet
// and returns a ready-to-use Vault.
func initTestVault(t *testing.T) *vault.Vault {
	t.Helper()
	dataDir := t.TempDir()

	mnemonic, err := wallet.GenerateMnemonic(wallet.Mnemonic12Words)
	require.NoError(t, err)
	seed, err := wallet.SeedFromMnemonic(mnemonic, "")
	require.NoError(t, err)

	encrypted, err := wallet.EncryptSeed(seed, testPassword)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "wallet.enc"), encrypted, 0600))

	w, err := wallet.NewWallet(seed, &wallet.MainNet)
	require.NoError(t, err)
	wState := wallet.NewWalletState()
	_, err = w.CreateVault(wState, "default")
	require.NoError(t, err)

	stateData, err := json.MarshalIndent(wState, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dataDir, "state.json"), stateData, 0600))

	v, err := vault.New(dataDir, testPassword)
	require.NoError(t, err)
	t.Cleanup(func() { v.Close() })

	return v
}

// mockDNSResolver is a test DNS resolver that returns configurable results.
type mockDNSResolver struct {
	records map[string][]string
	err     error
}

func (m *mockDNSResolver) LookupTXT(name string) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	recs, ok := m.records[name]
	if !ok {
		return nil, fmt.Errorf("no such host: %s", name)
	}
	return recs, nil
}

func newMockDNS() *mockDNSResolver {
	return &mockDNSResolver{records: make(map[string][]string)}
}

// --- lookupBitfsPubkey unit tests ---

func TestLookupBitfsPubkey_Found(t *testing.T) {
	pubHex := "02" + strings.Repeat("ab", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"v=spf1 include:_spf.google.com ~all",
		"bitfs=" + pubHex,
	}

	got, err := lookupBitfsPubkey(dns, "example.com")
	require.NoError(t, err)
	assert.Equal(t, pubHex, got)
}

func TestLookupBitfsPubkey_NotFound(t *testing.T) {
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"v=spf1 include:_spf.google.com ~all",
	}

	_, err := lookupBitfsPubkey(dns, "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no valid bitfs=")
}

func TestLookupBitfsPubkey_DNSError(t *testing.T) {
	dns := &mockDNSResolver{err: fmt.Errorf("network unreachable")}

	_, err := lookupBitfsPubkey(dns, "example.com")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DNS lookup")
}

func TestLookupBitfsPubkey_InvalidLength(t *testing.T) {
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"bitfs=deadbeef",
	}

	_, err := lookupBitfsPubkey(dns, "example.com")
	require.Error(t, err)
}

func TestLookupBitfsPubkey_NoRecords(t *testing.T) {
	dns := newMockDNS()

	_, err := lookupBitfsPubkey(dns, "example.com")
	require.Error(t, err)
}

func TestLookupBitfsPubkey_WhitespaceHandling(t *testing.T) {
	pubHex := "03" + strings.Repeat("ff", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"  bitfs=" + pubHex + "  ",
	}

	got, err := lookupBitfsPubkey(dns, "example.com")
	require.NoError(t, err)
	assert.Equal(t, pubHex, got)
}

// --- Publish with domain tests ---

func TestPublish_WithDomain_DNSNotConfigured(t *testing.T) {
	v := initTestVault(t)
	dns := &mockDNSResolver{err: fmt.Errorf("no such host")}

	result, err := Publish(v, dns, &PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	require.NoError(t, err)

	assert.Contains(t, result.Message, "_bitfs.example.com")
	assert.Contains(t, result.Message, "bitfs=")
	assert.NotEmpty(t, result.NodePub)
	assert.Contains(t, result.Message, "not yet configured")

	binding := v.State.GetPublishBinding("example.com")
	require.NotNil(t, binding)
	assert.False(t, binding.Verified)
	assert.Equal(t, result.NodePub, binding.PubKeyHex)
	assert.Equal(t, uint32(0), binding.VaultIndex)
	assert.Equal(t, "example.com", binding.Domain)
}

func TestPublish_WithDomain_Verified(t *testing.T) {
	v := initTestVault(t)

	kp, err := v.Wallet.DeriveVaultRootKey(0)
	require.NoError(t, err)
	expectedPub := fmt.Sprintf("%x", kp.PublicKey.Compressed())

	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + expectedPub}

	result, err := Publish(v, dns, &PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "VERIFIED")

	binding := v.State.GetPublishBinding("example.com")
	require.NotNil(t, binding)
	assert.True(t, binding.Verified)
}

func TestPublish_WithDomain_Mismatch(t *testing.T) {
	v := initTestVault(t)

	wrongPub := "02" + strings.Repeat("ff", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + wrongPub}

	result, err := Publish(v, dns, &PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "MISMATCH")

	binding := v.State.GetPublishBinding("example.com")
	require.NotNil(t, binding)
	assert.False(t, binding.Verified)
}

func TestPublish_WithDomain_UpdatesExistingBinding(t *testing.T) {
	v := initTestVault(t)
	dns := &mockDNSResolver{err: fmt.Errorf("no such host")}

	_, err := Publish(v, dns, &PublishOpts{VaultIndex: 0, Domain: "example.com"})
	require.NoError(t, err)
	assert.Len(t, v.State.PublishBindings, 1)

	// Publish again — should update, not duplicate.
	_, err = Publish(v, dns, &PublishOpts{VaultIndex: 0, Domain: "example.com"})
	require.NoError(t, err)
	assert.Len(t, v.State.PublishBindings, 1)
}

func TestPublish_WithDomain_NoTransaction(t *testing.T) {
	v := initTestVault(t)
	dns := &mockDNSResolver{err: fmt.Errorf("no such host")}

	result, err := Publish(v, dns, &PublishOpts{VaultIndex: 0, Domain: "example.com"})
	require.NoError(t, err)
	assert.Empty(t, result.TxHex)
	assert.Empty(t, result.TxID)
}

// --- Publish list (no domain) tests ---

func TestPublish_NoDomain_EmptyList(t *testing.T) {
	v := initTestVault(t)
	dns := newMockDNS()

	result, err := Publish(v, dns, &PublishOpts{})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "No publish bindings")
}

func TestPublish_NoDomain_ListsBindings(t *testing.T) {
	v := initTestVault(t)
	dns := newMockDNS()

	v.State.SetPublishBinding(&vault.PublishBinding{
		Domain: "example.com", VaultIndex: 0,
		PubKeyHex: "02" + strings.Repeat("ab", 32), Verified: false,
	})
	v.State.SetPublishBinding(&vault.PublishBinding{
		Domain: "test.org", VaultIndex: 1,
		PubKeyHex: "03" + strings.Repeat("cd", 32), Verified: true,
	})

	result, err := Publish(v, dns, &PublishOpts{})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "example.com")
	assert.Contains(t, result.Message, "test.org")
	assert.Contains(t, result.Message, "Publish bindings:")
}

func TestPublish_NoDomain_ReverifiesBindings(t *testing.T) {
	v := initTestVault(t)

	pubHex := "02" + strings.Repeat("ab", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + pubHex}

	v.State.SetPublishBinding(&vault.PublishBinding{
		Domain: "example.com", VaultIndex: 0,
		PubKeyHex: pubHex, Verified: false,
	})

	result, err := Publish(v, dns, &PublishOpts{})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "VERIFIED")

	binding := v.State.GetPublishBinding("example.com")
	require.NotNil(t, binding)
	assert.True(t, binding.Verified)
}

func TestPublish_NoDomain_ReverifyDetectsMismatch(t *testing.T) {
	v := initTestVault(t)

	pubHex := "02" + strings.Repeat("ab", 32)
	wrongPub := "03" + strings.Repeat("ff", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + wrongPub}

	v.State.SetPublishBinding(&vault.PublishBinding{
		Domain: "example.com", VaultIndex: 0,
		PubKeyHex: pubHex, Verified: true,
	})

	result, err := Publish(v, dns, &PublishOpts{})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "MISMATCH")

	binding := v.State.GetPublishBinding("example.com")
	require.NotNil(t, binding)
	assert.False(t, binding.Verified)
}

// --- State persistence tests ---

func TestPublishBinding_Persistence(t *testing.T) {
	v := initTestVault(t)
	dns := &mockDNSResolver{err: fmt.Errorf("no such host")}

	_, err := Publish(v, dns, &PublishOpts{VaultIndex: 0, Domain: "persist.example.com"})
	require.NoError(t, err)

	require.NoError(t, v.State.Save())

	loaded, err := vault.LoadLocalState(filepath.Join(v.DataDir, "nodes.json"))
	require.NoError(t, err)

	binding := loaded.GetPublishBinding("persist.example.com")
	require.NotNil(t, binding)
	assert.Equal(t, "persist.example.com", binding.Domain)
}

// --- DefaultDNSResolver tests ---

func TestDefaultDNSResolver_NotNil(t *testing.T) {
	r := DefaultDNSResolver()
	assert.NotNil(t, r)
}

func TestDefaultDNSResolver_LookupNonexistent(t *testing.T) {
	r := DefaultDNSResolver()
	_, err := r.LookupTXT("_bitfs.this-domain-does-not-exist-xyzzy-12345.example")
	if err == nil {
		t.Log("unexpectedly resolved nonexistent domain")
	}
}

// --- State helper tests ---

func TestPublishBinding_SetAndGet(t *testing.T) {
	state := vault.NewLocalState("")

	b := &vault.PublishBinding{
		Domain: "test.com", VaultIndex: 0,
		PubKeyHex: "02abcd", Verified: true,
	}

	state.SetPublishBinding(b)
	got := state.GetPublishBinding("test.com")
	require.NotNil(t, got)
	assert.Equal(t, "02abcd", got.PubKeyHex)
}

func TestPublishBinding_Update(t *testing.T) {
	state := vault.NewLocalState("")

	state.SetPublishBinding(&vault.PublishBinding{Domain: "test.com", Verified: false})
	state.SetPublishBinding(&vault.PublishBinding{Domain: "test.com", Verified: true})

	assert.Len(t, state.PublishBindings, 1)
	assert.True(t, state.PublishBindings[0].Verified)
}

func TestPublishBinding_Remove(t *testing.T) {
	state := vault.NewLocalState("")

	state.SetPublishBinding(&vault.PublishBinding{Domain: "a.com"})
	state.SetPublishBinding(&vault.PublishBinding{Domain: "b.com"})

	ok := state.RemovePublishBinding("a.com")
	assert.True(t, ok)
	assert.Len(t, state.PublishBindings, 1)
	assert.Equal(t, "b.com", state.PublishBindings[0].Domain)
}

func TestPublishBinding_RemoveNotFound(t *testing.T) {
	state := vault.NewLocalState("")

	ok := state.RemovePublishBinding("nonexistent.com")
	assert.False(t, ok)
}

func TestPublishBinding_GetNotFound(t *testing.T) {
	state := vault.NewLocalState("")

	got := state.GetPublishBinding("nonexistent.com")
	assert.Nil(t, got)
}

// --- Unpublish tests ---

func TestUnpublish_Success(t *testing.T) {
	v := initTestVault(t)

	v.State.SetPublishBinding(&vault.PublishBinding{
		Domain: "example.com", VaultIndex: 0,
		PubKeyHex: "aabbcc", Verified: true,
	})

	result, err := Unpublish(v, &UnpublishOpts{Domain: "example.com"})
	require.NoError(t, err)
	assert.Contains(t, result.Message, "Removed")
	assert.Contains(t, result.Message, "example.com")
	assert.Nil(t, v.State.GetPublishBinding("example.com"))
}

func TestUnpublish_NotFound(t *testing.T) {
	v := initTestVault(t)

	_, err := Unpublish(v, &UnpublishOpts{Domain: "nope.com"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no publish binding")
}

func TestUnpublish_EmptyDomain(t *testing.T) {
	v := initTestVault(t)

	_, err := Unpublish(v, &UnpublishOpts{Domain: ""})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "domain is required")
}
