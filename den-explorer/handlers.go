package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
)

// Server holds shared state for HTTP handlers.
type Server struct {
	explorer  *Explorer
	templates *Templates
}

// NewServer creates a new HTTP server.
func NewServer(explorer *Explorer, templates *Templates) *Server {
	return &Server{explorer: explorer, templates: templates}
}

// Routes registers all HTTP routes.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /block/{hash}", s.handleBlock)
	mux.HandleFunc("GET /block/height/{n}", s.handleBlockByHeight)
	mux.HandleFunc("GET /tx/{txid}", s.handleTx)
	mux.HandleFunc("GET /address/{addr}", s.handleAddress)
	mux.HandleFunc("GET /search", s.handleSearch)
	mux.HandleFunc("GET /metanet/{txid}", s.handleMetanet)
	mux.HandleFunc("GET /spv/{txid}", s.handleSPV)
	mux.HandleFunc("GET /method42/{txid}", s.handleMethod42)
	return mux
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx := r.Context()
	chain, err := s.explorer.GetChainInfo(ctx)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	blocks, err := s.explorer.GetRecentBlocks(ctx, 20)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	data := map[string]interface{}{
		"Title":  "Home",
		"Chain":  chain,
		"Blocks": blocks,
	}
	s.render(w, "home.html", data)
}

func (s *Server) handleBlock(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	ctx := r.Context()

	block, err := s.explorer.GetBlock(ctx, hash)
	if err != nil {
		http.Error(w, fmt.Sprintf("block not found: %v", err), 404)
		return
	}

	data := map[string]interface{}{
		"Title": fmt.Sprintf("Block %d", block.Height),
		"Block": block,
	}
	s.render(w, "block.html", data)
}

func (s *Server) handleBlockByHeight(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.ParseInt(r.PathValue("n"), 10, 64)
	if err != nil {
		http.Error(w, "invalid height", 400)
		return
	}

	hash, err := s.explorer.GetBlockHash(r.Context(), n)
	if err != nil {
		http.Error(w, fmt.Sprintf("block not found: %v", err), 404)
		return
	}
	http.Redirect(w, r, "/block/"+hash, http.StatusFound)
}

func (s *Server) handleTx(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	vtx, err := s.explorer.GetVerboseTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	// Try to decode as Metanet tx
	rawBytes, err := s.explorer.rpc.GetRawTx(ctx, txid)
	if err != nil {
		log.Printf("handlers: GetRawTx(%s): %v", truncHash(txid), err)
	}
	var decoded *DecodedMetanet
	if rawBytes != nil {
		decoded = DecodeMetanetTx(rawBytes)
	}

	data := map[string]interface{}{
		"Title":   fmt.Sprintf("Tx %s", truncHash(txid)),
		"Tx":      vtx,
		"Metanet": decoded,
	}
	s.render(w, "tx.html", data)
}

func (s *Server) handleAddress(w http.ResponseWriter, r *http.Request) {
	addr := r.PathValue("addr")
	ctx := r.Context()

	utxos, err := s.explorer.rpc.ListUnspent(ctx, addr)
	if err != nil {
		http.Error(w, fmt.Sprintf("address lookup failed: %v", err), 500)
		return
	}

	data := map[string]interface{}{
		"Title":   fmt.Sprintf("Address %s", truncHash(addr)),
		"Address": addr,
		"UTXOs":   utxos,
	}
	s.render(w, "address.html", data)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	path, err := s.explorer.SearchQuery(r.Context(), q)
	if err != nil {
		data := map[string]interface{}{
			"Title": "Search",
			"Query": q,
			"Error": err.Error(),
		}
		s.render(w, "search.html", data)
		return
	}
	http.Redirect(w, r, path, http.StatusFound)
}

func (s *Server) handleMetanet(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	rawBytes, err := s.explorer.rpc.GetRawTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	decoded := DecodeMetanetTx(rawBytes)
	if !decoded.IsMetanet {
		http.Error(w, "not a Metanet transaction", 400)
		return
	}

	var tlvFields []TLVField
	if decoded.RawPayload != "" {
		tlvFields = DecodeTLVFields(decoded.RawPayload)
	}

	data := map[string]interface{}{
		"Title":     fmt.Sprintf("Metanet %s", truncHash(txid)),
		"TxID":      txid,
		"Metanet":   decoded,
		"TLVFields": tlvFields,
	}
	s.render(w, "metanet.html", data)
}

func (s *Server) handleSPV(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	proof, err := s.explorer.rpc.GetMerkleProof(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("proof not available: %v", err), 404)
		return
	}

	headerBytes, err := s.explorer.rpc.GetBlockHeader(ctx, proof.BlockHash)
	if err != nil {
		log.Printf("handlers: GetBlockHeader(%s): %v", truncHash(proof.BlockHash), err)
	}
	verification := VerifySPVProof(proof, headerBytes)

	data := map[string]interface{}{
		"Title":        fmt.Sprintf("SPV Proof %s", truncHash(txid)),
		"TxID":         txid,
		"Verification": verification,
	}
	s.render(w, "spv.html", data)
}

func (s *Server) handleMethod42(w http.ResponseWriter, r *http.Request) {
	txid := r.PathValue("txid")
	ctx := r.Context()

	rawBytes, err := s.explorer.rpc.GetRawTx(ctx, txid)
	if err != nil {
		http.Error(w, fmt.Sprintf("tx not found: %v", err), 404)
		return
	}

	decoded := DecodeMetanetTx(rawBytes)
	if !decoded.IsMetanet {
		http.Error(w, "not a Metanet transaction", 400)
		return
	}

	analysis := AnalyzeMethod42(txid, decoded.Node, decoded.PNode)

	data := map[string]interface{}{
		"Title":    fmt.Sprintf("Method 42 %s", truncHash(txid)),
		"TxID":     txid,
		"Analysis": analysis,
	}
	s.render(w, "method42.html", data)
}

func (s *Server) render(w http.ResponseWriter, name string, data interface{}) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.Render(w, name, data); err != nil {
		http.Error(w, err.Error(), 500)
	}
}

func truncHash(h string) string {
	if len(h) > 16 {
		return h[:8] + "..." + h[len(h)-8:]
	}
	return h
}
