package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
)

// pkiResponse is the Paymail PKI response per BSV Alias specification.
type pkiResponse struct {
	BSVAlias string `json:"bsvalias"`
	Handle   string `json:"handle"`
	PubKey   string `json:"pubkey"`
}

// publicProfileResponse is the Paymail public-profile response (BRFC f12f968c92d6).
type publicProfileResponse struct {
	Name   string `json:"name"`
	Domain string `json:"domain"`
	Avatar string `json:"avatar"`
}

// verifyPubKeyResponse is the Paymail verify-pubkey response (BRFC a9f510c16bde).
type verifyPubKeyResponse struct {
	Handle string `json:"handle"`
	PubKey string `json:"pubkey"`
	Match  bool   `json:"match"`
}

// parseHandle extracts alias and domain from a handle string in "alias@domain" format.
// Returns alias, domain, ok.
func parseHandle(handle string) (string, string, bool) {
	if handle == "" {
		return "", "", false
	}
	parts := strings.SplitN(handle, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// handlePKI handles GET /api/v1/pki/{handle} requests.
// It resolves a Paymail handle (alias@domain) to the vault's compressed public key.
func (d *Daemon) handlePKI(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	alias, _, ok := parseHandle(handle)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "handle must be in alias@domain format")
		return
	}

	// Look up vault public key by alias
	pubKeyHex, err := d.wallet.GetVaultPubKey(alias)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "unknown alias: "+alias)
		return
	}

	resp := pkiResponse{
		BSVAlias: "1.0",
		Handle:   handle,
		PubKey:   pubKeyHex,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handlePublicProfile handles GET /api/v1/public-profile/{handle} requests.
// It returns the public profile for a Paymail handle (alias@domain).
func (d *Daemon) handlePublicProfile(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	alias, domain, ok := parseHandle(handle)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "handle must be in alias@domain format")
		return
	}

	// Verify alias exists by looking up the vault public key.
	if _, err := d.wallet.GetVaultPubKey(alias); err != nil {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "unknown alias: "+alias)
		return
	}

	resp := publicProfileResponse{
		Name:   alias,
		Domain: domain,
		Avatar: "", // Future: from config
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleVerifyPubKey handles GET /api/v1/verify/{handle}/{pubkey} requests.
// It verifies whether a given public key matches the one associated with a Paymail handle.
// For unknown aliases, it returns match=false without revealing alias existence (no 404).
func (d *Daemon) handleVerifyPubKey(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	pubkeyHex := r.PathValue("pubkey")

	alias, _, ok := parseHandle(handle)
	if !ok {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "handle must be in alias@domain format")
		return
	}

	// Look up vault public key. For unknown alias, match is simply false.
	match := false
	if vaultPubKeyHex, err := d.wallet.GetVaultPubKey(alias); err == nil {
		match = vaultPubKeyHex == pubkeyHex
	}

	resp := verifyPubKeyResponse{
		Handle: handle,
		PubKey: pubkeyHex,
		Match:  match,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
