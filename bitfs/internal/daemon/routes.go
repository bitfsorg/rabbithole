package daemon

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/bitfsorg/libbitfs-go/paymail"
)

// RegisterRoutes registers all HTTP handlers on the provided mux.
func (d *Daemon) RegisterRoutes(mux *http.ServeMux) {
	// Wrap all handlers with middleware
	wrap := func(h http.HandlerFunc) http.HandlerFunc {
		return d.withMiddleware(h)
	}

	// System endpoints
	mux.HandleFunc("GET /_bitfs/health", wrap(d.handleHealth))
	mux.HandleFunc("OPTIONS /_bitfs/health", wrap(d.handleOptions))

	// Method 42 handshake
	mux.HandleFunc("POST /_bitfs/handshake", wrap(d.handleHandshake))
	mux.HandleFunc("OPTIONS /_bitfs/handshake", wrap(d.handleOptions))

	// Content endpoints
	mux.HandleFunc("GET /_bitfs/data/{hash}", wrap(d.handleData))
	mux.HandleFunc("GET /_bitfs/meta/{pnode}/{path...}", wrap(d.handleMeta))
	mux.HandleFunc("GET /_bitfs/versions/{pnode}/{path...}", wrap(d.handleVersions))

	// x402 Buy endpoints
	mux.HandleFunc("GET /_bitfs/buy/{txid}", wrap(d.handleGetBuyInfo))
	mux.HandleFunc("POST /_bitfs/buy/{txid}", wrap(d.handleSubmitHTLC))
	mux.HandleFunc("OPTIONS /_bitfs/buy/{txid}", wrap(d.handleOptions))

	// x402 Bandwidth payment
	mux.HandleFunc("POST /_bitfs/pay/{invoice_id}", wrap(d.handlePayInvoice))
	mux.HandleFunc("OPTIONS /_bitfs/pay/{invoice_id}", wrap(d.handleOptions))

	// Sales listing
	mux.HandleFunc("GET /_bitfs/sales", wrap(d.handleSales))

	// SPV proof endpoint
	mux.HandleFunc("GET /_bitfs/spv/proof/{txid}", wrap(d.handleSPVProof))
	mux.HandleFunc("OPTIONS /_bitfs/spv/proof/{txid}", wrap(d.handleOptions))

	// Dashboard API
	mux.HandleFunc("GET /_bitfs/dashboard/status", wrap(d.handleDashboardStatus))
	mux.HandleFunc("GET /_bitfs/dashboard/storage", wrap(d.handleDashboardStorage))
	mux.HandleFunc("GET /_bitfs/dashboard/wallet", wrap(d.handleDashboardWallet))
	mux.HandleFunc("GET /_bitfs/dashboard/network", wrap(d.handleDashboardNetwork))
	mux.HandleFunc("GET /_bitfs/dashboard/logs", wrap(d.handleDashboardLogs))

	// Paymail/BSV Alias
	mux.HandleFunc("GET /.well-known/bsvalias", wrap(d.handleBSVAlias))
	mux.HandleFunc("GET /api/v1/pki/{handle}", wrap(d.handlePKI))
	mux.HandleFunc("GET /api/v1/public-profile/{handle}", wrap(d.handlePublicProfile))
	mux.HandleFunc("GET /api/v1/verify/{handle}/{pubkey}", wrap(d.handleVerifyPubKey))

	// Catch-all for path-based content with content negotiation
	mux.HandleFunc("GET /", wrap(d.handleRootOrPath))
}

// withMiddleware applies rate limiting and CORS to a handler.
func (d *Daemon) withMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// CORS headers
		d.setCORSHeaders(w, r)

		// Rate limiting
		if d.rateLimiter != nil {
			ip := extractClientIP(r, d.config.Security.TrustProxy)
			if !d.rateLimiter.Allow(ip) {
				writeJSONError(w, http.StatusTooManyRequests, "RATE_LIMITED", "Too many requests")
				return
			}
		}

		next(w, r)
	}
}

