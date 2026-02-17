// Copyright (c) 2024 The BitFS developers
// Use of this source code is governed by the Open BSV License v5
// that can be found in the LICENSE file.

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// ---------------------------------------------------------------------------
// Exit code tests
// ---------------------------------------------------------------------------

func TestNoArgs(t *testing.T) {
	code := run(nil)
	assert.Equal(t, 2, code, "no args should return usage error")
}

func TestEmptyArgs(t *testing.T) {
	code := run([]string{})
	assert.Equal(t, 2, code)
}

func TestInvalidURI(t *testing.T) {
	code := run([]string{"ftp://example.com/tree"})
	assert.Equal(t, 2, code, "non-bitfs URI should return error")
}

func TestEmptyURI(t *testing.T) {
	code := run([]string{""})
	assert.Equal(t, 2, code)
}

func TestValidPaymailURI(t *testing.T) {
	code := run([]string{"bitfs://alice@example.com/docs"})
	assert.Equal(t, 0, code)
}

func TestValidDNSLinkURI(t *testing.T) {
	code := run([]string{"bitfs://example.com/docs"})
	assert.Equal(t, 0, code)
}

func TestValidPubKeyURI(t *testing.T) {
	code := run([]string{"bitfs://02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2/tree"})
	assert.Equal(t, 0, code)
}

// ---------------------------------------------------------------------------
// Usage error stderr output
// ---------------------------------------------------------------------------

func TestNoArgsStderr(t *testing.T) {
	out := captureStderr(t, func() {
		run(nil)
	})
	assert.Contains(t, out, "Usage:")
	assert.Contains(t, out, "btree")
}

func TestInvalidURIStderr(t *testing.T) {
	out := captureStderr(t, func() {
		run([]string{"nope"})
	})
	assert.Contains(t, out, "Error:")
}

// ---------------------------------------------------------------------------
// Default text output
// ---------------------------------------------------------------------------

func TestDefaultOutputPaymail(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"bitfs://alice@example.com/docs"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, out, "Would show tree")
	assert.Contains(t, out, "Paymail")
	assert.Contains(t, out, "/docs")
}

func TestDefaultOutputDNSLink(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"bitfs://example.com/project"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, out, "DNSLink")
	assert.Contains(t, out, "/project")
}

func TestDefaultOutputPubKey(t *testing.T) {
	uri := "bitfs://02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2/root"
	out := captureStdout(t, func() {
		code := run([]string{uri})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, out, "PubKey")
	assert.Contains(t, out, "/root")
}

// ---------------------------------------------------------------------------
// --depth flag
// ---------------------------------------------------------------------------

func TestDepthFlag(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--depth", "3", "bitfs://example.com/docs"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, out, "Max depth")
	assert.Contains(t, out, "3")
}

func TestDepthFlagZero(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--depth", "0", "bitfs://example.com/docs"})
		assert.Equal(t, 0, code)
	})
	// depth 0 means unlimited, so it should NOT show max depth line
	assert.NotContains(t, out, "Max depth")
}

func TestDepthFlagDefault(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"bitfs://example.com/docs"})
		assert.Equal(t, 0, code)
	})
	// default depth is 0 (unlimited), should NOT show max depth line
	assert.NotContains(t, out, "Max depth")
}

func TestDepthFlagOne(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--depth", "1", "bitfs://example.com/"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, out, "Max depth")
	assert.Contains(t, out, "1")
}

// ---------------------------------------------------------------------------
// --json output
// ---------------------------------------------------------------------------

func TestJSONOutputPaymail(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--json", "bitfs://alice@example.com/docs"})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, "btree", result["command"])
	assert.Equal(t, "bitfs://alice@example.com/docs", result["uri"])
	assert.Equal(t, "Paymail", result["type"])
	assert.Equal(t, "/docs", result["path"])
	assert.Equal(t, float64(0), result["depth"])
	assert.Equal(t, "stub", result["status"])
}

func TestJSONOutputDNSLink(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--json", "bitfs://example.com/src"})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, "btree", result["command"])
	assert.Equal(t, "DNSLink", result["type"])
	assert.Equal(t, "/src", result["path"])
}

func TestJSONOutputPubKey(t *testing.T) {
	uri := "bitfs://03b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5/data"
	out := captureStdout(t, func() {
		code := run([]string{"--json", uri})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, "PubKey", result["type"])
	assert.Equal(t, "/data", result["path"])
}

func TestJSONOutputWithDepth(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--json", "--depth", "5", "bitfs://example.com/docs"})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, float64(5), result["depth"])
}

func TestJSONOutputIsValidJSON(t *testing.T) {
	out := captureStdout(t, func() {
		run([]string{"--json", "bitfs://example.com/"})
	})
	assert.True(t, json.Valid([]byte(strings.TrimSpace(out))))
}

// ---------------------------------------------------------------------------
// URI edge cases
// ---------------------------------------------------------------------------

func TestURIWithoutPath(t *testing.T) {
	code := run([]string{"bitfs://example.com"})
	assert.Equal(t, 0, code)
}

func TestURIRootPath(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--json", "bitfs://example.com/"})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, "/", result["path"])
}

func TestURIDeepPath(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--json", "bitfs://alice@example.com/a/b/c/d/e"})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, "/a/b/c/d/e", result["path"])
}

func TestMalformedPaymailURI(t *testing.T) {
	code := run([]string{"bitfs://@example.com/tree"})
	assert.Equal(t, 2, code)
}

func TestMalformedPaymailNoDomain(t *testing.T) {
	code := run([]string{"bitfs://alice@/tree"})
	assert.Equal(t, 2, code)
}

// ---------------------------------------------------------------------------
// Flag parsing edge cases
// ---------------------------------------------------------------------------

func TestUnknownFlag(t *testing.T) {
	code := run([]string{"--unknown", "bitfs://example.com/"})
	assert.Equal(t, 2, code)
}

func TestFlagsAfterURI(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"bitfs://example.com/docs", "--json"})
		assert.Equal(t, 0, code)
	})
	assert.Contains(t, out, "Would show tree")
}

func TestMultipleFlagsCombined(t *testing.T) {
	out := captureStdout(t, func() {
		code := run([]string{"--json", "--depth", "2", "bitfs://example.com/docs"})
		assert.Equal(t, 0, code)
	})
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
	assert.Equal(t, float64(2), result["depth"])
}

// ---------------------------------------------------------------------------
// Address type detection
// ---------------------------------------------------------------------------

func TestAddressTypeDetection(t *testing.T) {
	tests := []struct {
		name     string
		uri      string
		wantType string
	}{
		{"paymail", "bitfs://dave@example.com/tree", "Paymail"},
		{"dnslink", "bitfs://example.com/tree", "DNSLink"},
		{"pubkey 02", "bitfs://02a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2/tree", "PubKey"},
		{"pubkey 03", "bitfs://03b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5/tree", "PubKey"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := captureStdout(t, func() {
				code := run([]string{"--json", tt.uri})
				assert.Equal(t, 0, code)
			})
			var result map[string]interface{}
			require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(out)), &result))
			assert.Equal(t, tt.wantType, result["type"])
		})
	}
}
