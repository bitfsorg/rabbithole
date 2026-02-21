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

// handlePKI handles GET /api/v1/pki/{handle} requests.
// It resolves a Paymail handle (alias@domain) to the vault's compressed public key.
func (d *Daemon) handlePKI(w http.ResponseWriter, r *http.Request) {
	handle := r.PathValue("handle")
	if handle == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "missing handle")
		return
	}

	// Parse alias@domain
	parts := strings.SplitN(handle, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_HANDLE", "handle must be in alias@domain format")
		return
	}
	alias := parts[0]

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