// setCORSHeaders sets CORS headers based on configuration.
func (d *Daemon) setCORSHeaders(w http.ResponseWriter, r *http.Request) {
	origins := d.config.Security.CORS.Origins
	if len(origins) == 0 {
		origins = []string{"*"}
	}

	origin := r.Header.Get("Origin")
	allowedOrigin := ""
	for _, o := range origins {
		if o == "*" || o == origin {
			allowedOrigin = o
			break
		}
	}

	if allowedOrigin != "" {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
	}

	methods := d.config.Security.CORS.Methods
	if len(methods) == 0 {
		methods = []string{"GET", "POST", "OPTIONS"}
	}
	w.Header().Set("Access-Control-Allow-Methods", strings.Join(methods, ", "))
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Session-Id")
}

// handleOptions responds to CORS preflight requests.
func (d *Daemon) handleOptions(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// handleHealth responds with a health check.
func (d *Daemon) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, `{"status":"ok"}`)
}

// handleBSVAlias serves the .well-known/bsvalias capabilities document.
// Uses the configured ListenAddr instead of the request Host header to prevent
// attackers from injecting arbitrary hostnames into capability URLs (L-NEW-11).
func (d *Daemon) handleBSVAlias(w http.ResponseWriter, r *http.Request) {
	host := d.config.ListenAddr
	if host == "" || strings.HasPrefix(host, "0.0.0.0:") || strings.HasPrefix(host, ":") {
		port := strings.TrimPrefix(host, "0.0.0.0")
		if port == "" {
			port = ":8080"
		}
		host = "localhost" + port
	}

	scheme := "https"
	if !d.config.TLS.Enabled {
		scheme = "http"
	}
	base := scheme + "://" + host

	caps := map[string]interface{}{
		"bsvalias": "1.0",
		"capabilities": map[string]interface{}{
			"pki":                   base + "/api/v1/pki/{alias}@{domain.tld}",
			"f12f968c92d6":          base + "/api/v1/public-profile/{alias}@{domain.tld}",
			"a9f510c16bde":          base + "/api/v1/verify/{alias}@{domain.tld}/{pubkey}",
			paymail.BRFCBitFSBrowse: base + "/_bitfs/meta/{pnode}/{path}",
			paymail.BRFCBitFSBuy:    base + "/_bitfs/buy/{txid}",
			paymail.BRFCBitFSSell:   base + "/_bitfs/sales",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(caps)
}

// handleRootOrPath handles GET / and all sub-paths with content negotiation.
func (d *Daemon) handleRootOrPath(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}

	// Reject path traversal attempts.
	if containsPathTraversal(path) {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PATH", "Path must not contain '..' segments")
		return
	}

	d.serveWithContentNegotiation(w, r, path)
}

// serveWithContentNegotiation serves content based on the Accept header.
func (d *Daemon) serveWithContentNegotiation(w http.ResponseWriter, r *http.Request, path string) {
	// If Metanet service is available, try to resolve the path
	if d.metanet != nil {
		node, err := d.metanet.GetNodeByPath(path)
		if err == nil {
			// Check access control
			if node.Access == "paid" && d.config.X402.Enabled {
				d.servePaidContent(w, node)
				return
			}

			d.serveNodeContent(w, r, node)
			return
		}

		// For non-root paths, return 404 when metanet can't find the node
		if path != "/" {
			writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "Content not found")
			return
		}
		// For root path, fall through to basic info
	}

	// Without Metanet service or for root fallback, serve a basic info page
	d.serveBasicInfo(w, r, path)
}

// serveNodeContent serves content from a resolved node based on Accept header.
func (d *Daemon) serveNodeContent(w http.ResponseWriter, r *http.Request, node *NodeInfo) {
	accept := negotiateContentType(r)

	switch accept {
	case "text/html":
		d.serveHTML(w, node)
	case "text/markdown":
		d.serveMarkdown(w, node)
	default:
		d.serveJSON(w, node)
	}
}

