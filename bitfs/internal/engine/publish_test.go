package engine

import (
	"fmt"
	"strings"
	"testing"
)

// mockDNSResolver is a test DNS resolver that returns configurable results.
type mockDNSResolver struct {
	// records maps DNS names to TXT record slices.
	records map[string][]string
	// err is returned for all lookups if set.
	err error
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
	return &mockDNSResolver{
		records: make(map[string][]string),
	}
}

// --- lookupBitfsPubkey unit tests ---

func TestLookupBitfsPubkey_Found(t *testing.T) {
	pubHex := "02" + strings.Repeat("ab", 32) // 66 hex chars = valid compressed pubkey
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"v=spf1 include:_spf.google.com ~all", // unrelated
		"bitfs=" + pubHex,
	}

	got, err := lookupBitfsPubkey(dns, "example.com")
	if err != nil {
		t.Fatalf("lookupBitfsPubkey: %v", err)
	}
	if got != pubHex {
		t.Errorf("got %s, want %s", got, pubHex)
	}
}

func TestLookupBitfsPubkey_NotFound(t *testing.T) {
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"v=spf1 include:_spf.google.com ~all",
	}

	_, err := lookupBitfsPubkey(dns, "example.com")
	if err == nil {
		t.Error("expected error for missing bitfs= record")
	}
	if !strings.Contains(err.Error(), "no valid bitfs=") {
		t.Errorf("error should mention 'no valid bitfs=', got: %v", err)
	}
}

func TestLookupBitfsPubkey_DNSError(t *testing.T) {
	dns := &mockDNSResolver{err: fmt.Errorf("network unreachable")}

	_, err := lookupBitfsPubkey(dns, "example.com")
	if err == nil {
		t.Error("expected error for DNS failure")
	}
	if !strings.Contains(err.Error(), "DNS lookup") {
		t.Errorf("error should mention DNS lookup, got: %v", err)
	}
}

func TestLookupBitfsPubkey_InvalidLength(t *testing.T) {
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"bitfs=deadbeef", // too short to be a valid pubkey
	}

	_, err := lookupBitfsPubkey(dns, "example.com")
	if err == nil {
		t.Error("expected error for invalid pubkey length")
	}
}

func TestLookupBitfsPubkey_NoRecords(t *testing.T) {
	dns := newMockDNS()
	// No records for _bitfs.example.com at all -> LookupTXT returns error

	_, err := lookupBitfsPubkey(dns, "example.com")
	if err == nil {
		t.Error("expected error when no DNS records exist")
	}
}

func TestLookupBitfsPubkey_WhitespaceHandling(t *testing.T) {
	pubHex := "03" + strings.Repeat("ff", 32) // 66 hex chars, valid secp256k1 point
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{
		"  bitfs=" + pubHex + "  ", // whitespace around record
	}

	got, err := lookupBitfsPubkey(dns, "example.com")
	if err != nil {
		t.Fatalf("lookupBitfsPubkey: %v", err)
	}
	if got != pubHex {
		t.Errorf("got %s, want %s", got, pubHex)
	}
}

// --- Publish with domain tests ---

func TestPublish_WithDomain_DNSNotConfigured(t *testing.T) {
	eng := initTestEngine(t)
	eng.DNS = &mockDNSResolver{err: fmt.Errorf("no such host")}

	result, err := eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Should contain instructions.
	if !strings.Contains(result.Message, "_bitfs.example.com") {
		t.Errorf("message should contain DNS record instructions, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "bitfs=") {
		t.Errorf("message should contain bitfs= format, got: %s", result.Message)
	}
	if result.NodePub == "" {
		t.Error("NodePub should not be empty")
	}

	// Should indicate DNS not configured.
	if !strings.Contains(result.Message, "not yet configured") {
		t.Errorf("message should indicate DNS not configured, got: %s", result.Message)
	}

	// Binding should be stored but unverified.
	binding := eng.State.GetPublishBinding("example.com")
	if binding == nil {
		t.Fatal("binding should be stored")
	}
	if binding.Verified {
		t.Error("binding should not be verified when DNS fails")
	}
	if binding.PubKeyHex != result.NodePub {
		t.Errorf("binding pubkey = %s, want %s", binding.PubKeyHex, result.NodePub)
	}
	if binding.VaultIndex != 0 {
		t.Errorf("binding vault_index = %d, want 0", binding.VaultIndex)
	}
	if binding.Domain != "example.com" {
		t.Errorf("binding domain = %s, want example.com", binding.Domain)
	}
}

func TestPublish_WithDomain_Verified(t *testing.T) {
	eng := initTestEngine(t)

	// Derive the expected pubkey first.
	kp, err := eng.Wallet.DeriveVaultRootKey(0)
	if err != nil {
		t.Fatalf("DeriveVaultRootKey: %v", err)
	}
	expectedPub := fmt.Sprintf("%x", kp.PublicKey.Compressed())

	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + expectedPub}
	eng.DNS = dns

	result, err := eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if !strings.Contains(result.Message, "VERIFIED") {
		t.Errorf("message should indicate VERIFIED, got: %s", result.Message)
	}

	binding := eng.State.GetPublishBinding("example.com")
	if binding == nil {
		t.Fatal("binding should be stored")
	}
	if !binding.Verified {
		t.Error("binding should be verified")
	}
}

