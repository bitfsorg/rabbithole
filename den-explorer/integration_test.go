package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/bitfsorg/libbitfs-go/network"
)

func setupTestServer(t *testing.T) *httptest.Server {
	t.Helper()

	url := os.Getenv("DEN_RPC_URL")
	if url == "" {
		url = "http://localhost:18332"
	}
	rpc := network.NewRPCClient(network.RPCConfig{
		URL: url, User: "bitfs", Password: "bitfs",
	})

	// Health check
	ctx := context.Background()
	if _, err := rpc.GetBestBlockHeight(ctx); err != nil {
		t.Skipf("RPC not available: %v", err)
	}

	explorer := NewExplorer(rpc)
	templates, err := LoadTemplates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}

	srv := NewServer(explorer, templates)
	return httptest.NewServer(srv.Routes())
}

func TestHomePage(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "Den") {
		t.Error("missing title")
	}
	if !strings.Contains(html, "regtest") {
		t.Error("missing network info")
	}
}

func TestBlockPage(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Get genesis block via height redirect
	resp, err := http.Get(ts.URL + "/block/height/0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Block #0") {
		t.Error("missing block height")
	}
}

func TestSearch_Height(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	// Search for block 0
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse // don't follow redirects
	}}

	resp, err := client.Get(ts.URL + "/search?q=0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 302 {
		t.Fatalf("expected redirect, got %d", resp.StatusCode)
	}
	loc := resp.Header.Get("Location")
	if !strings.HasPrefix(loc, "/block/") {
		t.Errorf("expected /block/ redirect, got %s", loc)
	}
}

func TestSearch_NotFound(t *testing.T) {
	ts := setupTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/search?q=nonexistent_garbage_query_12345")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "not found") && !strings.Contains(string(body), "Nothing found") {
		t.Error("expected not found message")
	}
}