// serveBasicInfo serves basic information about the daemon.
func (d *Daemon) serveBasicInfo(w http.ResponseWriter, r *http.Request, path string) {
	accept := negotiateContentType(r)

	switch accept {
	case "text/html":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>BitFS</title></head>
<body><h1>BitFS LFCP Node</h1><p>Path: %s</p></body></html>`, htmlEscape(path))
	case "text/markdown":
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		_, _ = fmt.Fprintf(w, "# BitFS LFCP Node\n\nPath: %s\n", markdownEscape(path))
	default:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"node": "BitFS LFCP",
			"path": path,
		})
	}
}

// serveHTML serves node content as HTML with optional WebMCP.
func (d *Daemon) serveHTML(w http.ResponseWriter, node *NodeInfo) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if node.Type == "dir" {
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><head><title>BitFS Directory</title></head><body>`)
		_, _ = fmt.Fprintf(w, `<h1>Directory</h1><ul>`)
		for _, child := range node.Children {
			escapedName := htmlEscape(child.Name)
			escapedType := htmlEscape(child.Type)
			_, _ = fmt.Fprintf(w, `<li><a href="%s">%s</a> (%s)</li>`, escapedName, escapedName, escapedType)
		}
		_, _ = fmt.Fprint(w, `</ul></body></html>`)
	} else {
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><head><title>BitFS File</title></head><body>`)
		_, _ = fmt.Fprintf(w, `<h1>%s</h1><p>Type: %s, Size: %d bytes</p>`, htmlEscape(node.MimeType), htmlEscape(node.Type), node.FileSize)
		_, _ = fmt.Fprint(w, `</body></html>`)
	}
}

// serveMarkdown serves node content as Markdown (for CLI agents).
func (d *Daemon) serveMarkdown(w http.ResponseWriter, node *NodeInfo) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")

	if node.Type == "dir" {
		_, _ = fmt.Fprint(w, "# Directory Listing\n\n")
		for _, child := range node.Children {
			_, _ = fmt.Fprintf(w, "- %s (%s)\n", markdownEscape(child.Name), markdownEscape(child.Type))
		}
	} else {
		_, _ = fmt.Fprintf(w, "# File\n\nType: %s\nSize: %d bytes\n", node.MimeType, node.FileSize)
	}
}

// serveJSON serves node metadata as JSON.
// Only fields safe for the node's access level are included.
// In particular, key_hash is only exposed for "free" access nodes.
func (d *Daemon) serveJSON(w http.ResponseWriter, node *NodeInfo) {
	w.Header().Set("Content-Type", "application/json")

	resp := map[string]interface{}{
		"type":   node.Type,
		"access": node.Access,
	}
	if node.MimeType != "" {
		resp["mime_type"] = node.MimeType
	}
	if node.FileSize > 0 {
		resp["file_size"] = node.FileSize
	}
	if node.PricePerKB > 0 {
		resp["price_per_kb"] = node.PricePerKB
	}
	// Only expose key_hash for free content.
	if node.Access == "free" && len(node.KeyHash) > 0 {
		resp["key_hash"] = hex.EncodeToString(node.KeyHash)
	}
	if node.Type == "dir" && len(node.Children) > 0 {
		resp["children"] = node.Children
	}

	_ = json.NewEncoder(w).Encode(resp)
}

// markdownEscape escapes Markdown special characters in a string.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"#", `\#`,
		"+", `\+`,
		"-", `\-`,
		".", `\.`,
		"!", `\!`,
		"|", `\|`,
	)
	return replacer.Replace(s)
}

// negotiateContentType determines the best content type from the Accept header.
func negotiateContentType(r *http.Request) string {
	accept := r.Header.Get("Accept")
	if accept == "" {
		return "application/json"
	}

	// Simple content negotiation
	if strings.Contains(accept, "text/html") {
		return "text/html"
	}
	if strings.Contains(accept, "text/markdown") {
		return "text/markdown"
	}
	if strings.Contains(accept, "application/json") {
		return "application/json"
	}

	// Default
	return "application/json"
}
