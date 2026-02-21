package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	meta := MetaResponse{
		PNode:    "02abababababababababababababababababababababababababababababababababab",
		Type:     "file",
		Path:     "docs/readme.txt",
		MimeType: "text/plain",
		FileSize: 1024,
		KeyHash:  "aabbccdd",
		Access:   "free",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/_bitfs/meta/02abab/docs/readme.txt", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta("02abab", "docs/readme.txt")
	require.NoError(t, err)

	assert.Equal(t, meta.PNode, got.PNode)
	assert.Equal(t, "file", got.Type)
	assert.Equal(t, "docs/readme.txt", got.Path)
	assert.Equal(t, "text/plain", got.MimeType)
	assert.Equal(t, uint64(1024), got.FileSize)
	assert.Equal(t, "free", got.Access)
}

func TestGetMeta_WithChildren(t *testing.T) {
	meta := MetaResponse{
		PNode:  "03abab",
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
	got, err := c.GetMeta("03abab", "/")
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
	_, err := c.GetMeta("02abab", "nonexistent")
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
	_, err := c.GetMeta("02abab", "premium/video.mp4")
	assert.ErrorIs(t, err, ErrPaymentRequired)
}

func TestGetMeta_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":{"code":"INTERNAL","message":"internal error"}}`))
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetMeta("02abab", "path")
	assert.ErrorIs(t, err, ErrServer)
}

func TestGetMeta_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1") // nothing listening on port 1
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.GetMeta("02abab", "path")
	assert.ErrorIs(t, err, ErrNetwork)
}

// --- GetData tests ---

func TestGetData_Success(t *testing.T) {
	content := []byte("encrypted file content here")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/_bitfs/data/aabbccdd", r.URL.Path)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(content)
	}))
	defer srv.Close()

	c := New(srv.URL)
	rc, err := c.GetData("aabbccdd")
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
	_, err := c.GetData("deadbeef")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestGetData_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.GetData("aabb")
	assert.ErrorIs(t, err, ErrServer)
}

func TestGetData_NetworkError(t *testing.T) {
	c := New("http://127.0.0.1:1")
	c = c.WithTimeout(100 * time.Millisecond)
	_, err := c.GetData("aabbccdd")
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
	_, err := c.GetMeta("pnode", "path")
	assert.Error(t, err)
	// 429 is mapped to ErrServer (retryable server-side error)
	assert.ErrorIs(t, err, ErrServer)
}

// --- Edge cases ---

func TestGetMeta_SpecialCharsInPath(t *testing.T) {
	meta := MetaResponse{
		PNode:  "02abab",
		Type:   "file",
		Path:   "docs/my file#1.txt",
		Access: "free",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Each path segment should be individually URL-encoded.
		// "my file#1.txt" -> "my%20file%231.txt"
		// Use RequestURI which preserves the raw percent-encoded form.
		assert.Equal(t, "/_bitfs/meta/02abab/docs/my%20file%231.txt", r.RequestURI)
		// The server should decode the path correctly.
		assert.Equal(t, "/_bitfs/meta/02abab/docs/my file#1.txt", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(meta)
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta("02abab", "docs/my file#1.txt")
	require.NoError(t, err)
	assert.Equal(t, "docs/my file#1.txt", got.Path)
}

func TestGetMeta_EmptyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path should be /_bitfs/meta/pnode/ (trailing slash for empty path)
		assert.Equal(t, "/_bitfs/meta/pnode/", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(MetaResponse{PNode: "pnode", Type: "dir", Access: "free"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	got, err := c.GetMeta("pnode", "")
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
	rc, err := c.GetData("somehash")
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
	_, err := c.GetMeta("pnode", "path")
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