func TestPublish_WithDomain_Mismatch(t *testing.T) {
	eng := initTestEngine(t)

	// Use a different pubkey in DNS.
	wrongPub := "02" + strings.Repeat("ff", 32) // valid length but wrong key
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + wrongPub}
	eng.DNS = dns

	result, err := eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if !strings.Contains(result.Message, "MISMATCH") {
		t.Errorf("message should indicate MISMATCH, got: %s", result.Message)
	}

	binding := eng.State.GetPublishBinding("example.com")
	if binding == nil {
		t.Fatal("binding should be stored")
	}
	if binding.Verified {
		t.Error("binding should not be verified on mismatch")
	}
}

func TestPublish_WithDomain_UpdatesExistingBinding(t *testing.T) {
	eng := initTestEngine(t)
	eng.DNS = &mockDNSResolver{err: fmt.Errorf("no such host")}

	// First publish.
	_, err := eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	if err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	eng.State.mu.Lock()
	count := len(eng.State.PublishBindings)
	eng.State.mu.Unlock()
	if count != 1 {
		t.Fatalf("expected 1 binding, got %d", count)
	}

	// Publish again to same domain — should update, not duplicate.
	_, err = eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	if err != nil {
		t.Fatalf("second Publish: %v", err)
	}

	eng.State.mu.Lock()
	count = len(eng.State.PublishBindings)
	eng.State.mu.Unlock()
	if count != 1 {
		t.Errorf("expected 1 binding after re-publish, got %d", count)
	}
}

func TestPublish_WithDomain_NoTransaction(t *testing.T) {
	eng := initTestEngine(t)
	eng.DNS = &mockDNSResolver{err: fmt.Errorf("no such host")}

	result, err := eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "example.com",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	if result.TxHex != "" {
		t.Error("Publish should not produce a transaction")
	}
	if result.TxID != "" {
		t.Error("Publish should not produce a TxID")
	}
}

// --- Publish list (no domain) tests ---

func TestPublish_NoDomain_EmptyList(t *testing.T) {
	eng := initTestEngine(t)
	eng.DNS = newMockDNS()

	result, err := eng.Publish(&PublishOpts{})
	if err != nil {
		t.Fatalf("Publish list: %v", err)
	}

	if !strings.Contains(result.Message, "No publish bindings") {
		t.Errorf("message should say no bindings, got: %s", result.Message)
	}
}

