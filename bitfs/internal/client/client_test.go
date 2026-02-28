package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testPnode returns a valid 66-hex-char compressed public key for testing.
func testPnode() string {
	return "02" + strings.Repeat("ab", 32)
}

// testHash returns a valid 64-hex-char hash for testing.
func testHash() string {
	return strings.Repeat("ab", 32)
}

// --- New() and configuration tests ---

func TestNew_DefaultTimeout(t *testing.T) {
	c := New("http://localhost:8080")
	assert.Equal(t, "http://localhost:8080", c.BaseURL)
	assert.Equal(t, 30*time.Second, c.HTTPClient.Timeout)
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	c := New("http://localhost:8080/")
	assert.Equal(t, "http://localhost:8080", c.BaseURL)
}

func TestWithTimeout(t *testing.T) {
	c := New("http://localhost:8080")
	c2 := c.WithTimeout(5 * time.Second)

	// Returns a new client (copy semantics)
	assert.NotSame(t, c, c2)
	assert.Equal(t, 5*time.Second, c2.HTTPClient.Timeout)
	// Original unchanged
	assert.Equal(t, 30*time.Second, c.HTTPClient.Timeout)
	// BaseURL preserved
	assert.Equal(t, c.BaseURL, c2.BaseURL)
}

// --- GetMeta tests ---

