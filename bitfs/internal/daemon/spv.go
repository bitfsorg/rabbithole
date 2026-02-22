package daemon

import (
	"encoding/json"
	"net/http"
)

// spvProofResponse is the JSON response for the SPV proof endpoint.
type spvProofResponse struct {
	TxID        string `json:"txid"`
	Confirmed   bool   `json:"confirmed"`
	BlockHash   string `json:"block_hash,omitempty"`
	BlockHeight uint64 `json:"block_height,omitempty"`
}

// handleSPVProof handles GET /_bitfs/spv/proof/{txid}.
// It performs on-demand SPV verification and returns the result.
func (d *Daemon) handleSPVProof(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	if txid == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PARAM", "txid is required")
		return
	}

	if d.spv == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "SPV_UNAVAILABLE", "SPV service not configured (offline mode)")
		return
	}

	result, err := d.spv.VerifyTx(r.Context(), txid)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "SPV_ERROR", err.Error())
		return
	}

	resp := spvProofResponse{
		TxID:      txid,
		Confirmed: result.Confirmed,
	}
	if result.Confirmed {
		resp.BlockHash = result.BlockHash
		resp.BlockHeight = result.BlockHeight
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