func TestPublish_NoDomain_ListsBindings(t *testing.T) {
	eng := initTestEngine(t)
	dns := newMockDNS()
	eng.DNS = dns

	// Pre-populate some bindings.
	eng.State.SetPublishBinding(&PublishBinding{
		Domain:     "example.com",
		VaultIndex: 0,
		PubKeyHex:  "02" + strings.Repeat("ab", 32),
		Verified:   false,
	})
	eng.State.SetPublishBinding(&PublishBinding{
		Domain:     "test.org",
		VaultIndex: 1,
		PubKeyHex:  "03" + strings.Repeat("cd", 32),
		Verified:   true,
	})

	result, err := eng.Publish(&PublishOpts{})
	if err != nil {
		t.Fatalf("Publish list: %v", err)
	}

	if !strings.Contains(result.Message, "example.com") {
		t.Errorf("message should contain example.com, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "test.org") {
		t.Errorf("message should contain test.org, got: %s", result.Message)
	}
	if !strings.Contains(result.Message, "Publish bindings:") {
		t.Errorf("message should have header, got: %s", result.Message)
	}
}

func TestPublish_NoDomain_ReverifiesBindings(t *testing.T) {
	eng := initTestEngine(t)

	pubHex := "02" + strings.Repeat("ab", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + pubHex}
	eng.DNS = dns

	// Pre-populate an unverified binding.
	eng.State.SetPublishBinding(&PublishBinding{
		Domain:     "example.com",
		VaultIndex: 0,
		PubKeyHex:  pubHex,
		Verified:   false,
	})

	result, err := eng.Publish(&PublishOpts{})
	if err != nil {
		t.Fatalf("Publish list: %v", err)
	}

	if !strings.Contains(result.Message, "VERIFIED") {
		t.Errorf("message should show VERIFIED after re-check, got: %s", result.Message)
	}

	// Binding should now be verified in state.
	binding := eng.State.GetPublishBinding("example.com")
	if binding == nil {
		t.Fatal("binding should exist")
	}
	if !binding.Verified {
		t.Error("binding should be verified after re-check")
	}
}

func TestPublish_NoDomain_ReverifyDetectsMismatch(t *testing.T) {
	eng := initTestEngine(t)

	pubHex := "02" + strings.Repeat("ab", 32)
	wrongPub := "03" + strings.Repeat("ff", 32)
	dns := newMockDNS()
	dns.records["_bitfs.example.com"] = []string{"bitfs=" + wrongPub}
	eng.DNS = dns

	// Pre-populate a previously verified binding.
	eng.State.SetPublishBinding(&PublishBinding{
		Domain:     "example.com",
		VaultIndex: 0,
		PubKeyHex:  pubHex,
		Verified:   true,
	})

	result, err := eng.Publish(&PublishOpts{})
	if err != nil {
		t.Fatalf("Publish list: %v", err)
	}

	if !strings.Contains(result.Message, "MISMATCH") {
		t.Errorf("message should show MISMATCH, got: %s", result.Message)
	}

	// Binding should be unverified now.
	binding := eng.State.GetPublishBinding("example.com")
	if binding == nil {
		t.Fatal("binding should exist")
	}
	if binding.Verified {
		t.Error("binding should be unverified after mismatch")
	}
}

// --- State persistence tests ---

func TestPublishBinding_Persistence(t *testing.T) {
	eng := initTestEngine(t)
	eng.DNS = &mockDNSResolver{err: fmt.Errorf("no such host")}

	_, err := eng.Publish(&PublishOpts{
		VaultIndex: 0,
		Domain:     "persist.example.com",
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Save state.
	if err := eng.State.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Reload state.
	loaded, err := LoadLocalState(eng.State.path)
	if err != nil {
		t.Fatalf("LoadLocalState: %v", err)
	}

	binding := loaded.GetPublishBinding("persist.example.com")
	if binding == nil {
		t.Fatal("binding should survive persistence")
	}
	if binding.Domain != "persist.example.com" {
		t.Errorf("domain = %s, want persist.example.com", binding.Domain)
	}
}

// --- DefaultDNSResolver tests ---

func TestDefaultDNSResolver_NotNil(t *testing.T) {
	r := DefaultDNSResolver()
	if r == nil {
		t.Error("DefaultDNSResolver should not return nil")
	}
}

func TestDefaultDNSResolver_LookupNonexistent(t *testing.T) {
	r := DefaultDNSResolver()
	// This will do a real DNS lookup but to a nonexistent domain.
	_, err := r.LookupTXT("_bitfs.this-domain-does-not-exist-xyzzy-12345.example")
	if err == nil {
		// It's possible (but unlikely) this resolves. Don't fail hard.
		t.Log("unexpectedly resolved nonexistent domain")
	}
}

// --- State helper tests ---

func TestPublishBinding_SetAndGet(t *testing.T) {
	state := NewLocalState("")

	b := &PublishBinding{
		Domain:     "test.com",
		VaultIndex: 0,
		PubKeyHex:  "02abcd",
		Verified:   true,
	}

	state.SetPublishBinding(b)
	got := state.GetPublishBinding("test.com")
	if got == nil {
		t.Fatal("binding should be retrievable")
	}
	if got.PubKeyHex != "02abcd" {
		t.Errorf("pubkey = %s, want 02abcd", got.PubKeyHex)
	}
}

func TestPublishBinding_Update(t *testing.T) {
	state := NewLocalState("")

	state.SetPublishBinding(&PublishBinding{Domain: "test.com", Verified: false})
	state.SetPublishBinding(&PublishBinding{Domain: "test.com", Verified: true})

	if len(state.PublishBindings) != 1 {
		t.Errorf("expected 1 binding after update, got %d", len(state.PublishBindings))
	}
	if !state.PublishBindings[0].Verified {
		t.Error("binding should be updated to verified")
	}
}

func TestPublishBinding_Remove(t *testing.T) {
	state := NewLocalState("")

	state.SetPublishBinding(&PublishBinding{Domain: "a.com"})
	state.SetPublishBinding(&PublishBinding{Domain: "b.com"})

	ok := state.RemovePublishBinding("a.com")
	if !ok {
		t.Error("RemovePublishBinding should return true")
	}
	if len(state.PublishBindings) != 1 {
		t.Errorf("expected 1 binding after remove, got %d", len(state.PublishBindings))
	}
	if state.PublishBindings[0].Domain != "b.com" {
		t.Errorf("remaining binding domain = %s, want b.com", state.PublishBindings[0].Domain)
	}
}

func TestPublishBinding_RemoveNotFound(t *testing.T) {
	state := NewLocalState("")

	ok := state.RemovePublishBinding("nonexistent.com")
	if ok {
		t.Error("RemovePublishBinding should return false for nonexistent domain")
	}
}

func TestPublishBinding_GetNotFound(t *testing.T) {
	state := NewLocalState("")

	got := state.GetPublishBinding("nonexistent.com")
	if got != nil {
		t.Error("GetPublishBinding should return nil for nonexistent domain")
	}
}