func TestGetMeta_Success(t *testing.T) {
	pnode := testPnode()
	meta := MetaResponse{
		PNode:    pnode,
		Type:     "file",
		Path:     "docs/readme.txt",
		MimeType: "text/plain",
		FileSize: 1024,
		KeyHash:  "aabbccdd",
		Access:   "free",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/_bitfs/meta/"+pnode+"/docs/readme.txt", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta(pnode, "docs/readme.txt")
	require.NoError(t, err)

	assert.Equal(t, meta.PNode, got.PNode)
	assert.Equal(t, "file", got.Type)
	assert.Equal(t, "docs/readme.txt", got.Path)
	assert.Equal(t, "text/plain", got.MimeType)
	assert.Equal(t, uint64(1024), got.FileSize)
	assert.Equal(t, "free", got.Access)
}

func TestGetMeta_WithChildren(t *testing.T) {
	pnode := "03" + strings.Repeat("ab", 32)
	meta := MetaResponse{
		PNode:  pnode,
		Type:   "dir",
		Path:   "/",
		Access: "free",
		Children: []ChildEntry{
			{Name: "file1.txt", Type: "file"},
			{Name: "subdir", Type: "dir"},
		},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta(pnode, "/")
	require.NoError(t, err)

	assert.Len(t, got.Children, 2)
	assert.Equal(t, "file1.txt", got.Children[0].Name)
	assert.Equal(t, "dir", got.Children[1].Type)
}

func TestGetMeta_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"not found"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta(testPnode(), "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetMeta_PaymentRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Price-Per-KB", "50")
		w.Header().Set("X-File-Size", "10240")
		w.WriteHeader(http.StatusPaymentRequired)
		w.Write([]byte(`{"error":"payment required","price_per_kb":50,"file_size":10240}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta(testPnode(), "premium/video.mp4")
	assert.ErrorIs(t, err, ErrPaymentRequired)
}

func TestGetMeta_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL","message":"internal error"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta(testPnode(), "path")
	assert.ErrorIs(t, err, ErrServer)
}

func TestGetMeta_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1") // nothing listening on port 1
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.GetMeta(testPnode(), "path")
	assert.ErrorIs(t, err, ErrNetwork)
}

// --- GetData tests ---

func TestGetData_Success(t *testing.T) {
	content := []byte("encrypted file content here")
	hash := testHash()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/_bitfs/data/"+hash, r.URL.Path)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(content)
	}))
	defer srv.Close()

	c := New(srv.URL)
	rc, err := c.GetData(hash)
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, content, data)
}

func TestGetData_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":{"code":"NOT_FOUND","message":"not found"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetData(strings.Repeat("de", 32))
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetData_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetData(strings.Repeat("aa", 32))
	assert.ErrorIs(t, err, ErrServer)
}

func TestGetData_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.GetData(testHash())
	assert.ErrorIs(t, err, ErrNetwork)
}

// --- GetBuyInfo tests ---

func TestGetBuyInfo_Success(t *testing.T) {
	info := BuyInfo{
		CapsuleHash: "abc123",
		Price:       5000,
		PaymentAddr: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/_bitfs/buy/txid123", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(info)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetBuyInfo("txid123")
	require.NoError(t, err)

	assert.Equal(t, "abc123", got.CapsuleHash)
	assert.Equal(t, uint64(5000), got.Price)
	assert.Equal(t, "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", got.PaymentAddr)
}

func TestGetBuyInfo_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetBuyInfo("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetBuyInfo_PaymentRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetBuyInfo("txid123")
	assert.ErrorIs(t, err, ErrPaymentRequired)
}

// --- SubmitHTLC tests ---

func TestSubmitHTLC_Success(t *testing.T) {
	capsule := CapsuleResponse{
		Capsule: "encapsulated-key-data-hex",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/_bitfs/buy/txid456", r.URL.Path)
		assert.Equal(t, "application/octet-stream", r.Header.Get("Content-Type"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, []byte("raw-htlc-tx-bytes"), body)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(capsule)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.SubmitHTLC("txid456", []byte("raw-htlc-tx-bytes"))
	require.NoError(t, err)

	assert.Equal(t, "encapsulated-key-data-hex", got.Capsule)
}

func TestSubmitHTLC_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.SubmitHTLC("nonexistent", []byte("tx"))
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSubmitHTLC_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.SubmitHTLC("txid", []byte("tx"))
	assert.ErrorIs(t, err, ErrServer)
}

func TestSubmitHTLC_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.SubmitHTLC("txid", []byte("tx"))
	assert.ErrorIs(t, err, ErrNetwork)
}

// --- checkStatus helper tests ---

func TestCheckStatus_BadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetBuyInfo("bad")
	// 400 maps to a generic error (not one of our sentinel errors for specific statuses)
	assert.Error(t, err)
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrPaymentRequired)
	assert.NotErrorIs(t, err, ErrServer)
}

func TestCheckStatus_TooManyRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta(testPnode(), "path")
	assert.Error(t, err)
	// 429 is mapped to ErrServer (retryable server-side error)
	assert.ErrorIs(t, err, ErrServer)
}

// --- Edge cases ---

func TestGetMeta_SpecialCharsInPath(t *testing.T) {
	pnode := testPnode()
	meta := MetaResponse{
		PNode:  pnode,
		Type:   "file",
		Path:   "docs/my file#1.txt",
		Access: "free",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Each path segment should be individually URL-encoded.
		// "my file#1.txt" -> "my%20file%231.txt"
		// Use RequestURI which preserves the raw percent-encoded form.
		assert.Equal(t, "/_bitfs/meta/"+pnode+"/docs/my%20file%231.txt", r.RequestURI)
		// The server should decode the path correctly.
		assert.Equal(t, "/_bitfs/meta/"+pnode+"/docs/my file#1.txt", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta(pnode, "docs/my file#1.txt")
	require.NoError(t, err)
	assert.Equal(t, "docs/my file#1.txt", got.Path)
}

func TestGetMeta_EmptyPath(t *testing.T) {
	pnode := testPnode()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MetaResponse{PNode: pnode, Type: "dir", Access: "free"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta(pnode, "")
	require.NoError(t, err)
	assert.Equal(t, "dir", got.Type)
}

func TestGetData_LargeBody(t *testing.T) {
	// Ensure streaming works — body should not be buffered entirely
	bigData := make([]byte, 1024*1024) // 1 MB
	for i := range bigData {
		bigData[i] = byte(i % 256)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(bigData)
	}))
	defer srv.Close()

	c := New(srv.URL)
	rc, err := c.GetData(testHash())
	require.NoError(t, err)
	defer rc.Close()

	data, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, len(bigData), len(data))
	assert.Equal(t, bigData, data)
}

func TestGetMeta_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{invalid json`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta(testPnode(), "path")
	assert.Error(t, err)
	// Should not be a sentinel error — it's a decode error
	assert.NotErrorIs(t, err, ErrNotFound)
	assert.NotErrorIs(t, err, ErrServer)
}

func TestGetBuyInfo_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetBuyInfo("txid")
	assert.Error(t, err)
}

func TestSubmitHTLC_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.SubmitHTLC("txid", []byte("tx"))
	assert.Error(t, err)
}

// --- Query parameter injection tests ---

func TestGetBuyInfo_EscapesBuyerPubKey(t *testing.T) {
	var capturedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"capsule_hash":"abc","price":1000,"payment_addr":"addr","seller_pubkey":"def"}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, _ = c.GetBuyInfo("testtxid", "abc&injected=true")

	assert.NotContains(t, capturedURL, "&injected=true", "query parameter injection must be prevented")
	assert.Contains(t, capturedURL, "buyer_pubkey=abc%26injected%3Dtrue")
}

// --- Input validation tests (Task 20: security audit) ---

// TestValidateHex verifies the hex validation helper directly.
func TestValidateHex(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedBytes int
		fieldName     string
		wantErr       bool
		errContains   string
	}{
		{"valid 32 bytes", strings.Repeat("ab", 32), 32, "hash", false, ""},
		{"valid 33 bytes", "02" + strings.Repeat("ab", 32), 33, "pnode", false, ""},
		{"too short", "aabb", 32, "hash", true, "must be 64 hex characters"},
		{"too long", strings.Repeat("ab", 33), 32, "hash", true, "must be 64 hex characters"},
		{"odd length", strings.Repeat("a", 63), 32, "hash", true, "must be 64 hex characters"},
		{"not hex", strings.Repeat("zz", 32), 32, "hash", true, "invalid hash hex"},
		{"empty string", "", 32, "hash", true, "must be 64 hex characters"},
		{"mixed case valid", strings.Repeat("Ab", 32), 32, "hash", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateHex(tt.input, tt.expectedBytes, tt.fieldName)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetMeta_InvalidPnode verifies client-side pnode validation rejects bad inputs
// before making any HTTP request.
func TestGetMeta_InvalidPnode(t *testing.T) {
	// Use a server that would panic if called — ensuring we never reach it.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("HTTP request should not have been made")
	}))
	defer srv.Close()

	c := New(srv.URL)

	tests := []struct {
		name  string
		pnode string
	}{
		{"too short", "02abab"},
		{"too long", "02" + strings.Repeat("ab", 33)},
		{"not hex", strings.Repeat("zz", 33)},
		{"empty", ""},
		{"wrong length 32 bytes", strings.Repeat("ab", 32)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.GetMeta(tt.pnode, "some/path")
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "client:")
			assert.Contains(t, err.Error(), "pnode")
		})
	}
}

// TestGetData_InvalidHash verifies client-side hash validation rejects bad inputs
// before making any HTTP request.
func TestGetData_InvalidHash(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("HTTP request should not have been made")
	}))
	defer srv.Close()

	c := New(srv.URL)

	tests := []struct {
		name string
		hash string
	}{
		{"too short", "aabb"},
		{"too long", strings.Repeat("ab", 33)},
		{"not hex", strings.Repeat("zz", 32)},
		{"empty", ""},
		{"wrong length 33 bytes", strings.Repeat("ab", 33)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.GetData(tt.hash)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "client:")
			assert.Contains(t, err.Error(), "hash")
		})
	}
}

// --- GetVersions tests ---

func TestGetVersions_Success(t *testing.T) {
	pnode := testPnode()
	versions := []VersionEntry{
		{Version: 1, TxID: "aabb", BlockHeight: 100, Timestamp: 1700000000, FileSize: 512, Access: "free"},
		{Version: 2, TxID: "ccdd", BlockHeight: 99, Timestamp: 1699999000, FileSize: 256, Access: "paid"},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/_bitfs/versions/"+pnode)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(versions)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetVersions(pnode, "docs/readme.txt")
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, 1, got[0].Version)
	assert.Equal(t, "paid", got[1].Access)
}

func TestGetVersions_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetVersions(testPnode(), "nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetVersions_InvalidPnode(t *testing.T) {
	c := New("http://localhost:1")
	_, err := c.GetVersions("short", "path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "pnode")
}

func TestGetVersions_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetVersions(testPnode(), "path")
	assert.Error(t, err)
}

func TestGetVersions_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.GetVersions(testPnode(), "path")
	assert.ErrorIs(t, err, ErrNetwork)
}

// --- VerifySPV tests ---

func TestVerifySPV_Success(t *testing.T) {
	proof := SPVProofResponse{
		TxID:        "deadbeef",
		Confirmed:   true,
		BlockHash:   "blockhash123",
		BlockHeight: 800000,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/_bitfs/spv/proof/sometxid", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(proof)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.VerifySPV("sometxid")
	require.NoError(t, err)
	assert.True(t, got.Confirmed)
	assert.Equal(t, uint64(800000), got.BlockHeight)
}

func TestVerifySPV_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.VerifySPV("nonexistent")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestVerifySPV_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.VerifySPV("txid")
	assert.ErrorIs(t, err, ErrNetwork)
}

func TestVerifySPV_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{broken`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.VerifySPV("txid")
	assert.Error(t, err)
}

// --- GetSales tests ---

func TestGetSales_Success(t *testing.T) {
	sales := []SaleRecord{
		{InvoiceID: "inv1", Price: 1000, KeyHash: "aabb", Timestamp: 1700000000, Paid: true},
		{InvoiceID: "inv2", Price: 500, KeyHash: "ccdd", Timestamp: 1700001000, Paid: false},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.String(), "/_bitfs/sales")
		assert.Equal(t, "completed", r.URL.Query().Get("status"))
		assert.Equal(t, "10", r.URL.Query().Get("limit"))
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(sales)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetSales("completed", 10)
	require.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "inv1", got[0].InvoiceID)
	assert.True(t, got[0].Paid)
	assert.False(t, got[1].Paid)
}

func TestGetSales_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetSales("all", 50)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetSales_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.GetSales("all", 10)
	assert.ErrorIs(t, err, ErrNetwork)
}

func TestGetSales_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetSales("all", 10)
	assert.Error(t, err)
}
